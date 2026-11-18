package httputil

import (
	"errors"
	"testing"
)

type panickingBody struct {
	readErr error
	closes  int
}

func (b *panickingBody) Read([]byte) (int, error) {
	if b.readErr != nil {
		err := b.readErr

		b.readErr = nil

		return 0, err
	}

	panic("body read panic")
}

func (b *panickingBody) Close() error {
	b.closes++

	return nil
}

func TestBodyCloseOwnerPreservesPanicAndClosesOnce(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		readErr error
		read    func(*panickingBody) error
	}{
		{
			name: "read all",
			read: func(body *panickingBody) error {
				_, err := ReadAllAndClose(body, 64)

				return err
			},
		},
		{
			name:    "drain after failed read",
			readErr: errors.New("initial read failed"),
			read: func(body *panickingBody) error {
				_, err := ReadAllAndClose(body, 64)

				return err
			},
		},
		{
			name: "drain and close",
			read: func(body *panickingBody) error {
				return DrainAndClose(body, 64)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := &panickingBody{readErr: test.readErr}

			defer func() {
				if got := recover(); got != "body read panic" {
					t.Errorf("panic = %v, want original body read panic", got)
				}

				if body.closes != 1 {
					t.Errorf("body Close count = %d, want 1", body.closes)
				}
			}()

			if err := test.read(body); err != nil {
				t.Errorf("read returned %v, want a panic", err)
			}
		})
	}
}
