package agent

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCollectKnowledgeReferencesDeduplicatesChunkIDs(t *testing.T) {
	existing := &types.SearchResult{ID: "chunk-1"}
	newRef := &types.SearchResult{ID: "chunk-2"}
	state := &types.AgentState{KnowledgeRefs: []*types.SearchResult{existing}}
	toolCalls := []types.ToolCall{
		{Result: &types.ToolResult{KnowledgeReferences: []*types.SearchResult{existing, newRef, nil}}},
		{Result: &types.ToolResult{KnowledgeReferences: []*types.SearchResult{newRef}}},
		{Result: nil},
	}

	added := collectKnowledgeReferences(state, toolCalls)

	require.Equal(t, []*types.SearchResult{newRef}, added)
	require.Equal(t, []*types.SearchResult{existing, newRef}, state.KnowledgeRefs)
}
