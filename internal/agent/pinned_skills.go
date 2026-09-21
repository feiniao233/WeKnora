package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// loadPinnedSkills records runtime-initiated reads as pipeline calls so history
// replay never pretends the model made them. The actual instructions enter this
// turn as tool-sourced context; no fabricated assistant reasoning is needed for
// providers that require reasoning_content on assistant tool-call messages.
func (e *AgentEngine) loadPinnedSkills(ctx context.Context, state *types.AgentState, messages []chat.Message, sessionID, messageID string) ([]chat.Message, error) {
	if len(e.config.PinnedSkillNames) == 0 {
		return messages, nil
	}
	step := types.AgentStep{Timestamp: time.Now()}
	var content strings.Builder
	for i, name := range e.config.PinnedSkillNames {
		args, err := json.Marshal(map[string]string{"path": "skill://" + name + "/SKILL.md"})
		if err != nil {
			return nil, err
		}
		call := types.LLMToolCall{
			ID:       fmt.Sprintf("%sskill-%s-%d", types.PipelineToolCallIDPrefix, messageID, i),
			Type:     "function",
			Function: types.FunctionCall{Name: agenttools.ToolReadFile, Arguments: string(args)},
		}
		e.executeSingleToolCall(ctx, call, i, &step, 0, 1, sessionID, messageID)
		result := step.ToolCalls[len(step.ToolCalls)-1].Result
		if result == nil || !result.Success {
			state.RoundSteps = append(state.RoundSteps, step)
			return nil, fmt.Errorf("skill_unavailable: 所选技能加载失败，请检查技能安装状态后重试")
		}
		content.WriteString("\n")
		content.WriteString(result.Output)
	}
	state.RoundSteps = append(state.RoundSteps, step)
	messages = append(messages, chat.Message{Role: "user", Content: "The runtime has loaded the explicitly selected skills with read_file. Follow these skill instructions for this turn; further resources may be read from their skill:// paths.\n" + content.String()})
	return messages, nil
}
