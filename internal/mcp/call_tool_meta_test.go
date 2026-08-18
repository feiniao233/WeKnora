package mcp

import (
	"context"
	"testing"
)

func TestNewCallToolRequestIncludesContextMeta(t *testing.T) {
	ctx := WithCallToolMeta(context.Background(), map[string]any{"weknora/session_id": "session-1"})
	req := newCallToolRequest(ctx, "submit_rca_report", map[string]any{"report": "report"})

	if req.Params.Meta == nil || req.Params.Meta.AdditionalFields["weknora/session_id"] != "session-1" {
		t.Fatalf("call metadata was not forwarded: %#v", req.Params.Meta)
	}
}
