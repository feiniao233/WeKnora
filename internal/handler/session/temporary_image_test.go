package session

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type imageModels struct {
	interfaces.ModelService
	vision    bool
	kind      types.ModelType
	requested string
	tenant    uint64
	denied    bool
	vlmFail   bool
}

func (s *imageModels) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	if id == "vlm" {
		return &types.Model{Type: types.ModelTypeVLLM}, nil
	}
	s.requested = id
	s.tenant = types.MustTenantIDFromContext(ctx)
	if s.denied {
		return nil, errors.New("denied")
	}
	return &types.Model{Type: s.kind, Parameters: types.ModelParameters{SupportsVision: s.vision}}, nil
}
func (s *imageModels) GetVLMModel(context.Context, string) (vlm.VLM, error) { return s, nil }
func (s *imageModels) GetModelID() string                                   { return "vlm" }
func (s *imageModels) GetModelName() string                                 { return "vlm" }
func (s *imageModels) Predict(context.Context, [][]byte, string) (string, error) {
	if s.vlmFail {
		return "", errors.New("failed")
	}
	return "parsed screenshot", nil
}

type imageDocuments struct {
	interfaces.TemporaryDocumentService
	data   []byte
	denied bool
}

func (s *imageDocuments) OpenFile(_ context.Context, tenant uint64, session, id string) (io.ReadCloser, string, error) {
	if s.denied || tenant != 42 || session != "session" {
		return nil, "", errors.New("denied")
	}
	return io.NopCloser(bytes.NewReader(s.data)), "image.png", nil
}
func TestTemporaryImageCurrentModelRouting(t *testing.T) {
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	for _, tc := range []struct {
		name                                string
		vision, vlm, fail, denied, disabled bool
		text                                string
		wantErr                             bool
	}{
		{name: "direct without VLM", vision: true}, {name: "changed to nonvision", wantErr: true},
		{name: "changed model with fallback", vlm: true}, {name: "failed fallback", vlm: true, fail: true, wantErr: true},
		{name: "existing parsed VLM", text: "existing OCR"}, {name: "wrong session", vision: true, denied: true, wantErr: true},
		{name: "disabled images", vision: true, disabled: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models := &imageModels{vision: tc.vision, kind: types.ModelTypeKnowledgeQA, vlmFail: tc.fail}
			h := &Handler{modelService: models, temporaryDocuments: &imageDocuments{data: pngBytes.Bytes(), denied: tc.denied}}
			req := &qaRequestContext{sessionID: "session", session: &types.Session{TenantID: 42}, summaryModelID: "override", customAgent: &types.CustomAgent{Config: types.CustomAgentConfig{ModelID: "default", ImageUploadEnabled: !tc.disabled}}}
			if tc.vlm {
				req.customAgent.Config.VLMModelID = "vlm"
			}
			result := &types.TemporaryDocumentPromptResult{Attachments: types.MessageAttachments{{ID: "image", URL: "resource://one", FileType: ".png", Content: tc.text}}, ImageURLs: []string{"resource://one"}}
			ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(84))
			err := h.prepareTemporaryImages(ctx, req, result)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "override", models.requested)
			require.Equal(t, uint64(84), models.tenant)
			if tc.vision {
				require.Len(t, result.ImageURLs, 1)
				require.Contains(t, result.ImageURLs[0], "data:image/png;base64,")
				req.assistantMessage = &types.Message{ID: "answer"}
				req.images = []ImageAttachment{{URL: result.ImageURLs[0]}}
				require.Equal(t, result.ImageURLs, req.buildQARequest().ImageURLs)
			} else {
				require.Empty(t, result.ImageURLs)
				require.NotEmpty(t, result.Attachments[0].Content)
			}
		})
	}
}
func TestTemporaryImageModelValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   types.ModelType
		denied bool
	}{{"wrong type", types.ModelTypeEmbedding, false}, {"permission", types.ModelTypeKnowledgeQA, true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{modelService: &imageModels{vision: true, kind: tc.kind, denied: tc.denied}}
			_, err := h.attachmentChatSupportsVision(context.WithValue(t.Context(), types.TenantIDContextKey, uint64(42)), nil, "model")
			require.Error(t, err)
		})
	}
}
