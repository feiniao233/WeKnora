package service

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type imageDocumentRepo struct {
	interfaces.TemporaryDocumentRepository
}

func (s *imageDocumentRepo) GetScoped(_ context.Context, tenant uint64, session, id string) (*types.TemporaryDocument, error) {
	if tenant != 42 || session != "session" {
		return nil, nil
	}
	return &types.TemporaryDocument{ID: id, ResourceRef: "resource://" + id, FileType: ".png", Status: types.TemporaryDocumentStatusReady}, nil
}
func TestDirectVisionPreparesOriginalWithoutParserOrVLM(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	svc := &temporaryDocumentService{fileService: &stagingFileService{files: map[string][]byte{"source": buf.Bytes()}}}
	doc := &types.TemporaryDocument{ResourceRef: "source", FileType: ".png", ProcessingOptions: types.JSON(`{"direct_vision":true}`)}
	text, images, meta, err := svc.parse(t.Context(), doc)
	require.NoError(t, err)
	require.Empty(t, text)
	require.Len(t, images, 1)
	require.Equal(t, "source", images[0].URL)
	require.Equal(t, "direct_vision", meta["parser"])
	svc.fileService = &stagingFileService{files: map[string][]byte{"source": []byte("invalid")}}
	_, _, _, err = svc.parse(t.Context(), doc)
	require.Error(t, err)
}
func TestDirectVisionRetainsFiveImagesAndScope(t *testing.T) {
	svc := &temporaryDocumentService{repo: &imageDocumentRepo{}}
	ids := []string{"a", "b", "c", "d", "e"}
	got, err := svc.ResolveForPrompt(t.Context(), 42, "session", ids, "describe")
	require.NoError(t, err)
	require.Len(t, got.ImageURLs, 5)
	_, err = svc.ResolveForPrompt(t.Context(), 42, "session", append(ids, "f"), "describe")
	require.Error(t, err)
	_, err = svc.ResolveForPrompt(t.Context(), 43, "session", ids, "describe")
	require.Error(t, err)
}

func TestEffectiveAgentModelCannotDropDirectImage(t *testing.T) {
	for _, tc := range []struct {
		name         string
		vision       bool
		ext, content string
		wantErr      bool
	}{
		{name: "direct image on vision model", vision: true, ext: ".png"},
		{name: "model capability changed", ext: ".png", wantErr: true},
		{name: "jpeg capability changed", ext: ".jpeg", wantErr: true},
		{name: "legacy OCR", ext: ".png", content: "previously parsed screenshot"},
		{name: "optional document image", ext: ".pdf"},
		{name: "tool image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &types.QARequest{ImageURLs: []string{"data:image/png;base64,eA=="}}
			if tc.ext != "" {
				req.Attachments = types.MessageAttachments{{FileType: tc.ext, Content: tc.content}}
			}
			err := validateAgentImageCapability(req, tc.vision)
			if tc.wantErr {
				require.ErrorContains(t, err, "no longer supports image input")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
