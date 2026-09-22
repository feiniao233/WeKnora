package session

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type boundExecutionSession struct{ interfaces.SessionService }

func (boundExecutionSession) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return &types.Session{ID: "s", AgentID: "bound"}, nil
}

func TestChatRejectsBoundAgentMismatchBeforeAgentLookup(t *testing.T) {
	h := &Handler{sessionService: boundExecutionSession{}}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.POST("/chat/:session_id", h.AgentQA)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat/s", strings.NewReader(`{"query":"hello","agent_id":"other"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "immutable")
}

func TestChatRejectsInvalidActionIDBeforeSessionLookup(t *testing.T) {
	for _, action := range []string{"RCA", "report/action", "with space", strings.Repeat("a", 65)} {
		h := &Handler{}
		r := gin.New()
		r.Use(middleware.ErrorHandler())
		r.POST("/chat/:session_id", h.AgentQA)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/chat/s", strings.NewReader(`{"query":"hello","action_id":"`+action+`"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "invalid action_id")
	}
	for _, action := range []string{"", "generate_rca_report", "run-2", strings.Repeat("a", 64)} {
		require.True(t, validActionID(action))
	}
}

func TestActionMetadataIsSavedAndExposedWithoutPrivateContext(t *testing.T) {
	h := &Handler{sessionService: &steerOwnedSessionStub{}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "session_id", Value: "s"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/chat/s", strings.NewReader(`{"query":"hello","action_id":"generate_rca_report"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	request, _, err := h.parseQARequest(c, "test")
	require.NoError(t, err)
	msg := request.assistantMessage
	require.Equal(t, "generate_rca_report", msg.ExecutionContext.ActionID)
	msg.ExecutionContext.LangfuseTraceparent = "private-trace"
	h.messageService = &steerMessageLookupStub{msg: msg}
	w := httptest.NewRecorder()
	executionRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sessions/s/messages/m/execution-context", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"action_id":"generate_rca_report"`)
	require.NotContains(t, w.Body.String(), "private-trace")
}

type deniedExecutionSession struct{ interfaces.SessionService }

func (deniedExecutionSession) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return nil, errors.New("not owned")
}

func executionRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/sessions/:id/execution-state", h.GetExecutionState)
	r.GET("/sessions/:id/messages/:message_id/execution-context", h.GetMessageExecutionContext)
	return r
}

func TestExecutionReadsRequireOwner(t *testing.T) {
	h := &Handler{sessionService: deniedExecutionSession{}}
	for _, path := range []string{"/sessions/s/execution-state", "/sessions/s/messages/m/execution-context"} {
		w := httptest.NewRecorder()
		executionRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}

func TestExecutionStateReconcilesTerminalAndReportsStorageFailure(t *testing.T) {
	mgr := stream.NewMemoryStreamManager()
	require.NoError(t, mgr.ClaimExecution(t.Context(), "s", "m", "r"))
	h := &Handler{sessionService: &steerOwnedSessionStub{}, messageService: &steerMessageLookupStub{msg: &types.Message{ID: "m", SessionID: "s", IsCompleted: true}}, streamManager: mgr}
	w := httptest.NewRecorder()
	executionRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sessions/s/execution-state", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"running":false`)
	id, _, err := mgr.PeekExecution(t.Context(), "s")
	require.NoError(t, err)
	require.Empty(t, id)
	h.streamManager = &steerLiveRunLookupStub{err: errors.New("unavailable")}
	w = httptest.NewRecorder()
	executionRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sessions/s/execution-state", nil))
	require.Equal(t, 503, w.Code)
}

func TestSourceReplayPinsScopeAndRejectsRevocation(t *testing.T) {
	source := &types.Message{ID: "m", SessionID: "s", Role: "assistant", AgentID: "a", ModelID: "model", IsCompleted: true, ExecutionResult: &types.MessageExecutionResult{Status: "completed"}, ExecutionContext: types.MessageExecutionContext{ExecutionConfigHash: "hash", SkillsSelectionMode: "selected", SelectedSkillNames: []string{"rca"}, MCPSelectionMode: "selected", ResolvedMCPServiceIDs: []string{"mcp"}}}
	h := &Handler{messageService: &steerMessageLookupStub{msg: source}}
	s := &types.Session{ID: "s", AgentID: "a"}
	agent := &types.CustomAgent{ID: "a", Config: types.CustomAgentConfig{SkillsSelectionMode: "selected", SelectedSkills: []string{"rca", "other"}, MCPSelectionMode: "selected", MCPServices: []string{"mcp", "other"}}}
	req := &CreateKnowledgeQARequest{SourceMessageID: "m"}
	require.NoError(t, h.applySourceExecution(t.Context(), s, agent, req))
	require.Equal(t, "model", req.SummaryModelID)
	require.Equal(t, []string{"mcp"}, agent.Config.MCPServices)
	require.Equal(t, []string{"rca"}, agent.Config.SelectedSkills)
	agent.Config.SelectedSkills = nil
	require.ErrorContains(t, h.applySourceExecution(t.Context(), s, agent, req), "skill_unavailable")
	source.SessionID = "other"
	require.ErrorContains(t, h.applySourceExecution(t.Context(), s, agent, req), "source message")
}
