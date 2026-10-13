package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

type RequestUserInputTool struct{ BaseTool }

func NewRequestUserInputTool() *RequestUserInputTool {
	return &RequestUserInputTool{NewBaseTool(ToolRequestUserInput, "Ask 1–3 questions only when missing user information changes the next decision. Use options for choices or omit options for free text. Do not ask users to choose the cause or disclose passwords, API keys, tokens or other credentials. This tool MUST be called alone, after available evidence queries; success ends this turn and waits for the next user message. Never record a final diagnosis in the same turn as a question request.", json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"reason":{"type":"string","maxLength":500},"questions":{"type":"array","minItems":1,"maxItems":3,"items":{"type":"object","additionalProperties":false,"properties":{"question":{"type":"string","minLength":1,"maxLength":300},"options":{"type":"array","minItems":2,"maxItems":5,"items":{"type":"string","minLength":1,"maxLength":200}}},"required":["question"]}}},"required":["questions"]}`))}
}
func (t *RequestUserInputTool) Execute(_ context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input struct {
		Reason    string `json:"reason"`
		Questions []struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		} `json:"questions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return &types.ToolResult{Success: false, Error: "问题参数无效"}, nil
	}
	if decoder.Decode(new(any)) != io.EOF {
		return &types.ToolResult{Success: false, Error: "问题参数包含多余内容"}, nil
	}
	request := types.UserInputRequest{ID: uuid.NewString(), Reason: strings.TrimSpace(input.Reason)}
	for i, q := range input.Questions {
		item := types.UserInputQuestion{ID: fmt.Sprintf("q%d", i+1), Question: strings.TrimSpace(q.Question)}
		for j, label := range q.Options {
			item.Options = append(item.Options, types.UserInputOption{ID: fmt.Sprintf("o%d", j+1), Label: strings.TrimSpace(label)})
		}
		request.Questions = append(request.Questions, item)
	}
	if err := request.Validate(); err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &types.ToolResult{Success: true, Output: "请补充下面的信息，我会根据你的回答继续分析。", Data: map[string]any{"user_input_request": request}}, nil
}
