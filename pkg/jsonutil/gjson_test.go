package jsonutil

import (
	"errors"
	"testing"

	"github.com/park285/shared-go/pkg/httputil"
)

func TestErrBodyTooLargeAliasesHTTPUtilSentinel(t *testing.T) {
	t.Parallel()

	if !errors.Is(ErrBodyTooLarge, httputil.ErrResponseBodyTooLarge) {
		t.Fatalf("ErrBodyTooLarge = %v, want httputil.ErrResponseBodyTooLarge", ErrBodyTooLarge)
	}
}
