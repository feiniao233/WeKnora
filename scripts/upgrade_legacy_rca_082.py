"""Offline bridge from private RCA 108/27 to v0.8.2's 111/31.

Stop the app and back up the database before running. This command is only for
the private RCA fork, whose migration numbers collided with upstream v0.8.2.
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
    "sessions": {"sandbox_config_tenant_id", "host_workspace_dir"},
    "im_channels": {"locale"},
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
        if len(rows) != 1 or rows[0][1] or rows[0][0] not in (27, 31):
            raise RuntimeError("Expected clean private RCA version 27 or upgraded version 31")
        require_sqlite_columns(db, RCA_COLUMNS)
        if not db.execute("SELECT 1 FROM sqlite_master WHERE type='index' AND name='idx_knowledge_bases_tenant_category'").fetchone():
            raise RuntimeError("Missing RCA category index")
        if rows[0][0] == 27:
            for version in range(27, 31):
                execute_sqlite_statements(db, migration_sql("sqlite", version))
        require_sqlite_columns(db, NEW_COLUMNS | {
            "tenant_skills": {"catalog_id", "envs", "served"},
            "tenant_skill_snapshots": {"planned_name"},
            "tenant_skill_catalog": {"id", "name"},
            "tenant_user_env_vars": {"principal_type", "principal_id", "value"},
        })
        db.execute("UPDATE schema_migrations SET version=31, dirty=0")
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
    # keeps those intact. Migration 111 is already present as old RCA 108.
    pending = "\n".join(migration_sql("versioned", v) for v in range(108, 111))
    return f"""
BEGIN;
SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '120s';
SET LOCAL search_path = public;
LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE;
DO $rca_upgrade$
DECLARE current_version BIGINT;
BEGIN
    IF (SELECT count(*) FROM schema_migrations) <> 1 OR EXISTS (
        SELECT 1 FROM schema_migrations WHERE dirty OR version NOT IN (108,111)
    ) THEN
        RAISE EXCEPTION 'Expected clean private RCA version 108 or upgraded version 111';
    END IF;
    SELECT version INTO current_version FROM schema_migrations;
    {''.join(checks)}
    IF to_regclass('public.idx_knowledge_bases_tenant_category') IS NULL THEN
        RAISE EXCEPTION 'Missing RCA category index';
    END IF;
    IF current_version = 108 THEN
        {pending}
    END IF;
    {''.join(validate_new)}
    UPDATE schema_migrations SET version=111, dirty=false;
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
        print("SQLite legacy RCA upgrade verified: version=31, dirty=false")
    else:
        cmd = ["docker", "exec", "-i"]
        if args.database:
            cmd += ["-e", "PGDATABASE=" + args.database]
        cmd += [args.postgres_container, "sh", "-c",
                'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "${PGDATABASE:-$POSTGRES_DB}"']
        subprocess.run(cmd, input=postgres_upgrade_sql(), text=True, check=True)


if __name__ == "__main__":
    main()
