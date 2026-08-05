package jsonutil

import (
	"fmt"
	"io"

	"github.com/park285/shared-go/pkg/httputil"
)

var ErrBodyTooLarge = httputil.ErrResponseBodyTooLarge

// Deprecated: httputil.ReadAllLimited를 직접 사용하십시오. v1 호환을 위해서만 남아 있는 위임 wrapper입니다.
// maxBytes가 0 이하일 때 무제한으로 읽던 예전 동작은 제거되었고 httputil.ErrInvalidBodyLimit을 반환합니다.
// r을 닫지 않는 계약은 그대로입니다.
func ReadAllLimit(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("jsonutil: %w: max_bytes=%d", httputil.ErrInvalidBodyLimit, maxBytes)
	}
	return httputil.ReadAllLimited(r, maxBytes)
}
