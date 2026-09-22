package stream

import (
	"context"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecutionIdentityCAS(t *testing.T) {
	redisManager, _ := newTestRedisStreamManager(t, time.Minute)
	for name, manager := range map[string]interfaces.StreamManager{"memory": NewMemoryStreamManager(), "redis": redisManager} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			require.NoError(t, manager.ClaimExecution(ctx, "s", "m", "r"))
			require.ErrorIs(t, manager.ClaimExecution(ctx, "s", "m", "other"), ErrLiveRunExists)
			require.NoError(t, manager.ReleaseExecution(ctx, "s", "m", "other"))
			id, _, err := manager.PeekExecution(ctx, "s")
			require.NoError(t, err)
			require.Equal(t, "m", id)
			require.ErrorIs(t, manager.ReplaceExecution(ctx, "s", "m", "other", "new", "next"), ErrLiveRunExists)
			require.NoError(t, manager.ReplaceExecution(ctx, "s", "m", "r", "new", "next"))
			require.NoError(t, manager.ReleaseExecution(ctx, "s", "m", "r"))
			id, _, err = manager.PeekExecution(ctx, "s")
			require.NoError(t, err)
			require.Equal(t, "new", id)
			require.ErrorIs(t, manager.RenewExecution(ctx, "s", "m", "r"), ErrLiveRunExists)
			require.NoError(t, manager.ReleaseExecution(ctx, "s", "new", "next"))
			id, _, err = manager.PeekExecution(ctx, "s")
			require.NoError(t, err)
			require.Empty(t, id)
		})
	}
}

func TestPeekExecutionDoesNotRenewTTL(t *testing.T) {
	mgr, mini := newTestRedisStreamManager(t, time.Minute)
	ctx := context.Background()
	require.NoError(t, mgr.ClaimExecution(ctx, "s", "m", "r"))
	mini.FastForward(40 * time.Second)
	_, _, err := mgr.PeekExecution(ctx, "s")
	require.NoError(t, err)
	mini.FastForward(21 * time.Second)
	id, _, err := mgr.PeekExecution(ctx, "s")
	require.NoError(t, err)
	require.Empty(t, id)
}

func TestExecutionClaimHasOneWinner(t *testing.T) {
	mgr, _ := newTestRedisStreamManager(t, time.Minute)
	for name, manager := range map[string]interfaces.StreamManager{"redis": mgr, "memory": NewMemoryStreamManager()} {
		t.Run(name, func(t *testing.T) {
			var winners atomic.Int32
			var wg sync.WaitGroup
			for i := range 20 {
				wg.Go(func() {
					if manager.ClaimExecution(t.Context(), "session", fmt.Sprintf("message-%d", i), fmt.Sprintf("request-%d", i)) == nil {
						winners.Add(1)
					}
				})
			}
			wg.Wait()
			require.Equal(t, int32(1), winners.Load())
		})
	}
}
