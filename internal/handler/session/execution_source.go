package session

import (
	"context"
	"fmt"
	"slices"

	"github.com/Tencent/WeKnora/internal/types"
)

func validActionID(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, ch := range value {
		if ch != '_' && ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func (h *Handler) resolveExecutionMCPScope(ctx context.Context, agent *types.CustomAgent, tenantID uint64) error {
	if agent.Config.MCPSelectionMode == "none" {
		agent.Config.MCPServices = nil
		return nil
	}
	if tenantID == 0 {
		tenantID = agent.TenantID
	}
	if h.mcpService == nil {
		if agent.Config.MCPSelectionMode == "selected" {
			return nil
		}
		return fmt.Errorf("mcp_service_unavailable: cannot resolve execution scope")
	}
	services, err := h.mcpService.ListMCPServices(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("mcp_service_unavailable: cannot resolve execution scope")
	}
	ids := make([]string, 0, len(services))
	for _, svc := range services {
		if svc != nil && svc.Enabled && (agent.Config.MCPSelectionMode != "selected" || slices.Contains(agent.Config.MCPServices, svc.ID)) {
			ids = append(ids, svc.ID)
		}
	}
	agent.Config.MCPSelectionMode = "selected"
	agent.Config.MCPServices = ids
	return nil
}

func (h *Handler) resolveExecutionSkillScope(ctx context.Context, session *types.Session, agent *types.CustomAgent, tenantID uint64) {
	if agent.Config.SkillsSelectionMode != "all" || h.tenantSkills == nil {
		return
	}
	if tenantID == 0 {
		tenantID = session.TenantID
	}
	configID := agent.Config.SandboxConfigID
	if session.SandboxConfigID != "" {
		configID = session.SandboxConfigID
	}
	rows := h.tenantSkills.ListUsableSkills(ctx, tenantID, configID)
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			names = append(names, row.Name)
		}
	}
	agent.Config.SkillsSelectionMode = "selected"
	agent.Config.SelectedSkills = names
}

func (h *Handler) applySourceExecution(ctx context.Context, session *types.Session, agent *types.CustomAgent, request *CreateKnowledgeQARequest) error {
	if session.AgentID == "" || agent == nil {
		return fmt.Errorf("source execution requires a bound agent")
	}
	msg, err := h.messageService.GetMessage(ctx, session.ID, request.SourceMessageID)
	if err != nil || msg == nil || msg.SessionID != session.ID || msg.Role != "assistant" || !msg.IsCompleted || msg.ExecutionResult == nil || msg.ExecutionResult.Status != "completed" || msg.AgentID != session.AgentID {
		return fmt.Errorf("source message must be a completed assistant execution in this session")
	}
	source := msg.ExecutionContext
	if source.ExecutionConfigHash == "" || msg.ModelID == "" || source.MCPSelectionMode == "" {
		return fmt.Errorf("source execution scope is unavailable")
	}
	if source.SkillsSelectionMode == "all" {
		return fmt.Errorf("source execution has no fixed skill scope")
	}
	for _, name := range source.SelectedSkillNames {
		if agent.Config.SkillsSelectionMode != "all" && (agent.Config.SkillsSelectionMode != "selected" || !slices.Contains(agent.Config.SelectedSkills, name)) {
			return fmt.Errorf("skill_unavailable: source skill is no longer allowed")
		}
	}
	for _, id := range source.ResolvedMCPServiceIDs {
		if agent.Config.MCPSelectionMode == "none" || !slices.Contains(agent.Config.MCPServices, id) {
			return fmt.Errorf("mcp_service_unavailable: source service is no longer allowed")
		}
	}
	agent.Config.SkillsSelectionMode = source.SkillsSelectionMode
	agent.Config.SelectedSkills = slices.Clone(source.SelectedSkillNames)
	agent.Config.MCPSelectionMode = source.MCPSelectionMode
	agent.Config.MCPServices = slices.Clone(source.ResolvedMCPServiceIDs)
	request.SummaryModelID = msg.ModelID
	agent.Config.ModelID = msg.ModelID
	request.SkillNames = slices.Clone(source.SkillNames)
	request.MCPServiceIDs = slices.Clone(source.MCPServiceIDs)
	request.KnowledgeBaseIDs = slices.Clone(source.KnowledgeBaseIDs)
	request.KnowledgeIds = slices.Clone(source.KnowledgeIDs)
	request.TagIDs = slices.Clone(source.TagIDs)
	request.WebSearchEnabled = source.WebSearchEnabled
	request.MentionedItems = nil
	return nil
}
