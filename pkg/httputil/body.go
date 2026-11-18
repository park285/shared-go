package httputil

import (
	"errors"
	"fmt"
	"io"
	"math"
)

const DefaultDrainLimit int64 = 256 << 10

var (
	ErrNilBody          = errors.New("httputil: response body is nil")
	ErrInvalidBodyLimit = errors.New("httputil: invalid body limit")
)

func ReadAllLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("%w: max_bytes=%d", ErrInvalidBodyLimit, maxBytes)
	}

	readLimit := maxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}

	data, readErr := io.ReadAll(io.LimitReader(r, readLimit))
	if readErr != nil {
		return nil, fmt.Errorf("httputil: read body: %w", readErr)
	}

	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: max_bytes=%d", ErrResponseBodyTooLarge, maxBytes)
	}

	return data, nil
}

func ReadAllAndClose(rc io.ReadCloser, maxBytes int64) ([]byte, error) {
	return readAllAndCloseWithDrainLimit(rc, maxBytes, DefaultDrainLimit)
}

func readAllAndCloseWithDrainLimit(rc io.ReadCloser, maxBytes, maxDrainBytes int64) (data []byte, err error) {
	if rc == nil {
		return nil, ErrNilBody
	}

	defer func() {
		if closeErr := errors.Join(err, rc.Close()); closeErr != nil {
			data = nil
			err = fmt.Errorf("httputil: read all and close: %w", closeErr)
		}
	}()

	return ReadAllAndDrain(rc, maxBytes, maxDrainBytes)
}

func ReadAllAndDrain(r io.Reader, maxBytes, maxDrainBytes int64) ([]byte, error) {
	if r == nil {
		return nil, ErrNilBody
	}

	data, readErr := ReadAllLimited(r, maxBytes)
	if readErr == nil {
		return data, nil
	}

	return nil, errors.Join(readErr, drain(r, maxDrainBytes))
}

func DrainAndClose(rc io.ReadCloser, maxDrainBytes int64) (err error) {
	if rc == nil {
		return nil
	}

	defer func() {
		if closeErr := errors.Join(err, rc.Close()); closeErr != nil {
			err = fmt.Errorf("httputil: drain and close: %w", closeErr)
		}
	}()

	return drain(rc, maxDrainBytes)
}

func drain(r io.Reader, maxDrainBytes int64) error {
	if r == nil || maxDrainBytes <= 0 {
		return nil
	}

	drainLimit := maxDrainBytes
	if drainLimit < math.MaxInt64 {
		drainLimit++
	}

	if _, err := io.Copy(io.Discard, io.LimitReader(r, drainLimit)); err != nil {
		return fmt.Errorf("drain body: %w", err)
	}

	return nil
}
