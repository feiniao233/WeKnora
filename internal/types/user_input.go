package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const RequestUserInputToolName = "request_user_input"

type UserInputOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type UserInputQuestion struct {
	ID       string            `json:"id"`
	Question string            `json:"question"`
	Options  []UserInputOption `json:"options,omitempty"`
}
type UserInputRequest struct {
	ID        string              `json:"id"`
	Reason    string              `json:"reason,omitempty"`
	Questions []UserInputQuestion `json:"questions"`
}
type UserInputResponse struct {
	QuestionID string `json:"question_id"`
	OptionID   string `json:"option_id,omitempty"`
	Text       string `json:"text,omitempty"`
	Skipped    bool   `json:"skipped,omitzero"`
}
type UserInputAnswer struct {
	SourceMessageID string              `json:"source_message_id"`
	RequestID       string              `json:"request_id"`
	Answers         []UserInputResponse `json:"answers"`
}

func userInputText(s string, limit int, multiline bool) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit && !strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t' || r == '\r'))
	})
}
func (r UserInputRequest) Validate() error {
	if uuid.Validate(r.ID) != nil || len(r.Questions) < 1 || len(r.Questions) > 3 || (r.Reason != "" && !userInputText(r.Reason, 500, true)) {
		return errors.New("问题请求无效")
	}
	seen := map[string]bool{}
	for _, q := range r.Questions {
		if !userInputText(q.ID, 64, false) || seen[q.ID] || !userInputText(q.Question, 300, false) || (len(q.Options) != 0 && (len(q.Options) < 2 || len(q.Options) > 5)) {
			return errors.New("问题数量、标识或内容无效")
		}
		seen[q.ID] = true
		ids, labels := map[string]bool{}, map[string]bool{}
		for _, o := range q.Options {
			if !userInputText(o.ID, 64, false) || ids[o.ID] || !userInputText(o.Label, 200, false) || labels[strings.TrimSpace(o.Label)] {
				return errors.New("选项无效或重复")
			}
			ids[o.ID] = true
			labels[strings.TrimSpace(o.Label)] = true
		}
	}
	return nil
}

// AnswerText uses stored questions and labels. Answers remain user claims,
// never verified operational evidence or a replacement for an RCA result.
func (r UserInputRequest) AnswerText(answer UserInputAnswer) (string, error) {
	if r.Validate() != nil || uuid.Validate(answer.SourceMessageID) != nil || answer.RequestID != r.ID || len(answer.Answers) != len(r.Questions) {
		return "", errors.New("补充回答与问题请求不匹配")
	}
	byID := map[string]UserInputResponse{}
	for _, a := range answer.Answers {
		if _, exists := byID[a.QuestionID]; exists {
			return "", errors.New("问题回答重复")
		}
		byID[a.QuestionID] = a
	}
	var out strings.Builder
	out.WriteString("补充信息（用户提供，尚未核实）\n")
	for _, q := range r.Questions {
		a, ok := byID[q.ID]
		if !ok {
			return "", errors.New("缺少问题回答")
		}
		text := strings.TrimSpace(a.Text)
		if a.Skipped {
			if a.OptionID != "" || text != "" {
				return "", errors.New("跳过的问题不能同时填写答案")
			}
			fmt.Fprintf(&out, "\n%s\n未补充（跳过）\n", q.Question)
			continue
		}
		if text != "" && !userInputText(text, 1000, true) {
			return "", errors.New("自填回答无效或超过1000字")
		}
		label := ""
		if a.OptionID != "" {
			i := slices.IndexFunc(q.Options, func(o UserInputOption) bool { return o.ID == a.OptionID })
			if i < 0 {
				return "", errors.New("所选答案不属于当前问题")
			}
			label = q.Options[i].Label
		}
		if label == "" && text == "" {
			return "", errors.New("请选择、填写或跳过当前问题")
		}
		fmt.Fprintf(&out, "\n%s\n%s", q.Question, label)
		if label != "" && text != "" {
			out.WriteString("；")
		}
		out.WriteString(text)
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String()), nil
}

func UserInputRequestFromSteps(steps AgentSteps) *UserInputRequest {
	var found *UserInputRequest
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Name != RequestUserInputToolName || call.Target != nil || call.Result == nil || !call.Result.Success {
				continue
			}
			raw, err := json.Marshal(call.Result.Data["user_input_request"])
			if err != nil {
				continue
			}
			var request UserInputRequest
			if json.Unmarshal(raw, &request) != nil || request.Validate() != nil {
				continue
			}
			if found != nil {
				return nil
			}
			found = &request
		}
	}
	return found
}
