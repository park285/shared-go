package httputil

import (
	"errors"
	"io"
	"math"
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

	//nolint:wrapcheck // io.Reader 계약상 io.EOF를 포함한 하위 reader의 오류를 감싸지 않고 그대로 전달해야 한다.
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

func TestReadAllLimitedRejectsNegativeLimitWithoutReading(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: endlessReader{}}
	got, err := ReadAllLimited(body, -1)

	if !errors.Is(err, ErrInvalidBodyLimit) {
		t.Fatalf("ReadAllLimited(-1) error = %v, want ErrInvalidBodyLimit", err)
	}

	if got != nil {
		t.Fatalf("ReadAllLimited(-1) = %q, want nil", got)
	}

	if body.read != 0 {
		t.Fatalf("read bytes = %d, want 0", body.read)
	}

	if body.closes != 0 {
		t.Fatalf("close calls = %d, want 0: ReadAllLimited must not own close", body.closes)
	}
}

func TestReadAllLimitedLeavesBodyOpen(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: strings.NewReader("payload")}

	got, err := ReadAllLimited(body, 64)
	if err != nil {
		t.Fatalf("ReadAllLimited() error = %v, want nil", err)
	}

	if string(got) != "payload" {
		t.Fatalf("ReadAllLimited() = %q, want %q", got, "payload")
	}

	if body.closes != 0 {
		t.Fatalf("close calls = %d, want 0: ReadAllLimited must not own close", body.closes)
	}
}

func TestReadAllLimitedZeroLimitAcceptsOnlyEmptyBody(t *testing.T) {
	t.Parallel()

	got, err := ReadAllLimited(strings.NewReader(""), 0)
	if err != nil {
		t.Fatalf("ReadAllLimited(empty, 0) error = %v, want nil", err)
	}

	if len(got) != 0 {
		t.Fatalf("ReadAllLimited(empty, 0) = %q, want empty", got)
	}

	if _, err := ReadAllLimited(strings.NewReader("x"), 0); !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("ReadAllLimited(non-empty, 0) error = %v, want ErrResponseBodyTooLarge", err)
	}
}

func TestReadAllLimitedMaxInt64LimitDoesNotTruncate(t *testing.T) {
	t.Parallel()

	got, err := ReadAllLimited(strings.NewReader("payload"), math.MaxInt64)
	if err != nil {
		t.Fatalf("ReadAllLimited(MaxInt64) error = %v, want nil", err)
	}

	if string(got) != "payload" {
		t.Fatalf("ReadAllLimited(MaxInt64) = %q, want %q", got, "payload")
	}
}

func TestReadAllLimitedPropagatesReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read boom")
	body := &countingBody{reader: strings.NewReader("x"), readE: readErr}

	if _, err := ReadAllLimited(body, 64); !errors.Is(err, readErr) {
		t.Fatalf("ReadAllLimited() error = %v, want wrapped read error", err)
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
