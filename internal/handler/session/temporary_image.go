package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func (h *Handler) attachmentChatSupportsVision(ctx context.Context, agent *types.CustomAgent, modelID string) (bool, error) {
	if modelID == "" && agent != nil {
		modelID = agent.Config.ModelID
	}
	if modelID == "" || h.modelService == nil {
		return false, fmt.Errorf("chat model is not configured")
	}
	model, err := h.modelService.GetModelByID(ctx, modelID)
	if err != nil || model == nil || model.Type != types.ModelTypeKnowledgeQA {
		return false, fmt.Errorf("requested chat model is unavailable")
	}
	return model.Parameters.SupportsVision, nil
}

// prepareTemporaryImages rechecks the current turn's model, not the model used
// at upload time. File bytes are opened only through the session-scoped service.
func (h *Handler) prepareTemporaryImages(ctx context.Context, req *qaRequestContext, result *types.TemporaryDocumentPromptResult) error {
	hasImages := false
	for _, att := range result.Attachments {
		switch strings.ToLower(att.FileType) {
		case ".png", ".jpg", ".jpeg":
			hasImages = true
		}
	}
	if !hasImages {
		return nil
	}
	if req.customAgent == nil || !req.customAgent.Config.ImageUploadEnabled {
		return fmt.Errorf("image upload is not enabled")
	}
	vision, err := h.attachmentChatSupportsVision(ctx, req.customAgent, req.summaryModelID)
	if err != nil {
		return err
	}
	// Replace standalone resource references with authenticated bytes. This
	// avoids external model servers depending on private storage URLs.
	standalone := make(map[string]bool)
	for _, att := range result.Attachments {
		switch strings.ToLower(att.FileType) {
		case ".png", ".jpg", ".jpeg":
			standalone[att.URL] = true
		}
	}
	kept := result.ImageURLs[:0]
	for _, url := range result.ImageURLs {
		if !standalone[url] {
			kept = append(kept, url)
		}
	}
	result.ImageURLs = kept
	for i := range result.Attachments {
		att := &result.Attachments[i]
		switch strings.ToLower(att.FileType) {
		case ".png", ".jpg", ".jpeg":
		default:
			continue
		}
		if !vision && strings.TrimSpace(att.Content) != "" {
			continue
		}
		if !vision && req.customAgent.Config.VLMModelID == "" {
			return fmt.Errorf("selected chat model cannot read images and no vision model is configured")
		}
		file, _, err := h.temporaryDocuments.OpenFile(ctx, req.session.TenantID, req.sessionID, att.ID)
		if err != nil {
			return fmt.Errorf("image attachment is unavailable: %w", err)
		}
		limit := secutils.GetMaxFileSizeMB() * 1024 * 1024
		data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
		file.Close()
		if readErr != nil || len(data) == 0 || int64(len(data)) > limit {
			return fmt.Errorf("image attachment could not be read")
		}
		mime := http.DetectContentType(data)
		if mime != "image/png" && mime != "image/jpeg" {
			return fmt.Errorf("invalid image attachment content")
		}
		if vision {
			result.ImageURLs = append(result.ImageURLs, "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(data))
		} else {
			if err := h.validateAttachmentVLM(ctx, req.customAgent.Config.VLMModelID); err != nil {
				return err
			}
			model, err := h.modelService.GetVLMModel(ctx, req.customAgent.Config.VLMModelID)
			if err != nil {
				return fmt.Errorf("vision model is unavailable: %w", err)
			}
			text, err := model.Predict(ctx, [][]byte{data}, buildImageAnalysisPrompt(req.query))
			if err != nil || strings.TrimSpace(text) == "" {
				return fmt.Errorf("image analysis failed")
			}
			att.Content = text
		}
	}
	if len(result.ImageURLs)+len(req.images) > types.MaxTemporaryAttachmentsPerMessage {
		return fmt.Errorf("too many images in this turn")
	}
	return nil
}

func (h *Handler) validateAttachmentVLM(ctx context.Context, id string) error {
	if h.modelService == nil || id == "" {
		return fmt.Errorf("vision model is not configured")
	}
	model, err := h.modelService.GetModelByID(ctx, id)
	if err != nil || model == nil || model.Type != types.ModelTypeVLLM {
		return fmt.Errorf("vision model is unavailable")
	}
	return nil
}
