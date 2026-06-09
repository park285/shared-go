package httpserver

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStartServerWithPrefix_CustomErrorText(t *testing.T) {
	t.Parallel()
	wantText := "custom prefix"
	wantErr := errors.New("listen failed")
	server := newFakeServer(wantErr, nil)
	errCh := make(chan error, 1)

	StartServerWithPrefix(server, wantText, nil, errCh)

	select {
	case err := <-errCh:
		if !strings.Contains(err.Error(), wantText) {
			t.Fatalf("error = %q, want prefix %q", err, wantText)
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want wrapped %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
