package session

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

// Called under the existing turn lease and before either message is saved.
// The newest completed question is the only valid target; accepting an answer
// creates a user message, so duplicates and superseded questions fail closed.
func (h *Handler) resolveUserInputAnswer(ctx context.Context, req *qaRequestContext) error {
	if req.userInputAnswer == nil {
		return nil
	}
	if !req.userInputEnabled || req.customAgent == nil {
		return fmt.Errorf("当前助手不支持补充问题")
	}
	messages, err := h.messageService.GetRecentMessagesBySession(ctx, req.sessionID, 1)
	if err != nil || len(messages) != 1 || messages[0] == nil {
		return fmt.Errorf("无法读取待回答问题，请刷新后重试")
	}
	source := messages[0]
	if source.ID != req.userInputAnswer.SourceMessageID || source.SessionID != req.sessionID || source.Role != "assistant" || !source.IsCompleted || source.AgentID != req.customAgent.ID || source.ExecutionResult == nil || source.ExecutionResult.Status != "completed" {
		return fmt.Errorf("问题已回答、已过期或不属于当前会话，请刷新后重试")
	}
	questions := types.UserInputRequestFromSteps(source.AgentSteps)
	if questions == nil {
		return fmt.Errorf("来源消息没有有效的待回答问题")
	}
	text, err := questions.AnswerText(*req.userInputAnswer)
	if err != nil {
		return err
	}
	req.query = "请结合上一轮分析的对象与下面的补充信息继续分析，核对可用证据后再判断。\n\n" + text
	return nil
}
