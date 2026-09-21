package agent

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestMaxIterationsFallbackPrefersLatestSuccessfulRCAReport(t *testing.T) {
	state := &types.AgentState{RoundSteps: []types.AgentStep{
		{ToolCalls: []types.ToolCall{{
			Name:   "submit_rca_report",
			Args:   map[string]interface{}{"report": "较早报告"},
			Result: &types.ToolResult{Success: true},
		}}},
		{ToolCalls: []types.ToolCall{{
			Name:   "rca__submit_rca_report",
			Args:   map[string]interface{}{"report": "最终报告"},
			Result: &types.ToolResult{Success: true},
		}}},
	}}

	require.Equal(t, "最终报告", maxIterationsFallback(state))
}

func TestMaxIterationsFallbackUsesChineseWithoutSuccessfulRCAReport(t *testing.T) {
	tests := []struct {
		name string
		call types.ToolCall
	}{
		{
			name: "failed tool",
			call: types.ToolCall{
				Name:   "submit_rca_report",
				Args:   map[string]interface{}{"report": "失败报告"},
				Result: &types.ToolResult{Success: false},
			},
		},
		{
			name: "empty report",
			call: types.ToolCall{
				Name:   "submit_rca_report",
				Args:   map[string]interface{}{"report": "  "},
				Result: &types.ToolResult{Success: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &types.AgentState{RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{tt.call}}}}
			require.Equal(t, "抱歉，暂时无法生成完整分析结果，请稍后重试。", maxIterationsFallback(state))
		})
	}
}

func TestSuccessfulRCAReportAcceptsNamespacedToolAndTrimsOuterWhitespace(t *testing.T) {
	report, ok := successfulRCAReport([]types.ToolCall{
		{
			Name:   "ops__submit_rca_report",
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
			Name: "rca__submit_rca_report",
			Args: map[string]any{"report": "# 目标报告"},
		},
		Result: &types.ToolResult{Success: true},
	}})

	require.True(t, ok)
	require.Equal(t, "# 目标报告", report)
}
