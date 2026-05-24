package logging

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextAttrs(t *testing.T) {
	ctx := context.Background()
	ctx = WithRuntime(ctx, "bot")
	ctx = WithComponent(ctx, "command")
	ctx = WithRequestID(ctx, "req-1")
	ctx = WithJobID(ctx, "job-1")

	attrs := ContextAttrs(ctx)
	require.Len(t, attrs, 4)
	require.Equal(t, "runtime", attrs[0].Key)
	require.Equal(t, "bot", attrs[0].Value.String())
	require.Equal(t, "component", attrs[1].Key)
	require.Equal(t, "command", attrs[1].Value.String())
	require.Equal(t, "request_id", attrs[2].Key)
	require.Equal(t, "req-1", attrs[2].Value.String())
	require.Equal(t, "job_id", attrs[3].Key)
	require.Equal(t, "job-1", attrs[3].Value.String())
}
