package tools

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
)

func TestMCPTracingMetadataPreservesParentAndRequest(t *testing.T) {
	mgr, err := langfuse.Init(langfuse.Config{Enabled: true, Host: "http://127.0.0.1:1", PublicKey: "test", SecretKey: "test", FlushAt: 1, QueueSize: 1, FlushInterval: time.Second, SampleRate: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()); _, _ = langfuse.Init(langfuse.Config{}) })
	const parent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	ctx := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": parent})
	ctx = context.WithValue(ctx, types.RequestIDContextKey, "request-current")
	meta := mcpTracingMetadata(ctx, &ToolExecContext{SessionID: "session", RequestID: "request-fallback"})
	require.Equal(t, map[string]any{"traceparent": parent, "weknora/session_id": "session", "weknora/request_id": "request-current"}, meta)
	meta = mcpTracingMetadata(context.Background(), &ToolExecContext{SessionID: "session", RequestID: "request-fallback"})
	require.Equal(t, "request-fallback", meta["weknora/request_id"])
	require.NotContains(t, meta, "traceparent")
}
