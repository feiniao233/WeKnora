package session

import (
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/gin-gonic/gin"
	"net/http"
)

// GetExecutionState is a read-only, owner-scoped view of the execution lease.
func (h *Handler) GetExecutionState(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	if _, err := h.sessionService.GetOwnedSession(ctx, id); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	messageID, requestID, err := h.streamManager.PeekExecution(ctx, id)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Execution state unavailable"})
		return
	}
	if messageID != "" {
		msg, err := h.messageService.GetMessage(ctx, id, messageID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Execution state unavailable"})
			return
		}
		if msg == nil || msg.IsCompleted {
			if err := h.streamManager.ReleaseExecution(ctx, id, messageID, requestID); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Execution state unavailable"})
				return
			}
			// A handoff may have replaced the terminal lease during reconciliation.
			messageID, requestID, err = h.streamManager.PeekExecution(ctx, id)
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Execution state unavailable"})
				return
			}
		}
	}
	data := gin.H{"running": messageID != ""}
	if messageID != "" {
		data["message_id"] = messageID
		data["request_id"] = requestID
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// GetMessageExecutionContext deliberately exposes only reproducible scope and provenance.
func (h *Handler) GetMessageExecutionContext(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	if _, err := h.sessionService.GetOwnedSession(ctx, id); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	msg, err := h.messageService.GetMessage(ctx, id, c.Param("message_id"))
	if err != nil || msg == nil || msg.SessionID != id {
		c.Error(apperrors.NewNotFoundError("Message not found"))
		return
	}
	snapshot := msg.ExecutionContext
	snapshot.LangfuseTraceparent = ""
	snapshot.QuestionSuggestions = nil
	snapshot.SuggestionAttribution = nil
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"action_id": msg.ExecutionContext.ActionID,
		"id":        msg.ID, "session_id": msg.SessionID, "role": msg.Role, "is_completed": msg.IsCompleted,
		"agent_id": msg.AgentID, "model_id": msg.ModelID, "execution_result": msg.ExecutionResult, "execution_context": snapshot,
	}})
}
