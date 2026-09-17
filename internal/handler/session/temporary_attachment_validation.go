package session

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// validateResolvedAttachments closes the gap between ready-state checks and
// resolution. Every explicitly selected document must actually reach the turn.
func validateResolvedAttachments(ids []string, attachments types.MessageAttachments, agent *types.CustomAgent) error {
	available := make(map[string]bool, len(attachments))
	for _, att := range attachments {
		if agent != nil && len(agent.Config.SupportedFileTypes) > 0 &&
			!containsFileType(agent.Config.SupportedFileTypes, strings.TrimPrefix(strings.ToLower(att.FileType), ".")) {
			return fmt.Errorf("attachment_unavailable: attachment type is not supported by this agent")
		}
		available[att.ID] = true
	}
	for _, id := range ids {
		if !available[id] {
			return fmt.Errorf("attachment_unavailable: selected attachment was not resolved")
		}
	}
	return nil
}
