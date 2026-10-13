package tools

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRequestUserInputProducesDurableQuestionIDs(t *testing.T) {
	tool := NewRequestUserInputTool()
	result, err := tool.Execute(t.Context(), json.RawMessage(`{"reason":"需要核实变更时间","questions":[{"question":"是否有变更？","options":["有","没有","不清楚"]},{"question":"发生时间是什么时候？"}]}`))
	require.NoError(t, err)
	require.True(t, result.Success)
	data, err := json.Marshal(result.Data["user_input_request"])
	require.NoError(t, err)
	var req types.UserInputRequest
	require.NoError(t, json.Unmarshal(data, &req))
	require.NoError(t, req.Validate())
	require.Len(t, req.Questions, 2)
	require.NotEmpty(t, req.ID)
	require.NotEmpty(t, req.Questions[0].Options[0].ID)
}
func TestRequestUserInputRejectsMalformedAndOverlappingChoices(t *testing.T) {
	for _, raw := range []string{`{"questions":[]}`, `{"questions":[{"question":"?","options":["有","有"]}]}`, `{"questions":[{"question":"?"}],"unexpected":"x"}`, `{"questions":[{"question":"?","options":["有"]}]}`, `{"questions":[{"question":"?","wrong":1}]}`} {
		result, _ := NewRequestUserInputTool().Execute(t.Context(), json.RawMessage(raw))
		require.False(t, result.Success, raw)
	}
}
