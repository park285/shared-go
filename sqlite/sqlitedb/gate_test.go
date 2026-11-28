package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func openGateTest(t *testing.T) *DB {
	t.Helper()

	db, err := Open(t.Context(), Options{
		Path:               filepath.Join(t.TempDir(), "state", "state.db"),
		ReadConns:          1,
		ConnMaxIdleTime:    time.Minute,
		BusyTimeout:        2 * time.Second,
		JournalSizeLimit:   8 << 20,
		CheckpointInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := db.Close(context.WithoutCancel(t.Context())); closeErr != nil {
			t.Errorf("close: %v", closeErr)
		}
	})

	return db
}

func requireWaiters(t *testing.T, db *DB, want int) {
	t.Helper()
	synctest.Wait()

	if got := db.gate.waiting(); got != want {
		t.Fatalf("writers waiting = %d, want %d", got, want)
	}
}

func holdWriter(t *testing.T, db *DB) (release func(), done <-chan error) {
	t.Helper()

	held := make(chan struct{})
	unblock := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		result <- db.WriteTx(context.WithoutCancel(t.Context()), func(*sql.Tx) error {
			close(held)
			<-unblock

			return nil
		})
	}()

	<-held

	return func() { close(unblock) }, result
}

func TestWriteTxGrantsPriorityWritersFirst(t *testing.T) {
	synctest.Test(t, testPriorityWritersFirst)
}

func testPriorityWritersFirst(t *testing.T) {
	db := openGateTest(t)
	release, holder := holdWriter(t, db)

	var (
		mu    sync.Mutex
		order []string
		wg    sync.WaitGroup
	)

	write := func(ctx context.Context, name string) {
		defer wg.Done()

		if err := db.WriteTx(ctx, func(*sql.Tx) error {
			mu.Lock()
			defer mu.Unlock()

			order = append(order, name)

			return nil
		}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	ctx := t.Context()

	for i, name := range []string{"normal-1", "normal-2", "normal-3"} {
		wg.Add(1)

		go write(ctx, name)

		requireWaiters(t, db, i+1)
	}

	for i, name := range []string{"priority-1", "priority-2"} {
		wg.Add(1)

		go write(WithWritePriority(ctx), name)

		requireWaiters(t, db, 4+i)
	}

	release()
	wg.Wait()

	if err := <-holder; err != nil {
		t.Fatalf("holder: %v", err)
	}

	want := []string{"priority-1", "priority-2", "normal-1", "normal-2", "normal-3"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}

	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestWriteTxCanceledWaiterLeavesTheQueue(t *testing.T) {
	synctest.Test(t, testCanceledWaiterLeavesTheQueue)
}

func testCanceledWaiterLeavesTheQueue(t *testing.T) {
	db := openGateTest(t)
	release, holder := holdWriter(t, db)

	ctx, cancel := context.WithCancel(t.Context())
	canceled := make(chan error, 1)

	go func() {
		canceled <- db.WriteTx(ctx, func(*sql.Tx) error { return nil })
	}()

	requireWaiters(t, db, 1)
	cancel()

	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter = %v, want context.Canceled", err)
	}

	requireWaiters(t, db, 0)
	release()

	if err := <-holder; err != nil {
		t.Fatalf("holder: %v", err)
	}

	next, cancelNext := context.WithTimeout(t.Context(), time.Second)
	defer cancelNext()

	if err := db.WriteTx(next, func(*sql.Tx) error { return nil }); err != nil {
		t.Fatalf("writer after a canceled waiter: %v", err)
	}
}

type grantedWhileCanceling struct {
	gate *writeGate
	done chan struct{}
	once sync.Once
}

func (c *grantedWhileCanceling) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *grantedWhileCanceling) Value(any) any { return nil }

func (c *grantedWhileCanceling) Done() <-chan struct{} {
	c.once.Do(func() {
		c.gate.release()
		close(c.done)
	})

	return c.done
}

func (c *grantedWhileCanceling) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func TestWriteGateCanceledAfterGrantPassesTheGateOn(t *testing.T) {
	for range 64 {
		var gate writeGate

		if err := gate.acquire(t.Context()); err != nil {
			t.Fatalf("first acquire: %v", err)
		}

		ctx := &grantedWhileCanceling{gate: &gate, done: make(chan struct{})}

		err := gate.acquire(ctx)
		if err == nil {
			gate.release()
		} else if !errors.Is(err, context.Canceled) {
			t.Fatalf("acquire = %v, want nil or context.Canceled", err)
		}

		if gateHeld(&gate) {
			t.Fatalf("gate still held after a waiter was granted while canceling (acquire = %v)", err)
		}
	}
}

func gateHeld(g *writeGate) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.held
}
