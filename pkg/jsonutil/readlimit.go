package jsonutil

import (
	"errors"
	"io"
)

var ErrBodyTooLarge = errors.New("body too large")

func ReadAllLimit(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(r)
	}

	lr := io.LimitReader(r, maxBytes+1)
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, ErrBodyTooLarge
	}
	return b, nil
}
