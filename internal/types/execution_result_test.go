package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutionResultDatabaseRoundTrip(t *testing.T) {
	var absent *MessageExecutionResult
	v, err := absent.Value()
	require.NoError(t, err)
	require.Nil(t, v)
	failed := &MessageExecutionResult{Status: "failed", Error: ClassifyExecutionError("401 secret-key provider-body", "request")}
	v, err = failed.Value()
	require.NoError(t, err)
	var restored MessageExecutionResult
	require.NoError(t, restored.Scan(v))
	require.Equal(t, *failed, restored)
	encoded, err := json.Marshal(restored)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-key")
	require.NotContains(t, string(encoded), "provider-body")
	require.Equal(t, "model_unauthorized", restored.Error.Code)
	require.Error(t, restored.Scan(123))
}

func TestExecutionErrorClassification(t *testing.T) {
	for raw, code := range map[string]string{
		"429": "rate_limited", "403": "model_unauthorized", "context_length": "context_length_exceeded",
		"context deadline exceeded": "model_timeout", "requested chat model is unavailable": "model_unavailable",
		"reasoning_content is required": "model_protocol_error", "unexpected EOF": "stream_interrupted",
		"503": "upstream_unavailable", "execution result could not be saved": "message_save_failed", "unknown": "diagnosis_failed",
	} {
		t.Run(code, func(t *testing.T) {
			require.Equal(t, code, ClassifyExecutionError(raw, "request").Code)
			require.Equal(t, code, ClassifyExecutionError("model stream error: "+code, "request").Code)
		})
	}
}

func TestExplicitSelectionErrorsRemainSafeAndActionable(t *testing.T) {
	for _, code := range []string{"skill_unavailable", "attachment_unavailable"} {
		result := ClassifyExecutionError(code+": private backend details 403", "request")
		require.Equal(t, code, result.Code)
		require.True(t, result.Retryable)
		require.NotContains(t, result.Message, "private backend")
		require.Equal(t, "request", result.RequestID)
	}
}
