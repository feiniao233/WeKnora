package dto

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// RoleFromContext returns the caller's tenant role from ctx.
func RoleFromContext(ctx context.Context) types.TenantRole {
	return types.TenantRoleFromContext(ctx)
}

// CanViewIntegrationSecrets is true for Admin+ tenant members and for API keys
// with full tenant access or the manage_tenant_settings capability.
func CanViewIntegrationSecrets(ctx context.Context) bool {
	if RoleFromContext(ctx).HasPermission(types.TenantRoleAdmin) {
		return true
	}
	return apiKeyCanManageIntegrationSecrets(ctx)
}

func apiKeyCanManageIntegrationSecrets(ctx context.Context) bool {
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok {
		return false
	}
	if scope.FullAccess {
		return true
	}
	return scope.HasCapability(types.APIKeyCapabilityManageTenantSettings)
}

// CanManageMCPServices permits MCP configuration detail for the same scoped
// API keys that are allowed to manage those services, without granting the
// broader manage_tenant_settings capability.
func CanManageMCPServices(ctx context.Context) bool {
	if CanViewIntegrationSecrets(ctx) {
		return true
	}
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	return ok && scope.HasCapability(types.APIKeyCapabilityManageMCPServices)
}

// CanManageModels permits model configuration detail for the same scoped API
// keys that are allowed to manage models, without granting unrelated
// tenant-level integration detail.
func CanManageModels(ctx context.Context) bool {
	if CanViewIntegrationSecrets(ctx) {
		return true
	}
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	return ok && scope.HasCapability(types.APIKeyCapabilityManageModels)
}

// RoleCanViewTenantAPIKey is true for Owner+ only.
func RoleCanViewTenantAPIKey(role types.TenantRole) bool {
	return role.HasPermission(types.TenantRoleOwner)
}

// CanViewTenantAPIKey is true for Owner+ only.
func CanViewTenantAPIKey(ctx context.Context) bool {
	return RoleCanViewTenantAPIKey(RoleFromContext(ctx))
}
