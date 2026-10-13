package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

func (s *messageService) projectReadMessages(ctx context.Context, sessionID string, messages []*types.Message) []*types.Message {
	for _, message := range messages {
		if message != nil {
			message.UserInputAnswer = message.ExecutionContext.UserInputAnswer
		}
	}
	return s.clarifyReadArtifactVersions(ctx, sessionID, messages)
}
