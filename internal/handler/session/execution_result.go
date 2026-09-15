package session

import (
	"context"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

type executionOutcomeKey struct{}

// Only the outcome is shared between concurrent stop/error callbacks. The
// persisted result is an immutable snapshot taken by the final message save.
type executionOutcome struct {
	mu      sync.Mutex
	failure *types.ExecutionError
	stopped bool
}

func withExecutionOutcome(ctx context.Context) context.Context {
	return context.WithValue(ctx, executionOutcomeKey{}, &executionOutcome{})
}

func recordExecutionFailure(ctx context.Context, failure *types.ExecutionError) bool {
	o, _ := ctx.Value(executionOutcomeKey{}).(*executionOutcome)
	if o == nil {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure != nil || o.stopped {
		return false
	}
	o.failure = failure
	return true
}

func recordExecutionStop(ctx context.Context) {
	o, _ := ctx.Value(executionOutcomeKey{}).(*executionOutcome)
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure == nil {
		o.stopped = true
	}
}

func executionResult(ctx context.Context) *types.MessageExecutionResult {
	o, _ := ctx.Value(executionOutcomeKey{}).(*executionOutcome)
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure != nil {
		return &types.MessageExecutionResult{Status: "failed", Error: o.failure}
	}
	if o.stopped || ctx.Err() == context.Canceled {
		return &types.MessageExecutionResult{Status: "stopped"}
	}
	if ctx.Err() != nil {
		return &types.MessageExecutionResult{Status: "failed", Error: types.ClassifyExecutionError(ctx.Err().Error(), "")}
	}
	return &types.MessageExecutionResult{Status: "completed"}
}
