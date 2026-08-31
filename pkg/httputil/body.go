package httputil

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// DefaultDrainLimit은 body를 닫기 전 keep-alive 재사용을 위해 버릴 최대 바이트다.
const DefaultDrainLimit int64 = 256 << 10

var (
	ErrNilBody          = errors.New("httputil: response body is nil")
	ErrInvalidBodyLimit = errors.New("httputil: invalid body limit")
)

// ReadAllLimited는 상한을 넘으면 ErrResponseBodyTooLarge를 반환한다. R의 close 소유권은 호출부에 남는다.
// MaxBytes가 음수면 ErrInvalidBodyLimit이고, 0은 빈 body만 허용한다는 뜻이다.
func ReadAllLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("%w: max_bytes=%d", ErrInvalidBodyLimit, maxBytes)
	}

	// 상한 초과 판별에 1바이트가 더 필요한데, MaxInt64에서 +1은 음수로 감겨 LimitReader가 0바이트만 읽는다.
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

// ReadAllAndClose는 어떤 경로로 끝나든 body를 닫는다. 상한 초과는 ErrResponseBodyTooLarge이며
// 남은 스트림은 DefaultDrainLimit까지만 버리고 포기한다. Read 실패와 close 실패는 errors.Join으로 함께 남긴다.
func ReadAllAndClose(rc io.ReadCloser, maxBytes int64) ([]byte, error) {
	data, err := ReadAllAndCloseWithDrainLimit(rc, maxBytes, DefaultDrainLimit)
	if err != nil {
		return nil, fmt.Errorf("read all and close with default drain limit: %w", err)
	}

	return data, nil
}

// ReadAllAndCloseWithDrainLimit는 어떤 경로로 끝나든 body를 닫는다. 읽기 실패 뒤 남은 스트림은
// 설정한 maxDrainBytes까지만 버리며 read, drain, close 실패는 errors.Join으로 함께 남긴다.
func ReadAllAndCloseWithDrainLimit(rc io.ReadCloser, maxBytes, maxDrainBytes int64) ([]byte, error) {
	if rc == nil {
		return nil, ErrNilBody
	}

	data, readErr := ReadAllAndDrain(rc, maxBytes, maxDrainBytes)
	if err := errors.Join(readErr, rc.Close()); err != nil {
		return nil, fmt.Errorf("read all and close: %w", err)
	}

	return data, nil
}

// ReadAllAndDrain은 body를 닫지 않고 maxBytes까지 읽는다. 실패하거나 상한을 넘으면
// 남은 스트림을 maxDrainBytes까지만 버리며 read와 drain 실패를 errors.Join으로 함께 남긴다.
func ReadAllAndDrain(r io.Reader, maxBytes, maxDrainBytes int64) ([]byte, error) {
	if r == nil {
		return nil, ErrNilBody
	}

	data, readErr := ReadAllLimited(r, maxBytes)
	if readErr == nil {
		return data, nil
	}

	return nil, fmt.Errorf("read all and drain: %w", errors.Join(readErr, drain(r, maxDrainBytes)))
}

// 상한을 넘는 스트림은 초과를 error로 보고하지 않고 close로 포기하므로 connection 재사용만 잃는다.
// Drain 실패와 close 실패는 errors.Join으로 함께 남긴다.
func DrainAndClose(rc io.ReadCloser, maxDrainBytes int64) error {
	if rc == nil {
		return nil
	}

	if err := errors.Join(drain(rc, maxDrainBytes), rc.Close()); err != nil {
		return fmt.Errorf("drain and close: %w", err)
	}

	return nil
}

func drain(r io.Reader, maxDrainBytes int64) error {
	if r == nil || maxDrainBytes <= 0 {
		return nil
	}

	// 정확히 상한 크기인 body도 EOF까지 소비해야 net/http가 connection을 재사용한다.
	drainLimit := maxDrainBytes
	if drainLimit < math.MaxInt64 {
		drainLimit++
	}

	if _, err := io.Copy(io.Discard, io.LimitReader(r, drainLimit)); err != nil {
		return fmt.Errorf("drain body: %w", err)
	}

	return nil
}
