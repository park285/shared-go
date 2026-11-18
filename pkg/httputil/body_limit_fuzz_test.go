package httputil

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func FuzzReadAllLimited(f *testing.F) {
	f.Add([]byte{}, int64(0))
	f.Add([]byte("ok"), int64(2))
	f.Add([]byte("\x00\xffmore"), int64(1))
	f.Add([]byte("ignored"), int64(-1))
	f.Add([]byte("ok"), int64(math.MaxInt64))

	f.Fuzz(func(t *testing.T, body []byte, limit int64) {
		r := bytes.NewReader(body)
		got, err := ReadAllLimited(r, limit)
		size := int64(len(body))

		var (
			wantErr  error
			consumed int64
		)

		switch {
		case limit < 0:
			wantErr = ErrInvalidBodyLimit
		case size > limit:
			wantErr = ErrResponseBodyTooLarge
			consumed = limit + 1
		default:
			consumed = size
		}

		if !errors.Is(err, wantErr) || size-int64(r.Len()) != consumed {
			t.Fatalf("size %d limit %d: error %v, consumed %d; want %v, %d", size, limit, err, size-int64(r.Len()), wantErr, consumed)
		}

		if (wantErr != nil && got != nil) || (wantErr == nil && !bytes.Equal(got, body)) {
			t.Fatalf("incorrect returned bytes for size %d limit %d", size, limit)
		}
	})
}
