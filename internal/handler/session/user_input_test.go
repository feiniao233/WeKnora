package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type userInputMessages struct {
	interfaces.MessageService
	latest *types.Message
	err    error
}

func (s *userInputMessages) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return []*types.Message{s.latest}, s.err
}
func userInputSource() *types.Message {
	request := types.UserInputRequest{ID: "8cf376b3-4f3f-4601-96fb-407f3e51b5dc", Questions: []types.UserInputQuestion{{ID: "q1", Question: "是否发生变更？", Options: []types.UserInputOption{{ID: "o1", Label: "有"}, {ID: "o2", Label: "不清楚"}}}}}
	return &types.Message{ID: "f81c8bd0-cf79-42ef-9c4a-bb49cf14d468", SessionID: "session", AgentID: "agent", Role: "assistant", IsCompleted: true, ExecutionResult: &types.MessageExecutionResult{Status: "completed"}, AgentSteps: types.AgentSteps{{ToolCalls: []types.ToolCall{{Name: types.RequestUserInputToolName, Result: &types.ToolResult{Success: true, Data: map[string]any{"user_input_request": request}}}}}}}
}
func answerContext() *qaRequestContext {
	return &qaRequestContext{sessionID: "session", customAgent: &types.CustomAgent{ID: "agent"}, userInputEnabled: true, userInputAnswer: &types.UserInputAnswer{SourceMessageID: userInputSource().ID, RequestID: "8cf376b3-4f3f-4601-96fb-407f3e51b5dc", Answers: []types.UserInputResponse{{QuestionID: "q1", OptionID: "o2"}}}}
}
func TestUserInputAnswerRequiresLatestCompletedOwnedAgentMessage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*types.Message, *qaRequestContext)
		err    error
	}{
		{"valid", func(*types.Message, *qaRequestContext) {}, nil},
		{"other session", func(m *types.Message, _ *qaRequestContext) { m.SessionID = "other" }, nil},
		{"superseded", func(m *types.Message, _ *qaRequestContext) { m.ID = "other" }, nil},
		{"other agent", func(m *types.Message, _ *qaRequestContext) { m.AgentID = "other" }, nil},
		{"unfinished", func(m *types.Message, _ *qaRequestContext) { m.IsCompleted = false }, nil},
		{"failed", func(m *types.Message, _ *qaRequestContext) { m.ExecutionResult.Status = "failed" }, nil},
		{"disabled client", func(_ *types.Message, r *qaRequestContext) { r.userInputEnabled = false }, nil},
		{"no questions", func(m *types.Message, _ *qaRequestContext) { m.AgentSteps = nil }, nil},
		{"unavailable", func(*types.Message, *qaRequestContext) {}, errors.New("access denied")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, r := userInputSource(), answerContext()
			tc.change(m, r)
			h := &Handler{messageService: &userInputMessages{latest: m, err: tc.err}}
			err := h.resolveUserInputAnswer(t.Context(), r)
			if tc.name == "valid" {
				require.NoError(t, err)
				require.Contains(t, r.query, "不清楚")
				require.Contains(t, r.query, "尚未核实")
			} else {
				require.Error(t, err)
			}
		})
	}
}
