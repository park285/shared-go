package httputil

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) { return len(p), nil }

type countingBody struct {
	reader io.Reader
	read   int64
	closes int
	closeE error
	readE  error
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.readE != nil {
		return 0, b.readE
	}
	n, err := b.reader.Read(p)
	b.read += int64(n)
	return n, err
}

func (b *countingBody) Close() error {
	b.closes++
	return b.closeE
}

func TestReadAllAndCloseReturnsBodyAndCloses(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: strings.NewReader("payload")}
	data, err := ReadAllAndClose(body, 64)
	if err != nil {
		t.Fatalf("ReadAllAndClose() error = %v, want nil", err)
	}
	if string(data) != "payload" {
		t.Fatalf("ReadAllAndClose() = %q, want %q", data, "payload")
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
}

func TestReadAllAndCloseRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: strings.NewReader(strings.Repeat("a", 1024))}
	data, err := ReadAllAndClose(body, 16)
	if !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("ReadAllAndClose() error = %v, want ErrResponseBodyTooLarge", err)
	}
	if data != nil {
		t.Fatalf("ReadAllAndClose() data = %q, want nil on limit violation", data)
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
	if body.read > 16+1+DefaultDrainLimit+1 {
		t.Fatalf("read bytes = %d, want bounded by limit plus drain", body.read)
	}
}

func TestReadAllAndCloseJoinsReadAndCloseErrors(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read boom")
	closeErr := errors.New("close boom")
	body := &countingBody{reader: strings.NewReader("x"), readE: readErr, closeE: closeErr}

	_, err := ReadAllAndClose(body, 64)
	if !errors.Is(err, readErr) {
		t.Fatalf("ReadAllAndClose() error = %v, want wrapped read error", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("ReadAllAndClose() error = %v, want joined close error", err)
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
}

func TestReadAllAndCloseNilBody(t *testing.T) {
	t.Parallel()

	if _, err := ReadAllAndClose(nil, 64); !errors.Is(err, ErrNilBody) {
		t.Fatalf("ReadAllAndClose(nil) error = %v, want ErrNilBody", err)
	}
}

func TestReadAllAndCloseNegativeLimitClosesBody(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: strings.NewReader("payload")}
	if _, err := ReadAllAndClose(body, -1); err == nil {
		t.Fatal("ReadAllAndClose(-1) error = nil, want invalid limit error")
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
}

func TestDrainAndCloseStopsAtLimit(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: strings.NewReader(strings.Repeat("a", 4096))}
	if err := DrainAndClose(body, 128); err != nil {
		t.Fatalf("DrainAndClose() error = %v, want nil", err)
	}
	if body.read > 129 {
		t.Fatalf("drained bytes = %d, want at most limit plus EOF probe", body.read)
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
}

func TestDrainAndCloseZeroLimitOnlyCloses(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: strings.NewReader("payload")}
	if err := DrainAndClose(body, 0); err != nil {
		t.Fatalf("DrainAndClose() error = %v, want nil", err)
	}
	if body.read != 0 {
		t.Fatalf("drained bytes = %d, want 0", body.read)
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
}

func TestDrainAndCloseJoinsDrainAndCloseErrors(t *testing.T) {
	t.Parallel()

	drainErr := errors.New("drain boom")
	closeErr := errors.New("close boom")
	body := &countingBody{reader: strings.NewReader("x"), readE: drainErr, closeE: closeErr}

	err := DrainAndClose(body, 128)
	if !errors.Is(err, drainErr) || !errors.Is(err, closeErr) {
		t.Fatalf("DrainAndClose() error = %v, want joined drain and close errors", err)
	}
}

func TestJSONClientDiscardBodyBoundsUnlimitedStream(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: endlessReader{}}
	client := &JSONClient{}

	if err := client.DiscardBody(&http.Response{Body: body}); err != nil {
		t.Fatalf("DiscardBody() error = %v, want nil", err)
	}
	if body.read > DefaultDrainLimit+1 {
		t.Fatalf("drained bytes = %d, want bounded by DefaultDrainLimit", body.read)
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
}

func TestCheckStatusBoundsErrorBodyDrain(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: endlessReader{}}
	err := CheckStatus(&http.Response{StatusCode: http.StatusInternalServerError, Body: body})
	if !IsStatus(err, http.StatusInternalServerError) {
		t.Fatalf("CheckStatus() error = %v, want status 500 APIError", err)
	}
	if body.read > 4096+DefaultDrainLimit+1 {
		t.Fatalf("read bytes = %d, want bounded by body and drain limits", body.read)
	}
	if body.closes != 1 {
		t.Fatalf("close calls = %d, want 1", body.closes)
	}
}

func TestDrainAndCloseNilBody(t *testing.T) {
	t.Parallel()

	if err := DrainAndClose(nil, 128); err != nil {
		t.Fatalf("DrainAndClose(nil) error = %v, want nil", err)
	}
}
