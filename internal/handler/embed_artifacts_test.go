package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type stubMessageServiceForEmbedArtifacts struct {
	interfaces.MessageService
	message *types.Message
}

func (s *stubMessageServiceForEmbedArtifacts) GetMessage(_ context.Context, _, _ string) (*types.Message, error) {
	return s.message, nil
}

func TestEmbedListMessageArtifactsRequiresSignedSessionAndHidesStorageURL(t *testing.T) {
	ch := testEmbedChannel()
	sess := validEmbedSession(ch)
	sessionSvc := &stubSessionServiceForEmbed{sessions: map[string]*types.Session{sess.ID: sess}}
	messageSvc := &stubMessageServiceForEmbedArtifacts{message: &types.Message{
		ID:        "message-1",
		SessionID: sess.ID,
		Artifacts: types.MessageArtifacts{{
			URL:      "local://private/report.html",
			FileName: "report.html",
			FileType: ".html",
			FileSize: 42,
		}},
	}}
	sessionHandler := session.NewHandler(
		sessionSvc, messageSvc, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	embedHandler := &EmbedChannelHandler{sessionService: sessionSvc, sessionHandler: sessionHandler}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.EmbedChannelContextKey, ch)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.GET("/sessions/:session_id/messages/:message_id/artifacts", embedHandler.EmbedListMessageArtifacts)

	for _, tc := range []struct {
		name       string
		signature  string
		wantStatus int
	}{
		{name: "valid", signature: service.SignEmbedSessionHandle(ch, sess.ID), wantStatus: http.StatusOK},
		{name: "invalid signature", signature: "invalid", wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID+"/messages/message-1/artifacts", nil)
			req.Header.Set("X-Embed-Session", tc.signature)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus == http.StatusOK {
				if !strings.Contains(w.Body.String(), "report.html") {
					t.Fatalf("response missing artifact metadata: %s", w.Body.String())
				}
				if strings.Contains(w.Body.String(), "local://private") {
					t.Fatalf("response leaked storage URL: %s", w.Body.String())
				}
			}
		})
	}
}
