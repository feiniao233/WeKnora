package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func inputRequest() UserInputRequest {
	return UserInputRequest{ID: "f83c8a22-6e56-4629-bfab-43c5bbfef293", Questions: []UserInputQuestion{{ID: "q1", Question: "故障前是否改过配置？", Options: []UserInputOption{{ID: "o1", Label: "有变更"}, {ID: "o2", Label: "没有变更"}, {ID: "o3", Label: "不清楚"}}}, {ID: "q2", Question: "发生时间是什么时候？"}}}
}
func inputAnswer() UserInputAnswer {
	return UserInputAnswer{SourceMessageID: "2c797b15-b4c4-41f3-a1a5-c09968c2897e", RequestID: inputRequest().ID, Answers: []UserInputResponse{{QuestionID: "q1", OptionID: "o1", Text: "调整过地址"}, {QuestionID: "q2", Skipped: true}}}
}
func TestUserInputAnswersUseSavedQuestionsAndRemainUserClaims(t *testing.T) {
	req := inputRequest()
	text, err := req.AnswerText(inputAnswer())
	require.NoError(t, err)
	require.Contains(t, text, "故障前是否改过配置？")
	require.Contains(t, text, "有变更")
	require.Contains(t, text, "调整过地址")
	require.Contains(t, text, "跳过")
	require.Contains(t, text, "尚未核实")
	require.NotContains(t, text, req.ID)
	require.NoError(t, req.Validate())
}
func TestUserInputAnswersRejectMissingAmbiguousAndInventedResponses(t *testing.T) {
	cases := []struct {
		name   string
		change func(*UserInputAnswer)
	}{
		{"wrong request", func(a *UserInputAnswer) { a.RequestID = "be1ec461-f9d5-4ef2-8b2b-4630969cd4b6" }},
		{"bad source", func(a *UserInputAnswer) { a.SourceMessageID = "not-a-message" }},
		{"missing answer", func(a *UserInputAnswer) { a.Answers = a.Answers[:1] }},
		{"unknown question", func(a *UserInputAnswer) { a.Answers[0].QuestionID = "q3" }},
		{"duplicate question", func(a *UserInputAnswer) { a.Answers[1].QuestionID = "q1" }},
		{"unknown option", func(a *UserInputAnswer) { a.Answers[0].OptionID = "o9" }},
		{"skip with answer", func(a *UserInputAnswer) { a.Answers[0].Skipped = true }},
		{"blank answer", func(a *UserInputAnswer) { a.Answers[0].OptionID = ""; a.Answers[0].Text = " " }},
		{"long answer", func(a *UserInputAnswer) { a.Answers[0].Text = strings.Repeat("字", 1001) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := inputAnswer()
			tc.change(&a)
			_, err := inputRequest().AnswerText(a)
			require.Error(t, err)
		})
	}
}
func TestUserInputRequestRejectsInvalidOrUnboundedQuestions(t *testing.T) {
	for _, count := range []int{0, 4} {
		req := inputRequest()
		req.Questions = make([]UserInputQuestion, count)
		require.Error(t, req.Validate())
	}
	req := inputRequest()
	req.Questions[1].ID = "q1"
	require.Error(t, req.Validate())
	req = inputRequest()
	req.Questions[0].Options[1].ID = "o1"
	require.Error(t, req.Validate())
	req = inputRequest()
	req.Questions[0].Question = ""
	require.Error(t, req.Validate())
}
