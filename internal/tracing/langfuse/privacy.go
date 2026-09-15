package langfuse

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"go.opentelemetry.io/otel/attribute"
)

func (m *Manager) CaptureContent() bool { return m != nil && m.cfg.CaptureContent }

func (m *Manager) contentAttr(key string, value interface{}) attribute.KeyValue {
	if !m.CaptureContent() {
		return attribute.String(key, "[content capture disabled]")
	}
	return jsonAttr(key, value)
}

// A fixed allowlist keeps arbitrary prompts, queries and tool arguments out
// of metadata-only traces. Extend only with non-content correlation fields.
func (m *Manager) metadataAttr(key string, values map[string]interface{}) attribute.KeyValue {
	if m.CaptureContent() {
		return jsonAttr(key, values)
	}
	allowed := map[string]bool{
		"request_id": true, "session_id": true, "message_id": true, "agent_id": true,
		"model_id": true, "model": true, "streaming": true, "has_tools": true,
		"call_purpose": true, "prompt_prefix_fingerprint": true,
		"http.method": true, "http.path": true, "http.status_code": true,
		"tool_name": true, "tool_call_id": true, "tool_count": true,
		"skill_name": true, "file_path": true, "content_sha256": true,
		"iteration": true, "round": true, "rounds": true, "total_steps": true,
		"steps": true, "tool_calls": true, "complete": true, "max_iterations": true,
		"parallel_tool_calls": true, "web_search": true, "multi_turn": true,
		"tool_index": true, "argument_resolution": true, "unresolved_handle_count": true,
		"status": true, "outcome": true, "success": true, "error_code": true,
		"duration_ms": true, "total_duration_ms": true, "first_answer_ms": true,
		"elapsed_ms": true, "cancelled": true, "usage_reported": true,
		"environment": true, "release": true, "execution_config_hash": true,
	}
	out := map[string]interface{}{}
	for k, v := range values {
		if allowed[k] {
			// No arbitrary nested objects under otherwise allowed field names.
			switch v.(type) {
			case string, bool, int, int32, int64, uint, uint32, uint64, float32, float64:
				out[k] = v
			}
		}
	}
	return jsonAttr(key, out)
}

func observationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("execution_cancelled")
	}
	// Never ship upstream error bodies, even when content capture is enabled.
	return errors.New(types.ClassifyExecutionError(strings.TrimSpace(err.Error()), "").Code)
}
