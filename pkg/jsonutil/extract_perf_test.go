package jsonutil

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExtract_BracketFloodTerminates(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("{", 200000)

	done := make(chan error, 1)
	go func() {
		_, err := Extract(input)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrNoJSONFound) {
			t.Fatalf("Extract() error = %v, want ErrNoJSONFound", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Extract() did not terminate within 3s on bracket flood")
	}
}

func TestExtract_ArrayBracketFloodTerminates(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("[", 200000)

	done := make(chan error, 1)
	go func() {
		_, err := Extract(input)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrNoJSONFound) {
			t.Fatalf("Extract() error = %v, want ErrNoJSONFound", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Extract() did not terminate within 3s on array bracket flood")
	}
}
