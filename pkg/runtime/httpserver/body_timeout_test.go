package httpserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

type blockingReadCloser struct {
	closed     chan struct{}
	closeOnce  sync.Once
	closeCalls atomic.Int32
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (b *blockingReadCloser) Read([]byte) (int, error) {
	<-b.closed

	return 0, io.ErrClosedPipe
}

func (b *blockingReadCloser) Close() error {
	b.closeCalls.Add(1)
	b.closeOnce.Do(func() { close(b.closed) })

	return nil
}

func TestWithBodyReadTimeoutClosesBlockedBodyAndMapsDeadline(t *testing.T) {
	const timeout = 50 * time.Millisecond

	body := newBlockingReadCloser()

	var readErr error

	contextHadDeadline := false
	handler := WithBodyReadTimeout(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, contextHadDeadline = r.Context().Deadline()
		_, readErr = io.ReadAll(r.Body)
	}), timeout)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)

	started := time.Now()

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("blocked body elapsed = %v, want <1s", elapsed)
	}

	if !errors.Is(readErr, context.DeadlineExceeded) {
		t.Fatalf("body read error = %v, want context.DeadlineExceeded", readErr)
	}

	if contextHadDeadline {
		t.Fatal("request context acquired a deadline")
	}

	if got := body.closeCalls.Load(); got != 1 {
		t.Fatalf("original body close calls = %d, want 1", got)
	}

	if _, err := req.Body.Read(make([]byte, 1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("subsequent body read error = %v, want context.DeadlineExceeded", err)
	}

	if err := req.Body.Close(); err != nil {
		t.Fatalf("body Close() error = %v", err)
	}

	if got := body.closeCalls.Load(); got != 1 {
		t.Fatalf("original body close calls after handler close = %d, want 1", got)
	}
}

type transportTimeoutBody struct {
	closeCalls atomic.Int32
}

func (*transportTimeoutBody) Read([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }

func (b *transportTimeoutBody) Close() error {
	b.closeCalls.Add(1)

	return nil
}

func TestBodyReadTimeoutNormalizesTransportTimeoutAsTerminalDeadline(t *testing.T) {
	original := &transportTimeoutBody{}
	body := newBodyReadTimeout(original, time.Second)

	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Read() error = %v, want context.DeadlineExceeded", err)
	}

	if got := original.closeCalls.Load(); got != 1 {
		t.Fatalf("original body close calls after transport timeout = %d, want 1", got)
	}

	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("subsequent Read() error = %v, want context.DeadlineExceeded", err)
	}

	if err := body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := original.closeCalls.Load(); got != 1 {
		t.Fatalf("original body close calls = %d, want 1", got)
	}
}

type observedReadCloser struct {
	io.Reader

	closeCalls atomic.Int32
}

type deadlineRecordingWriter struct {
	http.ResponseWriter

	deadlines []time.Time
}

func (w *deadlineRecordingWriter) SetReadDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)

	return nil
}

func TestWithBodyReadTimeoutSetsAndClearsSupportedTransportDeadline(t *testing.T) {
	writer := &deadlineRecordingWriter{ResponseWriter: httptest.NewRecorder()}
	body := &observedReadCloser{Reader: strings.NewReader(`{"ok":true}`)}
	handler := WithBodyReadTimeout(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
	}), 50*time.Millisecond)

	handler.ServeHTTP(writer, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body))

	if len(writer.deadlines) != 2 || writer.deadlines[0].IsZero() || !writer.deadlines[1].IsZero() {
		t.Fatalf("transport deadlines = %v, want nonzero body deadline then clear", writer.deadlines)
	}
}

func (b *observedReadCloser) Close() error {
	b.closeCalls.Add(1)

	return nil
}

func TestWithBodyReadTimeoutStopsAtEOFWithoutCappingServiceWork(t *testing.T) {
	const timeout = 40 * time.Millisecond

	body := &observedReadCloser{Reader: strings.NewReader(`{"ok":true}`)}
	contextHadDeadline := false
	handler := WithBodyReadTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		_, contextHadDeadline = r.Context().Deadline()

		time.Sleep(2 * timeout)
		w.WriteHeader(http.StatusNoContent)
	}), timeout)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}

	if contextHadDeadline {
		t.Fatal("request context acquired a deadline")
	}

	if got := body.closeCalls.Load(); got != 1 {
		t.Fatalf("completed body close calls = %d, want 1 after handler completion", got)
	}
}

type closeObservedListener struct {
	net.Listener

	connectionCloseCalls atomic.Int32
}

func (l *closeObservedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("accept observed connection: %w", err)
	}

	return &closeObservedConn{Conn: conn, closeCalls: &l.connectionCloseCalls}, nil
}

type closeObservedConn struct {
	net.Conn

	closeCalls *atomic.Int32
}

func (c *closeObservedConn) Close() error {
	c.closeCalls.Add(1)

	if err := c.Conn.Close(); err != nil {
		return fmt.Errorf("close observed connection: %w", err)
	}

	return nil
}

type runningHTTPServer struct {
	server   *http.Server
	listener *closeObservedListener
	serveErr chan error
}

func startHTTP1BodyTimeoutServer(t *testing.T, timeout time.Duration, serviceCalls *atomic.Int32) *runningHTTPServer {
	t.Helper()

	return startHTTP1Server(t, timeout, newBodyTimeoutJSONHandler(serviceCalls))
}

func startHTTP1Server(t *testing.T, timeout time.Duration, handler http.Handler) *runningHTTPServer {
	t.Helper()

	listenConfig := net.ListenConfig{}

	baseListener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}

	listener := &closeObservedListener{Listener: baseListener}
	server := NewServer(
		listener.Addr().String(),
		WithBodyReadTimeout(handler, timeout),
		ServerOptions{ReadHeaderTimeout: timeout},
	)
	serveErr := make(chan error, 1)

	go func() { serveErr <- server.Serve(listener) }()

	t.Cleanup(func() {
		if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			t.Errorf("server.Close() error = %v", closeErr)
		}
	})

	return &runningHTTPServer{server: server, listener: listener, serveErr: serveErr}
}

func newBodyTimeoutJSONHandler(serviceCalls *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any

		err := httputil.DecodeJSONRequest(w, r, &payload, httputil.DecodeJSONRequestOptions{MaxBodyBytes: 1 << 20})

		switch {
		case errors.Is(err, context.DeadlineExceeded):
			w.WriteHeader(http.StatusRequestTimeout)

			return
		case err != nil:
			w.WriteHeader(httputil.DecodeJSONRequestStatus(err))

			return
		}

		serviceCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
}

func dialHTTP1BodyTimeoutServer(t *testing.T, address string) net.Conn {
	t.Helper()

	dialer := net.Dialer{}

	conn, err := dialer.DialContext(t.Context(), "tcp", address)
	if err != nil {
		t.Fatalf("dial loopback server: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("conn.Close() error = %v", closeErr)
		}
	})

	return conn
}

func writeHTTP1Part(t *testing.T, conn net.Conn, content string) {
	t.Helper()

	if _, err := fmt.Fprint(conn, content); err != nil {
		t.Fatalf("write request part: %v", err)
	}
}

func readHTTP1Status(t *testing.T, conn net.Conn) int {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}

	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("response body Close() error = %v", closeErr)
	}

	return response.StatusCode
}

func TestHTTP1SocketReadTimeoutMapsToBodyDeadlineBeforeIngressTimer(t *testing.T) {
	const (
		readTimeout = 150 * time.Millisecond
		headerDelay = 80 * time.Millisecond
	)

	serviceCalls := atomic.Int32{}
	running := startHTTP1BodyTimeoutServer(t, readTimeout, &serviceCalls)
	conn := dialHTTP1BodyTimeoutServer(t, running.listener.Addr().String())

	started := time.Now()

	time.Sleep(headerDelay)
	writeHTTP1Part(t, conn, "POST / HTTP/1.1\r\nHost: loopback\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n10\r\n{")

	statusCode := readHTTP1Status(t, conn)

	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("HTTP/1 slow body elapsed = %v, want <1s", elapsed)
	}

	if statusCode != http.StatusRequestTimeout {
		t.Fatalf("HTTP/1 slow body status = %d, want 408", statusCode)
	}

	if got := serviceCalls.Load(); got != 0 {
		t.Fatalf("service calls = %d, want 0", got)
	}

	deadline := time.Now().Add(time.Second)
	for running.listener.connectionCloseCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := running.listener.connectionCloseCalls.Load(); got == 0 {
		t.Fatal("server-side connection was not closed")
	}

	if err := running.server.Close(); err != nil {
		t.Fatalf("server.Close() error = %v", err)
	}

	select {
	case serveErr := <-running.serveErr:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			t.Fatalf("server.Serve() error = %v", serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("server.Serve() did not stop")
	}
}

func TestHTTP1EarlyRejectClosesIncompleteBodyUnderDeadline(t *testing.T) {
	const timeout = 75 * time.Millisecond

	serviceCalls := atomic.Int32{}
	running := startHTTP1Server(t, timeout, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	conn := dialHTTP1BodyTimeoutServer(t, running.listener.Addr().String())
	started := time.Now()

	writeHTTP1Part(t, conn, "POST / HTTP/1.1\r\nHost: loopback\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n10000\r\n{")

	statusCode := readHTTP1Status(t, conn)
	if statusCode != http.StatusTooManyRequests {
		t.Fatalf("early reject status = %d, want 429", statusCode)
	}

	if serviceCalls.Load() != 0 {
		t.Fatalf("service calls = %d, want 0", serviceCalls.Load())
	}

	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("early reject incomplete body elapsed = %v, want <1s", elapsed)
	}

	deadline := time.Now().Add(time.Second)
	for running.listener.connectionCloseCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := running.listener.connectionCloseCalls.Load(); got == 0 {
		t.Fatal("server-side early-reject connection was not closed")
	}
}

func TestHTTP1BodyBudgetStartsAfterDelayedHeaders(t *testing.T) {
	const (
		bodyTimeout = 150 * time.Millisecond
		headerDelay = 90 * time.Millisecond
		bodyDelay   = 90 * time.Millisecond
	)

	serviceCalls := atomic.Int32{}
	running := startHTTP1BodyTimeoutServer(t, bodyTimeout, &serviceCalls)
	conn := dialHTTP1BodyTimeoutServer(t, running.listener.Addr().String())

	started := time.Now()

	writeHTTP1Part(t, conn, "POST / HTTP/1.1\r\nHost: loopback\r\nContent-Type: application/json\r\nContent-Length: 11\r\n")

	time.Sleep(headerDelay)
	writeHTTP1Part(t, conn, "\r\n")

	time.Sleep(bodyDelay)
	writeHTTP1Part(t, conn, `{"ok":true}`)

	statusCode := readHTTP1Status(t, conn)

	if elapsed := time.Since(started); elapsed <= bodyTimeout {
		t.Fatalf("request elapsed = %v, want greater than total timeout to prove fresh body budget", elapsed)
	}

	if statusCode != http.StatusNoContent || serviceCalls.Load() != 1 {
		t.Fatalf("response = status %d service calls %d, want 204/1", statusCode, serviceCalls.Load())
	}
}
