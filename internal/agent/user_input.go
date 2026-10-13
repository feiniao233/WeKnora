package agent

import (
	"context"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

func (e *AgentEngine) completeWithUserInputRequest(ctx context.Context, state *types.AgentState, step types.AgentStep, sessionID string) bool {
	if types.UserInputRequestFromSteps(types.AgentSteps{step}) == nil {
		return false
	}
	answer := "还需要补充一些信息，请回答下面的问题后继续分析。"
	id := generateEventID("answer")
	_ = e.eventBus.Emit(ctx, event.Event{ID: id, Type: event.EventAgentFinalAnswer, SessionID: sessionID, Data: event.AgentFinalAnswerData{Content: answer}})
	_ = e.eventBus.Emit(ctx, event.Event{ID: id, Type: event.EventAgentFinalAnswer, SessionID: sessionID, Data: event.AgentFinalAnswerData{Done: true}})
	state.FinalAnswer = answer
	state.IsComplete = true
	return true
}
