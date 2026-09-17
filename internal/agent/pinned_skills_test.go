package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/skills"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPinnedSkillsLoadBeforeModelAndRecordActualContentHash(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "answer", Done: true}}}}}
	engine := newTestEngine(t, model)
	engine.config.PinnedSkillNames = []string{"diagnosis"}
	manager := skills.NewManager(&skills.ManagerConfig{Enabled: true}, nil)
	manager.WithTenantSource(skills.NewTenantSkillSource([]*types.TenantSkillEntity{{Name: "diagnosis", Instructions: "Unique skill instructions: verify evidence before answering.", Enabled: true, Status: types.SkillStatusReady}}, nil))
	require.NoError(t, manager.Initialize(t.Context()))
	engine.toolRegistry = agenttools.NewToolRegistry()
	engine.toolRegistry.RegisterTool(agenttools.NewReadSkillTool(manager))
	var results []event.AgentToolResultData
	engine.eventBus.On(event.EventAgentToolResult, func(_ context.Context, evt event.Event) error {
		results = append(results, evt.Data.(event.AgentToolResultData))
		return nil
	})

	state, err := engine.Execute(t.Context(), "session", "message", "diagnose", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.True(t, results[0].Success)
	require.Equal(t, "answer", state.FinalAnswer)
	require.NotEmpty(t, model.calls)
	var loaded bool
	for _, msg := range model.calls[0] {
		if msg.Role == "user" && strings.Contains(msg.Content, "Unique skill instructions") {
			loaded = true
		}
	}
	require.True(t, loaded, "the first model call must contain actual installed skill instructions")
	call := state.RoundSteps[0].ToolCalls[0]
	require.True(t, types.IsPipelineToolCallID(call.ID))
	require.Equal(t, "read_skill", call.Name)
	require.True(t, call.Result.Success)
	require.NotEmpty(t, call.Result.Data["content_sha256"])
}

func TestPinnedSkillFailureStopsBeforeModel(t *testing.T) {
	model := &mockChat{}
	engine := newTestEngine(t, model)
	engine.config.PinnedSkillNames = []string{"missing"}
	engine.toolRegistry = agenttools.NewToolRegistry()
	_, err := engine.Execute(t.Context(), "session", "message", "diagnose", nil)
	require.ErrorContains(t, err, "skill_unavailable")
	require.Zero(t, model.callCount)
}

func TestNoPinnedSkillsLeavesMessagesUnchanged(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	messages := []chat.Message{{Role: "user", Content: "original"}}
	state := &types.AgentState{}
	got, err := engine.loadPinnedSkills(t.Context(), state, messages, "session", "message")
	require.NoError(t, err)
	require.Equal(t, messages, got)
	require.Empty(t, state.RoundSteps)
}
