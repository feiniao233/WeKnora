package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListCatalogGroupsInstallsByDefinition(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:catalog-list?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&types.TenantSkillEntity{}, &types.TenantSkillCatalogEntity{},
		&types.TenantSkillSnapshotEntity{}, &types.TenantUserEnvVar{},
	))
	repo := repository.NewTenantSkillRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.CreateCatalog(ctx, &types.TenantSkillCatalogEntity{
		ID: "cat-pdf", TenantID: 7, Name: "pdf", Description: "extract",
	}))
	require.NoError(t, repo.CreateSkill(ctx, &types.TenantSkillEntity{
		ID: "sk-1", TenantID: 7, SandboxConfigID: "cfg-a", CatalogID: "cat-pdf",
		Name: "pdf", Status: types.SkillStatusReady, Enabled: true,
	}))
	require.NoError(t, repo.CreateSkill(ctx, &types.TenantSkillEntity{
		ID: "sk-2", TenantID: 7, SandboxConfigID: "cfg-b", CatalogID: "cat-pdf",
		Name: "pdf", Status: types.SkillStatusInstalling, Enabled: true,
	}))

	svc := NewTenantSkillService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	list, err := svc.ListCatalog(ctx, 7)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "pdf", list[0].Name)
	require.Len(t, list[0].Installations, 2)
}

func TestResolveCatalogFindsLegacySkillID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:catalog-legacy?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&types.TenantSkillEntity{}, &types.TenantSkillCatalogEntity{},
		&types.TenantSkillSnapshotEntity{}, &types.TenantUserEnvVar{},
	))
	repo := repository.NewTenantSkillRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.CreateSkill(ctx, &types.TenantSkillEntity{
		ID: "sk-old", TenantID: 7, SandboxConfigID: "cfg-a",
		Name: "pdf", BundleRef: "local://7/tenant-skills/sk-old.zip",
		Status: types.SkillStatusReady, Enabled: true,
	}))

	svc := NewTenantSkillService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cat, err := svc.resolveCatalog(ctx, 7, "sk-old")
	require.NoError(t, err)
	require.NotNil(t, cat)
	require.Equal(t, "pdf", cat.Name)
	require.Equal(t, "local://7/tenant-skills/sk-old.zip", cat.BundleRef)
}

func catalogTestRepo(t *testing.T, dsn string) repository.TenantSkillRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&types.TenantSkillEntity{}, &types.TenantSkillCatalogEntity{},
		&types.TenantSkillSnapshotEntity{}, &types.TenantUserEnvVar{},
	))
	return repository.NewTenantSkillRepository(db)
}

func TestListCatalogShowsInstallsWhoseCatalogWasDeleted(t *testing.T) {
	repo := catalogTestRepo(t, "file:catalog-orphan?mode=memory&cache=shared")
	ctx := context.Background()
	require.NoError(t, repo.CreateCatalog(ctx, &types.TenantSkillCatalogEntity{
		ID: "cat-gone", TenantID: 7, Name: "pdf",
	}))
	require.NoError(t, repo.CreateSkill(ctx, &types.TenantSkillEntity{
		ID: "sk-1", TenantID: 7, SandboxConfigID: "cfg-a", CatalogID: "cat-gone",
		Name: "pdf", Status: types.SkillStatusReady, Enabled: true,
	}))
	require.NoError(t, repo.DeleteCatalog(ctx, 7, "cat-gone"))

	svc := NewTenantSkillService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	list, err := svc.ListCatalog(ctx, 7)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "pdf", list[0].Name)
	require.Len(t, list[0].Installations, 1)
	require.Equal(t, "sk-1", list[0].Installations[0].SkillID)
}

func TestDeleteCatalogRefusesWhileARemovalIsInFlight(t *testing.T) {
	repo := catalogTestRepo(t, "file:catalog-removing?mode=memory&cache=shared")
	ctx := context.Background()
	require.NoError(t, repo.CreateCatalog(ctx, &types.TenantSkillCatalogEntity{
		ID: "cat-pdf", TenantID: 7, Name: "pdf",
	}))
	require.NoError(t, repo.CreateSkill(ctx, &types.TenantSkillEntity{
		ID: "sk-1", TenantID: 7, SandboxConfigID: "cfg-a", CatalogID: "cat-pdf",
		Name: "pdf", Status: types.SkillStatusRemoving, Enabled: true,
	}))

	svc := NewTenantSkillService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := svc.DeleteCatalog(ctx, 7, "cat-pdf")
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	require.Equal(t, 409, appErr.HTTPCode)
}

func TestUpsertCatalogDoesNotStampSHAWhenStoreFails(t *testing.T) {
	fx := newInstallFixture(t)
	fx.saveErr = errors.New("disk full")
	ctx := context.Background()
	oldSHA := strings.Repeat("c", 64)
	require.NoError(t, fx.skillRepo.CreateCatalog(ctx, &types.TenantSkillCatalogEntity{
		ID: "cat-1", TenantID: 7, Name: "pdf-tools",
		BundleRef: "file://old.zip", BundleSHA256: oldSHA,
	}))
	archive := zipBundle(t, map[string]string{"SKILL.md": validSkillMD})
	bundle, err := ParseSkillBundle(archive)
	require.NoError(t, err)
	require.NotEqual(t, oldSHA, bundle.SHA256)

	got, err := fx.svc.upsertCatalogFromBundle(ctx, 7, bundle, archive, false)
	require.NoError(t, err)
	require.Equal(t, oldSHA, got.BundleSHA256)
	require.Equal(t, "file://old.zip", got.BundleRef)
}

func TestInstallCatalogToConfigsReportsPartialFailure(t *testing.T) {
	fx := newInstallFixture(t)
	ctx := context.Background()
	archive := zipBundle(t, map[string]string{
		"SKILL.md":           validSkillMD,
		"scripts/extract.py": "print('hi')\n",
	})
	cat, err := fx.svc.RegisterCatalogFromArchive(ctx, 7, archive)
	require.NoError(t, err)
	require.NotNil(t, cat)

	result, err := fx.svc.InstallCatalogToConfigs(ctx, 7, cat.ID, []string{"cfg-1", "missing"})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Installs, "cfg-1")
	require.Equal(t, "sandbox config not found", result.Errors["missing"])
}

func TestUpdateCatalogFileReplacesTextAndKeepsCatalogIdentity(t *testing.T) {
	fx := newInstallFixture(t)
	ctx := context.Background()
	archive := zipBundle(t, map[string]string{
		"SKILL.md":              validSkillMD,
		"references/runbook.md": "old\n",
		"scripts/tool.bin":      "\x00\x01",
	})
	cat, err := fx.svc.RegisterCatalogFromArchive(ctx, 7, archive)
	require.NoError(t, err)
	oldSHA := cat.BundleSHA256
	installBefore, err := fx.skillRepo.GetSkill(ctx, 7, "cfg-1", "sk-1")
	require.NoError(t, err)

	got, err := fx.svc.UpdateCatalogFile(ctx, 7, cat.ID, "references/runbook.md", "new\n")
	require.NoError(t, err)
	require.Equal(t, "new\n", got.Content)
	require.Equal(t, skillFileEncodingUTF8, got.Encoding)

	updated, err := fx.skillRepo.GetCatalog(ctx, 7, cat.ID)
	require.NoError(t, err)
	require.Equal(t, cat.ID, updated.ID)
	require.NotEqual(t, oldSHA, updated.BundleSHA256)
	bundle, err := ParseSkillBundle(fx.storedBundles[updated.BundleRef])
	require.NoError(t, err)
	require.Equal(t, []byte("new\n"), bundle.Files["references/runbook.md"])
	require.Equal(t, []byte{0, 1}, bundle.Files["scripts/tool.bin"])

	installAfter, err := fx.skillRepo.GetSkill(ctx, 7, "cfg-1", "sk-1")
	require.NoError(t, err)
	require.Equal(t, installBefore.BundleSHA256, installAfter.BundleSHA256)
	require.Equal(t, installBefore.BundleRef, installAfter.BundleRef)
}

func TestUpdateCatalogFileRejectsBinaryAndNameChange(t *testing.T) {
	fx := newInstallFixture(t)
	ctx := context.Background()
	archive := zipBundle(t, map[string]string{
		"SKILL.md":         validSkillMD,
		"assets/fake.png":  "plain text with an image extension",
		"scripts/tool.bin": "\x00\x01",
	})
	cat, err := fx.svc.RegisterCatalogFromArchive(ctx, 7, archive)
	require.NoError(t, err)

	_, err = fx.svc.UpdateCatalogFile(ctx, 7, cat.ID, "scripts/tool.bin", "text")
	require.ErrorContains(t, err, "only untruncated UTF-8 text files can be edited")
	_, err = fx.svc.UpdateCatalogFile(ctx, 7, cat.ID, "assets/fake.png", "text")
	require.ErrorContains(t, err, "only untruncated UTF-8 text files can be edited")
	_, err = fx.svc.UpdateCatalogFile(ctx, 7, cat.ID, "SKILL.md", strings.Repeat("x", skillFileTextLimit+1))
	require.ErrorContains(t, err, "larger than 1 MiB")
	_, err = fx.svc.UpdateCatalogFile(ctx, 7, cat.ID, "SKILL.md", strings.Replace(validSkillMD, "pdf-tools", "renamed", 1))
	require.ErrorContains(t, err, "SKILL.md name cannot be changed")
	_, err = fx.svc.UpdateCatalogFile(ctx, 7, cat.ID, "missing.md", "text")
	require.ErrorContains(t, err, "skill file not found")
}
