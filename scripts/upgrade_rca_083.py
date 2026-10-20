"""Offline bridge from private RCA v0.8.2 (111/31) to v0.8.3 (119/37).

Stop the app and back up the database before running. This command is only for
the private RCA fork, whose migration numbers collided with upstream v0.8.3.
It preserves the RCA columns and data and applies the missed upstream SQL in a
single transaction. A clean, already upgraded database is a validated no-op.
PostgreSQL credentials stay in the existing container environment.
"""
import argparse
from pathlib import Path
import sqlite3
import subprocess

ROOT = Path(__file__).resolve().parents[1]
RCA_COLUMNS = {
    "knowledge_bases": {"category"},
    "messages": {"execution_result"},
    "sessions": {"agent_id"},
}
NEW_COLUMNS = {
    "model_catalog_configs": {"version", "overlay", "history", "updated_by", "updated_at"},
    "chunk_images": {"chunk_id", "image_index", "url", "caption", "attrs"},
    "chunks": {"source_locators"},
}


def migration_sql(engine, version):
    files = list((ROOT / "migrations" / engine).glob(f"{version:06d}_*.up.sql"))
    if len(files) != 1:
        raise RuntimeError(f"Expected exactly one {engine} migration {version}")
    return files[0].read_text()


def require_sqlite_columns(db, required):
    for table, columns in required.items():
        actual = {row[1] for row in db.execute(f'PRAGMA table_info("{table}")')}
        if not columns <= actual:
            raise RuntimeError(f"Incomplete schema: {table} missing {sorted(columns - actual)}")


def execute_sqlite_statements(db, sql):
    # executescript implicitly commits, so execute each complete statement to
    # keep DDL and the version marker inside our single transaction.
    statement = ""
    for line in sql.splitlines(keepends=True):
        statement += line
        if sqlite3.complete_statement(statement):
            db.execute(statement)
            statement = ""
    if statement.strip():
        db.execute(statement)


def upgrade_sqlite(path):
    path = Path(path).resolve()
    if not path.is_file():
        raise RuntimeError("SQLite database does not exist")
    db = sqlite3.connect(path.as_uri() + "?mode=rw", uri=True, timeout=10)
    try:
        db.execute("BEGIN IMMEDIATE")
        rows = db.execute("SELECT version, dirty FROM schema_migrations").fetchall()
        if len(rows) != 1 or rows[0][1] or rows[0][0] not in (31, 37):
            raise RuntimeError("Expected clean private RCA version 31 or upgraded version 37")
        require_sqlite_columns(db, RCA_COLUMNS)
        if not db.execute("SELECT 1 FROM sqlite_master WHERE type='index' AND name='idx_knowledge_bases_tenant_category'").fetchone():
            raise RuntimeError("Missing RCA category index")
        if rows[0][0] == 31:
            for version in range(31, 37):
                execute_sqlite_statements(db, migration_sql("sqlite", version))
        require_sqlite_columns(db, NEW_COLUMNS | {
            "mcp_services": {"usage_instructions"},
            "mcp_metadata": {"tenant_id", "service_id", "principal", "tools"},
        })
        for index in ("idx_messages_live_attachments",):
            if not db.execute("SELECT 1 FROM sqlite_master WHERE type='index' AND name=?", (index,)).fetchone():
                raise RuntimeError(f"Missing upgraded index: {index}")
        if db.execute("SELECT count(*) FROM model_catalog_configs WHERE id=1").fetchone()[0] != 1:
            raise RuntimeError("Missing model catalog configuration")
        db.execute("UPDATE schema_migrations SET version=37, dirty=0")
        db.commit()
    except Exception:
        db.rollback()
        raise
    finally:
        db.close()


def postgres_upgrade_sql():
    checks = []
    for table, columns in RCA_COLUMNS.items():
        for column in sorted(columns):
            checks.append(f"""
            IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema='public' AND table_name='{table}' AND column_name='{column}') THEN
                RAISE EXCEPTION 'Incomplete RCA schema: {table}.{column} missing';
            END IF;
            """)
    validate_new = []
    for table, columns in NEW_COLUMNS.items():
        for column in sorted(columns):
            validate_new.append(f"""
            IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema='public' AND table_name='{table}' AND column_name='{column}') THEN
                RAISE EXCEPTION 'Incomplete upgraded schema: {table}.{column} missing';
            END IF;
            """)
    # Original upstream SQL includes DO blocks; a distinct outer delimiter
    # keeps those intact. Migration 119 is already present as old RCA 111.
    # The app is stopped. Build the attachment index normally so all SQL,
    # including the version marker, remains atomic; leave upstream SQL intact.
    pending = "\n".join(
        migration_sql("versioned", v).replace("CREATE INDEX CONCURRENTLY", "CREATE INDEX")
        if v == 117 else migration_sql("versioned", v)
        for v in range(111, 119)
    )
    return f"""
BEGIN;
SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '15min';
SET LOCAL search_path = public;
LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE;
DO $rca_upgrade$
DECLARE current_version BIGINT;
BEGIN
    IF (SELECT count(*) FROM schema_migrations) <> 1 OR EXISTS (
        SELECT 1 FROM schema_migrations WHERE dirty OR version NOT IN (111,119)
    ) THEN
        RAISE EXCEPTION 'Expected clean private RCA version 111 or upgraded version 119';
    END IF;
    SELECT version INTO current_version FROM schema_migrations;
    {''.join(checks)}
    IF to_regclass('public.idx_knowledge_bases_tenant_category') IS NULL THEN
        RAISE EXCEPTION 'Missing RCA category index';
    END IF;
    IF current_version = 111 THEN
        IF EXISTS (SELECT 1 FROM pg_extension WHERE extname='pg_search' AND extversion <> '0.25.11')
            AND NOT EXISTS (SELECT 1 FROM pg_available_extension_versions WHERE name='pg_search' AND version='0.25.11') THEN
            RAISE EXCEPTION 'Start the restored database on the v0.25.11 ParadeDB image before upgrading';
        END IF;
        {pending}
    END IF;
    {''.join(validate_new)}
    IF NOT EXISTS (SELECT 1 FROM model_catalog_configs WHERE id=1)
        OR to_regclass('public.idx_messages_live_attachments') IS NULL THEN
        RAISE EXCEPTION 'Incomplete upgraded catalog or attachment index';
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public'
        AND table_name IN ('mcp_oauth_tokens','mcp_oauth_clients')
        AND column_name IN ('expires_at','created_at','updated_at') AND data_type <> 'timestamp with time zone') THEN
        RAISE EXCEPTION 'Incomplete MCP OAuth timestamp migration';
    END IF;
    UPDATE schema_migrations SET version=119, dirty=false;
END $rca_upgrade$;
COMMIT;
SELECT version, dirty FROM schema_migrations;
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument("--sqlite", type=Path)
    target.add_argument("--postgres-container", help="Existing PostgreSQL container; app must be stopped")
    parser.add_argument("--database", help="Override POSTGRES_DB, e.g. a disposable restored backup")
    args = parser.parse_args()
    if args.sqlite:
        if args.database:
            parser.error("--database requires --postgres-container")
        upgrade_sqlite(args.sqlite)
        print("SQLite RCA v0.8.3 upgrade verified: version=37, dirty=false")
    else:
        cmd = ["docker", "exec", "-i"]
        if args.database:
            cmd += ["-e", "PGDATABASE=" + args.database]
        cmd += [args.postgres_container, "sh", "-c",
                'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "${PGDATABASE:-$POSTGRES_DB}"']
        subprocess.run(cmd, input=postgres_upgrade_sql(), text=True, check=True)


if __name__ == "__main__":
    main()
