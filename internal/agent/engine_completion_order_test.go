package agent

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestExecutionFailurePrecedesCompletion(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "partial"},
		{ResponseType: types.ResponseTypeError, Content: "unexpected EOF", Done: true, FinishReason: types.FinishReasonIncomplete},
	}}}}
	engine := newTestEngine(t, model)
	var events []event.EventType
	for _, kind := range []event.EventType{event.EventError, event.EventAgentComplete} {
		engine.eventBus.On(kind, func(_ context.Context, evt event.Event) error { events = append(events, evt.Type); return nil })
	}
	_, err := engine.executeLoop(context.Background(), &types.AgentState{}, "query", emptyMessages(), nil, "session", "message")
	require.Error(t, err)
	require.Equal(t, []event.EventType{event.EventError, event.EventAgentComplete}, events)
}
