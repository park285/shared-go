package httputil

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// DefaultDrainLimit은 body를 닫기 전 keep-alive 재사용을 위해 버릴 최대 바이트다.
const DefaultDrainLimit int64 = 256 << 10

var ErrNilBody = errors.New("httputil: response body is nil")

// ReadAllAndClose는 어떤 경로로 끝나든 body를 닫는다. 상한 초과는 ErrResponseBodyTooLarge이며
// 남은 스트림은 DefaultDrainLimit까지만 버리고 포기한다. read 실패와 close 실패는 errors.Join으로 함께 남긴다.
func ReadAllAndClose(rc io.ReadCloser, maxBytes int64) ([]byte, error) {
	if rc == nil {
		return nil, ErrNilBody
	}
	if maxBytes < 0 {
		closeErr := DrainAndClose(rc, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("httputil: invalid body limit %d", maxBytes), closeErr)
	}

	// maxBytes+1까지 읽어야 본문이 상한을 실제로 넘었는지 판별할 수 있다.
	data, readErr := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if readErr != nil {
		closeErr := DrainAndClose(rc, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("httputil: read body: %w", readErr), closeErr)
	}
	if int64(len(data)) > maxBytes {
		closeErr := DrainAndClose(rc, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("%w: max_bytes=%d", ErrResponseBodyTooLarge, maxBytes), closeErr)
	}
	if closeErr := DrainAndClose(rc, DefaultDrainLimit); closeErr != nil {
		return nil, fmt.Errorf("httputil: close body: %w", closeErr)
	}
	return data, nil
}

// 상한을 넘는 스트림은 초과를 error로 보고하지 않고 close로 포기하므로 connection 재사용만 잃는다.
// drain 실패와 close 실패는 errors.Join으로 함께 남긴다.
func DrainAndClose(rc io.ReadCloser, maxDrainBytes int64) error {
	if rc == nil {
		return nil
	}
	if maxDrainBytes <= 0 {
		return rc.Close()
	}
	// 정확히 상한 크기인 body도 EOF까지 소비해야 net/http가 connection을 재사용한다.
	drainLimit := maxDrainBytes
	if drainLimit < math.MaxInt64 {
		drainLimit++
	}
	_, drainErr := io.Copy(io.Discard, io.LimitReader(rc, drainLimit))
	closeErr := rc.Close()
	return errors.Join(drainErr, closeErr)
}
