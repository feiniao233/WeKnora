package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestExecutionProvenanceCapturesConfigWithoutChangingSuggestionCache(t *testing.T) {
	agent := &types.CustomAgent{ID: "agent", TenantID: 1, Config: types.CustomAgentConfig{
		ModelID: "model", SystemPrompt: "private prompt", SkillsSelectionMode: "selected", SelectedSkills: []string{"rca-diagnosis"},
	}}
	requested := []string{"requested-skill"}
	capture := func() types.MessageExecutionContext {
		snapshot, id, tenant, model := buildMessageExecutionContext(context.Background(), agent, 1, "", nil, nil, nil, nil, nil, requested, []string{"submit_rca_report"}, false)
		require.Equal(t, "agent", id)
		require.Equal(t, uint64(1), tenant)
		require.Equal(t, "model", model)
		return snapshot
	}
	original := capture()
	require.Regexp(t, `^sha256-v1:[a-f0-9]{64}$`, original.ExecutionConfigHash)
	require.Equal(t, original.ExecutionConfigHash, capture().ExecutionConfigHash)
	override, _, _, model := buildMessageExecutionContext(context.Background(), agent, 1, "override-model", nil, nil, nil, nil, nil, requested, []string{"submit_rca_report"}, false)
	require.Equal(t, "override-model", model)
	require.NotEqual(t, original.ExecutionConfigHash, override.ExecutionConfigHash)
	agent.Config.SystemPrompt = "edited private prompt"
	edited := capture()
	require.NotEqual(t, original.ExecutionConfigHash, edited.ExecutionConfigHash)
	require.Equal(t, original.AgentConfigHash, edited.AgentConfigHash, "prompt changes must not invalidate suggestion-cache scope")
	agent.Config.MaxIterations = 20
	require.NotEqual(t, edited.ExecutionConfigHash, capture().ExecutionConfigHash)
	agent.Config.SelectedSkills[0] = "replacement"
	requested[0] = "replacement-request"
	require.Equal(t, []string{"rca-diagnosis"}, original.SelectedSkillNames)
	require.Equal(t, []string{"requested-skill"}, original.SkillNames)
	require.Equal(t, []string{"submit_rca_report"}, original.DisabledToolNames)
	value, err := original.Value()
	require.NoError(t, err)
	var restored types.MessageExecutionContext
	require.NoError(t, restored.Scan(value))
	require.Equal(t, original, restored)
	encoded, err := json.Marshal(restored)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private prompt")
}

func TestNormalizeDisabledToolNames(t *testing.T) {
	names, err := normalizeDisabledToolNames([]string{"submit_rca_report", "submit_rca_report"})
	require.NoError(t, err)
	require.Equal(t, []string{"submit_rca_report"}, names)

	for _, values := range [][]string{{" bad"}, {"bad/tool"}, {strings.Repeat("x", 65)}} {
		_, err := normalizeDisabledToolNames(values)
		require.Error(t, err)
	}
}

func TestExecutionProvenanceMissingAgentHasNoFingerprint(t *testing.T) {
	snapshot, _, _, _ := buildMessageExecutionContext(context.Background(), nil, 1, "model", nil, nil, nil, nil, nil, nil, nil, false)
	require.Empty(t, snapshot.ExecutionConfigHash)
}
