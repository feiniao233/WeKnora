package agent

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSuccessfulRCAReportAcceptsDirectToolAndTrimsOuterWhitespace(t *testing.T) {
	report, ok := successfulRCAReport([]types.ToolCall{
		{
			Name:   "submit_rca_report",
			Args:   map[string]any{"report": "  # 根因分析报告\n\n正文  \n"},
			Result: &types.ToolResult{Success: true},
		},
	})

	require.True(t, ok)
	require.Equal(t, "# 根因分析报告\n\n正文", report)
}

func TestSuccessfulRCAReportUsesResolvedMCPCallTarget(t *testing.T) {
	report, ok := successfulRCAReport([]types.ToolCall{{
		Name: "call_mcp_tool",
		Args: map[string]any{"tool_ref": "opaque"},
		Target: &types.ToolCallTarget{
			Name:     "mcp_Steel_submit_rca_report_0123456789abcdef",
			ToolName: "submit_rca_report",
			Args:     map[string]any{"report": "# 目标报告"},
		},
		Result: &types.ToolResult{Success: true},
	}})

	require.True(t, ok)
	require.Equal(t, "# 目标报告", report)
}

func TestSuccessfulRCAReportRejectsRegisteredNameLookalike(t *testing.T) {
	_, ok := successfulRCAReport([]types.ToolCall{{
		Name:   "mcp_Steel_submit_rca_report_0123456789abcdef",
		Args:   map[string]any{"report": "不应采纳"},
		Result: &types.ToolResult{Success: true},
	}})

	require.False(t, ok)
}
