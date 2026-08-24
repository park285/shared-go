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
	if rc == nil {
		return nil, ErrNilBody
	}

	data, err := ReadAllLimited(rc, maxBytes)
	if err != nil {
		if joinedErr := errors.Join(err, DrainAndClose(rc, DefaultDrainLimit)); joinedErr != nil {
			return nil, fmt.Errorf("read all and close: %w", joinedErr)
		}

		return nil, nil
	}

	if closeErr := DrainAndClose(rc, DefaultDrainLimit); closeErr != nil {
		return nil, fmt.Errorf("httputil: close body: %w", closeErr)
	}

	return data, nil
}

// 상한을 넘는 스트림은 초과를 error로 보고하지 않고 close로 포기하므로 connection 재사용만 잃는다.
// Drain 실패와 close 실패는 errors.Join으로 함께 남긴다.
func DrainAndClose(rc io.ReadCloser, maxDrainBytes int64) error {
	if rc == nil {
		return nil
	}

	if maxDrainBytes <= 0 {
		if err := rc.Close(); err != nil {
			return fmt.Errorf("close: %w", err)
		}

		return nil
	}

	// 정확히 상한 크기인 body도 EOF까지 소비해야 net/http가 connection을 재사용한다.
	drainLimit := maxDrainBytes
	if drainLimit < math.MaxInt64 {
		drainLimit++
	}

	_, drainErr := io.Copy(io.Discard, io.LimitReader(rc, drainLimit))
	closeErr := rc.Close()

	if err := errors.Join(drainErr, closeErr); err != nil {
		return fmt.Errorf("drain and close: %w", err)
	}

	return nil
}
