package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type executionTestStream struct {
	interfaces.StreamManager
	events []interfaces.StreamEvent
}

func (s *executionTestStream) AppendEvent(_ context.Context, _, _ string, evt interfaces.StreamEvent) error {
	s.events = append(s.events, evt)
	return nil
}

type executionTestMessages struct {
	interfaces.MessageService
	saved *types.Message
	err   error
}

func (s *executionTestMessages) UpdateMessage(_ context.Context, msg *types.Message) error {
	copy := *msg
	s.saved = &copy
	return s.err
}

func TestFailedExecutionIsSavedBeforeCompletion(t *testing.T) {
	ctx := withExecutionOutcome(context.Background())
	message := &types.Message{ID: "message", ModelID: "deepseek-test"}
	stream := &executionTestStream{}
	handler := NewAgentStreamHandler(ctx, "session", message.ID, "request", 1, time.Now(), message, stream, event.NewEventBus(), nil)
	handler.deferCompletion = true
	require.NoError(t, handler.handleFinalAnswer(ctx, event.Event{ID: "answer", Data: event.AgentFinalAnswerData{Content: "已有发现"}}))
	require.NoError(t, handler.handleError(ctx, event.Event{Data: event.ErrorData{Error: "429 secret-provider-body"}}))
	// The service can propagate the same failure again; only one error is sent.
	require.NoError(t, handler.handleError(ctx, event.Event{Data: event.ErrorData{Error: "429 again"}}))
	require.NoError(t, handler.handleComplete(ctx, event.Event{Data: event.AgentCompleteData{MessageID: message.ID, Usage: &types.TokenUsage{TotalTokens: 12}}}))
	for _, evt := range stream.events {
		require.NotEqual(t, types.ResponseTypeComplete, evt.Type)
	}

	messages := &executionTestMessages{}
	require.NoError(t, (&Handler{messageService: messages}).completeAssistantMessage(ctx, message, "", ""))
	require.Equal(t, "已有发现", messages.saved.Content)
	require.Equal(t, "failed", messages.saved.ExecutionResult.Status)
	require.Equal(t, "rate_limited", messages.saved.ExecutionResult.Error.Code)
	require.Equal(t, 12, messages.saved.Usage.TotalTokens)
	require.NoError(t, handler.flushCompletion(ctx))
	require.NoError(t, handler.flushCompletion(ctx))
	require.Len(t, stream.events, 3) // answer, error, complete
	last := stream.events[2]
	require.Equal(t, types.ResponseTypeComplete, last.Type)
	require.Equal(t, "failed", last.Data["execution_result"].(*types.MessageExecutionResult).Status)
	require.Equal(t, message.ModelID, last.Data["model_id"])
	require.Equal(t, "request", last.Data["request_id"])
	require.NotContains(t, stream.events[1].Content, "secret")
}

func TestCompletionReportsSaveFailure(t *testing.T) {
	ctx := withExecutionOutcome(context.Background())
	message := &types.Message{ID: "message"}
	stream := &executionTestStream{}
	handler := NewAgentStreamHandler(ctx, "session", message.ID, "request", 1, time.Now(), message, stream, event.NewEventBus(), nil)
	messages := &executionTestMessages{err: errors.New("database unavailable")}
	require.Error(t, (&Handler{messageService: messages}).completeAssistantMessage(ctx, message, "", ""))
	require.NoError(t, handler.handleError(ctx, event.Event{Data: event.ErrorData{Error: "execution result could not be saved"}}))
	require.NoError(t, handler.flushCompletion(ctx))
	result := stream.events[1].Data["execution_result"].(*types.MessageExecutionResult)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "message_save_failed", result.Error.Code)
}

func TestToolFailureDoesNotFailExecution(t *testing.T) {
	ctx := withExecutionOutcome(context.Background())
	stream := &executionTestStream{}
	handler := NewAgentStreamHandler(ctx, "session", "message", "request", 1, time.Now(), &types.Message{}, stream, event.NewEventBus(), nil)
	require.NoError(t, handler.handleError(ctx, event.Event{Data: event.ErrorData{Error: "timeout", Extra: map[string]interface{}{"tool_call_id": "tool-1"}}}))
	require.Equal(t, "completed", executionResult(ctx).Status)
	require.Equal(t, "tool-1", stream.events[0].Data["tool_call_id"])
}

func TestExecutionOutcomeFirstTerminalResultSurvivesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(withExecutionOutcome(context.Background()))
	recordExecutionFailure(ctx, types.ClassifyExecutionError("429", "request"))
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); recordExecutionStop(ctx); _ = executionResult(ctx) }()
	}
	cancel()
	wg.Wait()
	require.Equal(t, "failed", executionResult(context.WithoutCancel(ctx)).Status)
	stopped := withExecutionOutcome(context.Background())
	recordExecutionStop(stopped)
	require.False(t, recordExecutionFailure(stopped, types.ClassifyExecutionError("EOF", "request")))
	require.Equal(t, "stopped", executionResult(stopped).Status)
}

func TestAgentStopWaitsForFinalizer(t *testing.T) {
	ctx, cancel := context.WithCancel(withExecutionOutcome(context.Background()))
	defer cancel()
	messages := &executionTestMessages{}
	bus := event.NewEventBus()
	h := &Handler{messageService: messages}
	h.setupStopEventHandler(ctx, bus, "session", 1, &types.Message{}, cancel, false)
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventStop}))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Nil(t, messages.saved)
	require.Equal(t, "stopped", executionResult(context.WithoutCancel(ctx)).Status)
}
