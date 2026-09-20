package session

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageUploadSession struct{ interfaces.SessionService }

func (s *imageUploadSession) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return &types.Session{TenantID: 42}, nil
}

type imageUploadDocuments struct {
	interfaces.TemporaryDocumentService
	options types.TemporaryDocumentCreateOptions
	created bool
}

func (s *imageUploadDocuments) Create(_ context.Context, _ uint64, _, _, _ string, _ int64, _ io.Reader, options types.TemporaryDocumentCreateOptions) (*types.TemporaryDocument, error) {
	s.options = options
	s.created = true
	return &types.TemporaryDocument{ID: "image"}, nil
}

func TestImageUploadUsesVerifiedModelOverride(t *testing.T) {
	for _, tc := range []struct {
		name, override                 string
		vision, vlm, disabled, wantErr bool
	}{{name: "direct override", override: "override", vision: true}, {name: "agent default", vision: true}, {name: "legacy VLM", vlm: true}, {name: "missing both", wantErr: true}, {name: "disabled", vision: true, disabled: true, wantErr: true}} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			file, err := writer.CreateFormFile("file", "screenshot.png")
			require.NoError(t, err)
			_, err = file.Write([]byte("payload validated by service"))
			require.NoError(t, err)
			require.NoError(t, writer.WriteField("agent_id", "agent"))
			require.NoError(t, writer.WriteField("summary_model_id", tc.override))
			require.NoError(t, writer.WriteField("direct_vision", "true"))
			require.NoError(t, writer.Close())
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set(types.TenantIDContextKey.String(), uint64(42))
			c.Params = gin.Params{{Key: "session_id", Value: "session"}}
			c.Request = httptest.NewRequest(http.MethodPost, "/upload", &body)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			models := &imageModels{vision: tc.vision, kind: types.ModelTypeKnowledgeQA}
			docs := &imageUploadDocuments{}
			agent := &types.CustomAgent{ID: "agent", Config: types.CustomAgentConfig{ModelID: "default", ImageUploadEnabled: !tc.disabled}}
			if tc.vlm {
				agent.Config.VLMModelID = "vlm"
			}
			h := &Handler{sessionService: &imageUploadSession{}, customAgentService: &resolveOwnAgentStub{agent: agent}, modelService: models, temporaryDocuments: docs}
			h.UploadTemporaryDocument(c)
			if tc.wantErr {
				require.NotEmpty(t, c.Errors)
				require.False(t, docs.created)
				return
			}
			require.Empty(t, c.Errors)
			require.Equal(t, http.StatusAccepted, rec.Code)
			require.Equal(t, tc.vision, docs.options.DirectVision)
			require.Equal(t, uint64(42), models.tenant)
			if tc.override != "" {
				require.Equal(t, tc.override, models.requested)
			} else {
				require.Equal(t, "default", models.requested)
			}
		})
	}
}
