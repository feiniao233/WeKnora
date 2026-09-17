package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type explicitAttachmentService struct {
	interfaces.TemporaryDocumentService
	docs         map[string]*types.TemporaryDocument
	resolved     *types.TemporaryDocumentPromptResult
	resolveErr   error
	resolveCalls int
}

func (s *explicitAttachmentService) Get(_ context.Context, tenant uint64, session, id string) (*types.TemporaryDocument, error) {
	if tenant != 42 || session != "session" {
		return nil, errors.New("not found")
	}
	return s.docs[id], nil
}
func (s *explicitAttachmentService) ResolveForPrompt(_ context.Context, tenant uint64, session string, ids []string, query string) (*types.TemporaryDocumentPromptResult, error) {
	s.resolveCalls++
	return s.resolved, s.resolveErr
}

func TestExplicitAttachmentsCannotBeSilentlyDropped(t *testing.T) {
	for _, tc := range []struct {
		name, status                                string
		missing, resolveFailure, partial, wrongType bool
		wantResolve                                 int
		wantErr                                     bool
	}{
		{name: "ready", status: types.TemporaryDocumentStatusReady, wantResolve: 1},
		{name: "failed", status: types.TemporaryDocumentStatusFailed, wantErr: true},
		{name: "missing or out of scope", missing: true, wantErr: true},
		{name: "processing timeout", status: types.TemporaryDocumentStatusProcessing, wantErr: true},
		{name: "failed after readiness check", status: types.TemporaryDocumentStatusReady, resolveFailure: true, wantResolve: 1, wantErr: true},
		{name: "partial resolution", status: types.TemporaryDocumentStatusReady, partial: true, wantResolve: 1, wantErr: true},
		{name: "unsupported type", status: types.TemporaryDocumentStatusReady, wrongType: true, wantResolve: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &explicitAttachmentService{docs: map[string]*types.TemporaryDocument{}, resolved: &types.TemporaryDocumentPromptResult{Attachments: types.MessageAttachments{{ID: "doc", FileType: "txt", Content: "evidence"}}}}
			if !tc.missing {
				svc.docs["doc"] = &types.TemporaryDocument{ID: "doc", Status: tc.status}
			}
			if tc.partial {
				svc.resolved.Attachments = nil
			}
			if tc.resolveFailure {
				svc.resolveErr = errors.New("private backend details")
			}
			req := &qaRequestContext{sessionID: "session", session: &types.Session{TenantID: 42}, attachmentIDs: []string{"doc"}, customAgent: &types.CustomAgent{Config: types.CustomAgentConfig{AttachmentParseWaitTimeoutSec: 1}}}
			if tc.wrongType {
				req.customAgent.Config.SupportedFileTypes = []string{"pdf"}
			}
			bus := event.NewEventBus()
			var results []event.AgentToolResultData
			bus.On(event.EventAgentToolResult, func(_ context.Context, evt event.Event) error {
				results = append(results, evt.Data.(event.AgentToolResultData))
				return nil
			})
			h := &Handler{temporaryDocuments: svc}
			err := h.resolveTemporaryAttachments(&sseStreamContext{asyncCtx: t.Context(), eventBus: bus}, req)
			require.Equal(t, tc.wantResolve, svc.resolveCalls)
			require.Len(t, results, 1)
			if tc.wantErr {
				require.ErrorContains(t, err, "attachment_unavailable")
				require.Empty(t, req.attachments)
				require.False(t, results[0].Success)
				require.NotContains(t, results[0].Output, "private backend")
			} else {
				require.NoError(t, err)
				require.Len(t, req.attachments, 1)
				require.True(t, results[0].Success)
			}
		})
	}
}

func TestNoExplicitAttachmentsDoesNotCallService(t *testing.T) {
	require.NoError(t, (&Handler{}).resolveTemporaryAttachments(nil, &qaRequestContext{}))
}
