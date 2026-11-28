package sqlitedb

import (
	"context"
	"slices"
	"sync"
)

type writePriorityKey struct{}

func WithWritePriority(ctx context.Context) context.Context {
	return context.WithValue(ctx, writePriorityKey{}, true)
}

func hasWritePriority(ctx context.Context) bool {
	priority, ok := ctx.Value(writePriorityKey{}).(bool)

	return ok && priority
}

type writeGate struct {
	mu       sync.Mutex
	held     bool
	priority []chan struct{}
	normal   []chan struct{}
}

func (g *writeGate) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	g.mu.Lock()

	if !g.held {
		g.held = true
		g.mu.Unlock()

		return nil
	}

	granted := make(chan struct{})
	queue := &g.normal

	if hasWritePriority(ctx) {
		queue = &g.priority
	}

	*queue = append(*queue, granted)
	g.mu.Unlock()

	select {
	case <-granted:
		return nil
	case <-ctx.Done():
		g.mu.Lock()

		index := slices.Index(*queue, granted)
		if index >= 0 {
			*queue = slices.Delete(*queue, index, index+1)
		}

		g.mu.Unlock()

		if index < 0 {
			g.release()
		}

		return ctx.Err()
	}
}

func (g *writeGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, queue := range []*[]chan struct{}{&g.priority, &g.normal} {
		if len(*queue) > 0 {
			next := (*queue)[0]

			*queue = slices.Delete(*queue, 0, 1)

			close(next)

			return
		}
	}

	g.held = false
}

func (g *writeGate) waiting() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.priority) + len(g.normal)
}
