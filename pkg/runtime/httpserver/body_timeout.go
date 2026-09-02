package httpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// WithBodyReadTimeout은 handler 진입 이후의 요청 본문 읽기에 timeout 예산을 둔다.
//
// 요청 context에는 deadline을 더하지 않으므로 본문을 다 읽은 뒤의 서비스 작업은 제한하지
// 않는다. 예산이 끝나면 본문을 닫고 이후 Read는 context.DeadlineExceeded를 돌려주며,
// transport의 read deadline 초과(os.ErrDeadlineExceeded, net.Error Timeout)도 같은 오류로
// 정규화한다. ResponseController가 read deadline을 지원하면 socket deadline도 함께 건다.
func WithBodyReadTimeout(next http.Handler, timeout time.Duration) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}

	if timeout <= 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)

			return
		}

		controller := http.NewResponseController(w)
		deadlineSet := controller.SetReadDeadline(time.Now().Add(timeout)) == nil

		body := newBodyReadTimeout(r.Body, timeout)

		r.Body = body

		defer func() {
			closeErr := body.Close()

			if deadlineSet && controller.SetReadDeadline(time.Time{}) != nil {
				return
			}

			if closeErr != nil {
				return
			}
		}()

		next.ServeHTTP(w, r)
	})
}

type bodyReadTimeout struct {
	body      io.ReadCloser
	timer     *time.Timer
	closeOnce sync.Once
	mu        sync.Mutex
	expired   bool
	finished  bool
}

func newBodyReadTimeout(body io.ReadCloser, timeout time.Duration) *bodyReadTimeout {
	timed := &bodyReadTimeout{body: body}

	timed.timer = time.AfterFunc(timeout, timed.expire)

	return timed
}

func (b *bodyReadTimeout) Read(p []byte) (int, error) {
	b.mu.Lock()

	if b.expired {
		b.mu.Unlock()

		return 0, context.DeadlineExceeded
	}

	if b.finished {
		b.mu.Unlock()

		return 0, io.EOF
	}

	b.mu.Unlock()

	n, err := b.body.Read(p)

	b.mu.Lock()

	if bodyReadTimedOut(err) {
		b.expired = true
	}

	expired := b.expired

	if err != nil {
		b.finished = true
		b.timer.Stop()
	}

	b.mu.Unlock()

	if expired {
		b.closeExpiredBody()

		return n, context.DeadlineExceeded
	}

	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}

	if err != nil {
		return n, fmt.Errorf("read request body: %w", err)
	}

	return n, nil
}

func bodyReadTimedOut(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	netErr, ok := errors.AsType[net.Error](err)

	return ok && netErr.Timeout()
}

func (b *bodyReadTimeout) Close() error {
	b.stop()

	if err := b.closeBody(); err != nil {
		return fmt.Errorf("close timed request body: %w", err)
	}

	return nil
}

func (b *bodyReadTimeout) expire() {
	b.mu.Lock()

	if b.finished {
		b.mu.Unlock()

		return
	}

	b.expired = true
	b.mu.Unlock()

	b.closeExpiredBody()
}

func (b *bodyReadTimeout) stop() {
	b.mu.Lock()

	b.finished = true
	b.timer.Stop()

	b.mu.Unlock()
}

func (b *bodyReadTimeout) closeExpiredBody() {
	if err := b.closeBody(); err != nil {
		return
	}
}

func (b *bodyReadTimeout) closeBody() error {
	var err error

	b.closeOnce.Do(func() {
		if closeErr := b.body.Close(); closeErr != nil {
			err = fmt.Errorf("close request body: %w", closeErr)
		}
	})

	return err
}
