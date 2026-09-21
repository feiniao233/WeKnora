package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	sqlite3migrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

var (
	migrationStateMu        sync.RWMutex
	currentMigrationVersion uint
	currentMigrationDirty   bool
	migrationVersionSet     bool
	currentMigrationError   string
)

// CachedMigrationVersion returns the migration version captured at startup.
// Returns (version, dirty, ok). ok is false if the version was never captured.
//
// Note: when migrations fail mid-way, the cache may still be populated via a
// best-effort m.Version() call inside RunMigrationsWithOptions so the system
// info endpoint can surface the partial state. Check CachedMigrationError() to
// distinguish a clean version reading from a recorded-after-failure one.
func CachedMigrationVersion() (uint, bool, bool) {
	migrationStateMu.RLock()
	defer migrationStateMu.RUnlock()
	return currentMigrationVersion, currentMigrationDirty, migrationVersionSet
}

// CachedMigrationError returns the error message captured when the most recent
// migration attempt failed at startup. Empty string means migrations either
// succeeded or were never run.
func CachedMigrationError() string {
	migrationStateMu.RLock()
	defer migrationStateMu.RUnlock()
	return currentMigrationError
}

// setMigrationState records the latest known migration state. Unlike the old
// sync.Once-based setter, this is intentionally idempotent-overwrite so the
// failure path (which runs after Up() errored) can replace the pre-migration
// snapshot taken from the initial m.Version() call.
func setMigrationState(version uint, dirty bool, errMsg string, versionKnown bool) {
	migrationStateMu.Lock()
	defer migrationStateMu.Unlock()
	if versionKnown {
		currentMigrationVersion = version
		currentMigrationDirty = dirty
		migrationVersionSet = true
	}
	currentMigrationError = errMsg
}

// captureMigrationFailure best-effort queries m for the current version so the
// system info endpoint can show "N (failed)" instead of vanishing the row, and
// stores the human-readable error message. Always returns the original error.
func captureMigrationFailure(m *migrate.Migrate, err error) error {
	versionKnown := false
	var ver uint
	var dirty bool
	if m != nil {
		v, d, vErr := m.Version()
		if vErr == nil {
			versionKnown = true
			ver, dirty = v, d
		}
	}
	setMigrationState(ver, dirty, err.Error(), versionKnown)
	return err
}

// RunMigrations executes all pending database migrations
// This should be called during application startup
func RunMigrations(dsn string) error {
	return RunMigrationsWithOptions(dsn, MigrationOptions{AutoRecoverDirty: false})
}

// MigrationOptions configures migration behavior
type MigrationOptions struct {
	// AutoRecoverDirty when true, automatically attempts to recover from dirty state
	// by forcing to the previous version and retrying the migration
	AutoRecoverDirty bool

	// SQLiteDBPath is the raw filesystem path to the SQLite database file.
	// When set, the migrator opens the DB directly via sql.Open instead of
	// parsing a URL-based DSN, which avoids breakage when the path contains
	// spaces (e.g. macOS "Application Support").
	SQLiteDBPath string
}

// RunMigrationsWithOptions executes all pending database migrations with custom options
func RunMigrationsWithOptions(dsn string, opts MigrationOptions) error {
	ctx := context.Background()

	logger.Infof(ctx, "Starting database migration...")

	migrationsPath := "file://migrations/versioned"
	if strings.HasPrefix(dsn, "sqlite3://") {
		migrationsPath = "file://migrations/sqlite"
	}

	var m *migrate.Migrate
	var sqliteDB *sql.DB
	if opts.SQLiteDBPath != "" {
		sqlDB, err := sql.Open("sqlite3", opts.SQLiteDBPath)
		if err != nil {
			logger.Errorf(ctx, "Failed to open sqlite db for migration: %v", err)
			wrapped := fmt.Errorf("failed to open sqlite db for migration: %w", err)
			setMigrationState(0, false, wrapped.Error(), false)
			return wrapped
		}
		sqliteDB = sqlDB
		driver, err := sqlite3migrate.WithInstance(sqlDB, &sqlite3migrate.Config{})
		if err != nil {
			sqlDB.Close()
			logger.Errorf(ctx, "Failed to create sqlite3 migrate driver: %v", err)
			wrapped := fmt.Errorf("failed to create sqlite3 migrate driver: %w", err)
			setMigrationState(0, false, wrapped.Error(), false)
			return wrapped
		}
		m, err = migrate.NewWithDatabaseInstance(migrationsPath, "sqlite3", driver)
		if err != nil {
			logger.Errorf(ctx, "Failed to create migrate instance: %v", err)
			wrapped := fmt.Errorf("failed to create migrate instance: %w", err)
			setMigrationState(0, false, wrapped.Error(), false)
			return wrapped
		}
	} else {
		var err error
		m, err = migrate.New(migrationsPath, dsn)
		if err != nil {
			logger.Errorf(ctx, "Failed to create migrate instance: %v", err)
			wrapped := fmt.Errorf("failed to create migrate instance: %w", err)
			setMigrationState(0, false, wrapped.Error(), false)
			return wrapped
		}
	}
	defer m.Close()

	// Check current version and dirty state before migration
	oldVersion, oldDirty, versionErr := m.Version()
	if versionErr != nil && versionErr != migrate.ErrNilVersion {
		logger.Errorf(ctx, "Failed to get migration version: %v", versionErr)
		return captureMigrationFailure(m, fmt.Errorf("failed to get migration version: %w", versionErr))
	}

	if versionErr == migrate.ErrNilVersion {
		logger.Infof(ctx, "Database has no migration history, will start from version 0")
	} else {
		logger.Infof(ctx, "Current migration version: %d, dirty: %v", oldVersion, oldDirty)
	}

	// If database is in dirty state, try to recover or return error
	if oldDirty {
		logger.Warnf(ctx, "Database is in dirty state at version %d", oldVersion)
		if opts.AutoRecoverDirty {
			logger.Infof(ctx, "AutoRecoverDirty is enabled, attempting recovery...")
			if err := recoverFromDirtyState(ctx, m, oldVersion); err != nil {
				return captureMigrationFailure(m, err)
			}
			// Update oldVersion after recovery
			oldVersion, _, _ = m.Version()
		} else {
			// Calculate the version to force to (usually the previous version)
			forceVersion := int(oldVersion) - 1
			if oldVersion == 0 || forceVersion < 0 {
				forceVersion = 0
			}
			return captureMigrationFailure(m, fmt.Errorf(
				"database is in dirty state at version %d. This usually means a migration failed partway through. "+
					"To fix this:\n"+
					"1. Check if the migration partially applied changes and manually fix if needed\n"+
					"2. Use the force command to set the version to the last successful migration (usually %d):\n"+
					"   ./scripts/migrate.sh force %d\n"+
					"   Or if using make: make migrate-force version=%d\n"+
					"3. After fixing, restart the application to retry the migration\n"+
					"Or enable AutoRecoverDirty option to automatically retry",
				oldVersion,
				forceVersion,
				forceVersion,
				forceVersion,
			))
		}
	}

	// Run all pending migrations
	logger.Infof(ctx, "Running pending migrations...")
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		logger.Errorf(ctx, "Migration failed: %v", err)
		// Check if error is due to dirty state (in case it became dirty during migration)
		currentVersion, currentDirty, versionCheckErr := m.Version()
		if versionCheckErr == nil && currentDirty {
			logger.Warnf(ctx, "Migration caused dirty state at version %d", currentVersion)
			if opts.AutoRecoverDirty {
				logger.Infof(ctx, "Attempting to recover from dirty state...")
				// Try to recover and retry
				if recoverErr := recoverFromDirtyState(ctx, m, currentVersion); recoverErr != nil {
					return captureMigrationFailure(m, recoverErr)
				}
				// Retry migration after recovery
				logger.Infof(ctx, "Retrying migration after recovery...")
				if retryErr := m.Up(); retryErr != nil && retryErr != migrate.ErrNoChange {
					logger.Errorf(ctx, "Migration failed after recovery attempt: %v", retryErr)
					return captureMigrationFailure(m, fmt.Errorf("migration failed after recovery attempt: %w", retryErr))
				}
			} else {
				// Calculate the version to force to (usually the previous version)
				forceVersion := currentVersion - 1
				if currentVersion == 0 {
					forceVersion = 0
				}
				return captureMigrationFailure(m, fmt.Errorf(
					"migration failed and database is now in dirty state at version %d. "+
						"To fix this:\n"+
						"1. Check if the migration partially applied changes and manually fix if needed\n"+
						"2. Use the force command to set the version to the last successful migration (usually %d):\n"+
						"   ./scripts/migrate.sh force %d\n"+
						"   Or if using make: make migrate-force version=%d\n"+
						"3. After fixing, restart the application to retry the migration\n"+
						"Or enable AutoRecoverDirty option to automatically retry",
					currentVersion,
					forceVersion,
					forceVersion,
					forceVersion,
				))
			}
		} else {
			return captureMigrationFailure(m, fmt.Errorf("failed to run migrations: %w", err))
		}
	}
	// Get current version after migration
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return captureMigrationFailure(m, fmt.Errorf("failed to get migration version: %w", err))
	}
	if sqliteDB != nil && version >= 27 {
		if err := reconcilePrivateForkSQLiteSchema(sqliteDB); err != nil {
			return captureMigrationFailure(m, fmt.Errorf("failed to reconcile private-fork sqlite schema: %w", err))
		}
	}

	setMigrationState(version, dirty, "", true)

	if oldVersion != version {
		logger.Infof(ctx, "Database migrated from version %d to %d", oldVersion, version)
	} else {
		logger.Infof(ctx, "Database is up to date (version: %d)", version)
	}

	if dirty {
		logger.Warnf(ctx, "Database is in dirty state! Manual intervention may be required.")
	}

	return nil
}

// reconcilePrivateForkSQLiteSchema repairs the historical collision where the
// private fork and upstream both assigned migrations 000013 and 000014. It is
// deliberately idempotent and runs after every SQLite migration attempt so a
// clean version marker cannot hide a partially repaired schema.
func reconcilePrivateForkSQLiteSchema(db *sql.DB) error {
	for _, column := range []struct {
		table, name, definition string
	}{
		{"mcp_tool_approvals", "enabled", "BOOLEAN NOT NULL DEFAULT 1"},
		{"knowledge_bases", "category", "TEXT NOT NULL DEFAULT 'general'"},
		{"messages", "execution_result", "TEXT"},
	} {
		exists, err := privateForkSQLiteColumnExists(db, column.table, column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", column.table, column.name, column.definition)); err != nil {
				return fmt.Errorf("add %s.%s: %w", column.table, column.name, err)
			}
		}
	}

	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_category ON knowledge_bases (tenant_id, category)`,
		`CREATE TABLE IF NOT EXISTS browser_devices (
 scope_key VARCHAR(32) PRIMARY KEY, id VARCHAR(32) NOT NULL UNIQUE, tenant BIGINT NOT NULL,
 "user" VARCHAR(36) NOT NULL, label VARCHAR(100) NOT NULL, token_hash VARCHAR(64) NOT NULL UNIQUE,
 previous_hash VARCHAR(64) NOT NULL DEFAULT '', previous_until DATETIME NOT NULL,
 expires_at DATETIME NOT NULL, renew_after DATETIME NOT NULL, created_at DATETIME NOT NULL,
 last_seen_at DATETIME NOT NULL, revoked_at DATETIME, owner VARCHAR(32) NOT NULL DEFAULT '',
 owner_url VARCHAR(500) NOT NULL DEFAULT '', lease_key VARCHAR(32) NOT NULL DEFAULT '', lease_until DATETIME NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS browser_pairings (
 scope_key VARCHAR(32) PRIMARY KEY, token_hash VARCHAR(64) NOT NULL UNIQUE,
 tenant BIGINT NOT NULL, "user" VARCHAR(36) NOT NULL, expires_at DATETIME NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS browser_pairings_expiry ON browser_pairings(expires_at)`,
		`CREATE TABLE IF NOT EXISTS browser_task_interruptions (
 scope_key VARCHAR(32) NOT NULL, session VARCHAR(36) NOT NULL, PRIMARY KEY (scope_key, session)
)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func privateForkSQLiteColumnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// recoverFromDirtyState attempts to recover from a dirty migration state
// by forcing to the previous version and allowing the migration to be retried
func recoverFromDirtyState(ctx context.Context, m *migrate.Migrate, dirtyVersion uint) error {
	// Special case: if dirty at version 0 (init migration), we cannot go back further
	// The only option is to force to version 0 and retry, but this requires the migration to be idempotent
	if dirtyVersion == 0 {
		logger.Warnf(ctx, "Database is in dirty state at version 0 (init migration). "+
			"This is the initial migration, cannot rollback further. "+
			"Will attempt to clear dirty flag and retry. "+
			"Note: This only works if the init migration uses IF NOT EXISTS clauses.")

		// Force to version -1 (no version) to allow re-running version 0
		// This effectively tells migrate that no migrations have been applied
		if err := m.Force(-1); err != nil {
			return fmt.Errorf(
				"failed to recover from dirty state at version 0. "+
					"Manual intervention required:\n"+
					"1. Check what was partially created in the database\n"+
					"2. Either drop all created objects and retry, or\n"+
					"3. Manually complete the migration and run: ./scripts/migrate.sh force 0\n"+
					"Error: %w", err)
		}

		logger.Infof(ctx, "Cleared migration state, will retry from version 0")
		return nil
	}

	forceVersion := int(dirtyVersion) - 1

	logger.Warnf(ctx, "Database is in dirty state at version %d, attempting auto-recovery by forcing to version %d",
		dirtyVersion, forceVersion)

	// Force to previous version to clear dirty state
	if err := m.Force(forceVersion); err != nil {
		return fmt.Errorf("failed to force migration version during recovery: %w", err)
	}

	logger.Infof(ctx, "Successfully forced migration to version %d, migration will be retried", forceVersion)
	return nil
}

// GetMigrationVersion returns the current migration version
func GetMigrationVersion() (uint, bool, error) {
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)

	migrationsPath := "file://migrations/versioned"

	m, err := migrate.New(migrationsPath, dbURL)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil {
		return 0, false, err
	}

	return version, dirty, nil
}
