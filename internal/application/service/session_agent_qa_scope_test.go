package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestResolvePerRequestMCPScope_SelectedIntersection(t *testing.T) {
	effective, mode := resolvePerRequestMCPScope(
		[]string{"mcp-b", "mcp-c"},
		[]string{"mcp-a", "mcp-b"},
		"selected",
		false,
	)
	assert.Equal(t, "selected", mode)
	assert.Equal(t, []string{"mcp-b"}, effective)
}

func TestResolvePerRequestMCPScope_SelectedRejectsOutsidePreset(t *testing.T) {
	effective, mode := resolvePerRequestMCPScope(
		[]string{"mcp-x"},
		[]string{"mcp-a"},
		"selected",
		false,
	)
	assert.Empty(t, effective)
	assert.Equal(t, "selected", mode)
}

func TestResolvePerRequestMCPScope_NoneRejectsMention(t *testing.T) {
	effective, mode := resolvePerRequestMCPScope(
		[]string{"mcp-iwiki"},
		nil,
		"none",
		false,
	)
	assert.Empty(t, effective)
	assert.Equal(t, "none", mode)
}

func TestResolvePerRequestMCPScope_SharedAgentBlocksOutsidePreset(t *testing.T) {
	effective, mode := resolvePerRequestMCPScope(
		[]string{"mcp-x"},
		[]string{"mcp-a"},
		"all",
		true,
	)
	assert.Empty(t, effective)
	assert.Equal(t, "all", mode)
}

func TestResolvePerRequestMCPScope_SharedAgentAllowsPreset(t *testing.T) {
	effective, mode := resolvePerRequestMCPScope(
		[]string{"mcp-a", "mcp-x"},
		[]string{"mcp-a", "mcp-b"},
		"all",
		true,
	)
	assert.Equal(t, "selected", mode)
	assert.Equal(t, []string{"mcp-a"}, effective)
}

func TestApplyPerRequestMCPScope_SelectedPinsWithoutNarrowing(t *testing.T) {
	cfg := &types.AgentConfig{MCPSelectionMode: "selected", MCPServices: []string{"mcp-a", "mcp-b"}}
	applyPerRequestMCPScope(context.Background(), cfg, []string{"mcp-a", "mcp-b"}, false, []string{"mcp-b"})
	assert.Equal(t, "selected", cfg.MCPSelectionMode)
	assert.Equal(t, []string{"mcp-a", "mcp-b"}, cfg.MCPServices)
	assert.Equal(t, []string{"mcp-b"}, cfg.PinnedMCPServiceIDs)
}

func TestApplyPerRequestMCPScope_NoneIgnoresMentionAndDoesNotPin(t *testing.T) {
	cfg := &types.AgentConfig{MCPSelectionMode: "none", MCPServices: []string{"mcp-a"}}
	applyPerRequestMCPScope(context.Background(), cfg, []string{"mcp-a"}, false, []string{"mcp-a"})
	assert.Equal(t, "none", cfg.MCPSelectionMode)
	assert.Empty(t, cfg.PinnedMCPServiceIDs)
}

func TestApplyPerRequestSkillScope(t *testing.T) {
	for _, tc := range []struct {
		name, mode         string
		enabled            bool
		allowed, requested []string
		rows               []*types.TenantSkillEntity
		wantErr            bool
	}{
		{name: "default unchanged", mode: "none"},
		{name: "disabled", mode: "none", requested: []string{"a"}, wantErr: true},
		{name: "unknown mode", mode: "future", enabled: true, requested: []string{"a"}, wantErr: true},
		{name: "empty mode", requested: []string{"a"}, wantErr: true},
		{name: "outside agent", mode: "selected", enabled: true, allowed: []string{"b"}, requested: []string{"a"}, wantErr: true},
		{name: "empty selected", mode: "selected", enabled: true, requested: []string{"a"}, wantErr: true},
		{name: "uninstalled", mode: "all", enabled: true, requested: []string{"a"}, wantErr: true},
		{name: "disabled install", mode: "all", enabled: true, requested: []string{"a"}, rows: []*types.TenantSkillEntity{{Name: "a", Status: types.SkillStatusReady}}, wantErr: true},
		{name: "installing", mode: "all", enabled: true, requested: []string{"a"}, rows: []*types.TenantSkillEntity{{Name: "a", Enabled: true, Status: "installing"}}, wantErr: true},
		{name: "ready selected", mode: "selected", enabled: true, allowed: []string{"a", "b"}, requested: []string{"a", "a"}, rows: []*types.TenantSkillEntity{{Name: "a", Enabled: true, Status: types.SkillStatusReady}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &types.AgentConfig{SkillsEnabled: tc.enabled, AllowedSkills: tc.allowed, TenantSkills: tc.rows}
			err := applyPerRequestSkillScope(t.Context(), cfg, tc.mode, tc.requested)
			if tc.wantErr {
				assert.ErrorContains(t, err, "skill_unavailable")
				assert.Empty(t, cfg.PinnedSkillNames)
			} else {
				assert.NoError(t, err)
				if len(tc.requested) > 0 {
					assert.Equal(t, []string{"a"}, cfg.PinnedSkillNames)
				}
			}
			assert.Equal(t, tc.allowed, cfg.AllowedSkills, "selection cannot revoke mandatory Agent skills")
		})
	}
}

func TestConfigureSkillsFromAgentDoesNotLoadHostPreloadedDir(t *testing.T) {
	svc := &sessionService{}
	cfg := &types.AgentConfig{}
	svc.configureSkillsFromAgent(context.Background(), cfg, &types.CustomAgent{
		Config: types.CustomAgentConfig{
			SandboxConfigID:     "cfg-1",
			SkillsSelectionMode: "all",
		},
	})
	assert.True(t, cfg.SkillsEnabled)
	assert.Equal(t, "cfg-1", cfg.SandboxConfigID)
	assert.Empty(t, cfg.SkillDirs,
		"a host skill directory is not what the sandbox image carries")
}

func TestExplicitSkillCannotUseAnotherWorkspaceOrSandbox(t *testing.T) {
	fx := newEffectiveFixture(t)
	for _, scope := range []struct {
		tenant uint64
		config string
	}{{8, "cfg-1"}, {7, "different-config"}} {
		cfg := &types.AgentConfig{SkillsEnabled: true, TenantSkills: effectiveTenantSkills(t.Context(), fx.configs, fx.skills, scope.tenant, scope.config)}
		assert.ErrorContains(t, applyPerRequestSkillScope(t.Context(), cfg, "all", []string{"ready-enabled"}), "skill_unavailable")
		assert.Empty(t, cfg.PinnedSkillNames)
	}
}
