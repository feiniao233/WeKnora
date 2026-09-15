package langfuse

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
)

func TestMetadataOnlyTracePreservesCorrelationUsageAndSafeFailure(t *testing.T) {
	m, exp := newTestManager(t)
	m.cfg.CaptureContent = false
	ctx, trace := m.StartTrace(context.Background(), TraceOptions{
		Name: "diagnosis", SessionID: "session", Input: "private-prompt",
		Metadata: map[string]interface{}{"request_id": "request", "http.query": "private-query"},
	})
	_, span := m.StartSpan(ctx, SpanOptions{Name: "read_skill", Input: "private-tool-args", Metadata: map[string]interface{}{
		"skill_name": "rca-test", "content_sha256": "digest", "raw_error": "private-error",
		"status": map[string]string{"bad": "private-nested"},
	}})
	span.Finish("private-tool-output", map[string]interface{}{"success": true}, nil)
	_, gen := m.StartGeneration(ctx, GenerationOptions{Name: "chat", Model: "deepseek-test", Input: "private-chat", Metadata: map[string]interface{}{"model_id": "model"}})
	gen.MarkCompletionStart(time.Now())
	gen.Finish("private-answer", &TokenUsage{Input: 10, Output: 2, Total: 12}, errors.New("429 private-credential private-provider-body"))
	trace.MarkError(errors.New("429 private-trace-error"))
	trace.Finish("private-result", map[string]interface{}{"http.status_code": 200})
	spans := exp.GetSpans()
	require.Len(t, spans, 3)
	encoded, err := json.Marshal(spans)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
	for _, s := range spans {
		switch s.Name {
		case "diagnosis":
			require.Contains(t, spanAttr(s.Attributes, attrTraceMetadata), `"request_id":"request"`)
			require.Contains(t, spanAttr(s.Attributes, attrTraceMetadata), `"http.status_code":200`)
			require.Equal(t, codes.Error, s.Status.Code)
		case "chat":
			require.Contains(t, spanAttr(s.Attributes, attrObsUsageDetails), `"total":12`)
			require.Equal(t, "deepseek-test", spanAttr(s.Attributes, attrObsModel))
			require.Equal(t, "ERROR", spanAttr(s.Attributes, "langfuse.observation.level"))
			require.Equal(t, "rate_limited", s.Status.Description)
		case "read_skill":
			require.Contains(t, spanAttr(s.Attributes, attrObsMetadata), `"content_sha256":"digest"`)
		}
	}
}

func TestContentCaptureRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("LANGFUSE_CAPTURE_CONTENT", "")
	require.False(t, LoadConfigFromEnv().CaptureContent)
	t.Setenv("LANGFUSE_CAPTURE_CONTENT", "true")
	require.True(t, LoadConfigFromEnv().CaptureContent)
}
