package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLoadMessagesExecutionProvenancePrivateSnapshot(t *testing.T) {
	t.Setenv("RESOURCE_URL_MODE", "handle")
	message := &types.Message{Role: "assistant", AgentID: "agent", ModelID: "model", AgentTenantID: 987,
		RenderedContent: "private-rendered-prompt", ExecutionContext: types.MessageExecutionContext{
			ExecutionConfigHash: "sha256-v1:historical-config", SkillsSelectionMode: "selected",
			SelectedSkillNames: []string{"rca-diagnosis"}, SkillNames: []string{"requested-skill"},
			KnowledgeBaseIDs: []string{"private-knowledge-scope"}, MCPServiceIDs: []string{"private-mcp-scope"},
			LangfuseTraceparent: "private-trace", AgentConfigHash: "private-suggestion-cache",
		},
	}
	legacy := &types.Message{Role: "assistant", AgentID: "agent", ModelID: "model"}
	router := newResourceURLTestRouter(t, []*types.Message{message, legacy, {Role: "user"}})
	var previous map[string]interface{}
	for _, query := range []string{"", "?resource_urls=public"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/messages/sess1/load"+query, nil))
		require.Equal(t, http.StatusOK, w.Code)
		for _, secret := range []string{"execution_context", "agent_tenant_id", "private-", "rendered_content", "langfuse_traceparent"} {
			require.NotContains(t, w.Body.String(), secret)
		}
		var body struct {
			Data []map[string]interface{} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Len(t, body.Data, 3)
		item := body.Data[0]
		require.Equal(t, "agent", item["agent_id"])
		require.Equal(t, "model", item["model_id"])
		require.Equal(t, map[string]interface{}{
			"execution_config_hash": "sha256-v1:historical-config", "skills_selection_mode": "selected",
			"selected_skill_names": []interface{}{"rca-diagnosis"}, "requested_skill_names": []interface{}{"requested-skill"},
		}, item["execution_provenance"])
		require.NotContains(t, body.Data[1], "execution_provenance")
		require.NotContains(t, body.Data[2], "execution_provenance")
		if previous != nil {
			require.Equal(t, previous, item)
		}
		previous = item
	}
	require.Nil(t, message.ExecutionProvenance, "response decoration must not mutate cached rows")
	projected := messagesWithExecutionProvenance([]*types.Message{message})
	projected[0].ExecutionProvenance.SelectedSkillNames[0] = "changed"
	require.Equal(t, "rca-diagnosis", message.ExecutionContext.SelectedSkillNames[0])
}

func TestLoadMessagesBeforeTimeExecutionProvenance(t *testing.T) {
	message := &types.Message{Role: "assistant", ExecutionContext: types.MessageExecutionContext{ExecutionConfigHash: "historical"}}
	router := newMessageTestRouter(&stubMessageService{getBeforeTime: func(context.Context, string, time.Time, int) ([]*types.Message, error) {
		return []*types.Message{message}, nil
	}})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/messages/sess1/load?before_time=2026-09-01T00:00:00Z", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"execution_provenance":{"execution_config_hash":"historical"}`)
	require.NotContains(t, w.Body.String(), "execution_context")
}
