"""Real SQLite migration checks; run with python3 scripts/test_upgrade_legacy_rca_082.py."""
import importlib.util
from pathlib import Path
import sqlite3
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("upgrade", ROOT / "scripts/upgrade_legacy_rca_082.py")
upgrade = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(upgrade)


class LegacyUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / "legacy.db"
        self.db = sqlite3.connect(self.path)
        self.addCleanup(self.db.close)
        for migration in sorted((ROOT / "migrations/sqlite").glob("*.up.sql")):
            if int(migration.name.split("_")[0]) <= 26:
                self.db.executescript(migration.read_text())
        # The old 000027 RCA migration is byte-identical to the renamed 000031.
        self.db.executescript((ROOT / "migrations/sqlite/000031_rca_schema.up.sql").read_text())
        self.db.executescript("""
            CREATE TABLE schema_migrations (version BIGINT PRIMARY KEY, dirty BOOLEAN NOT NULL);
            INSERT INTO schema_migrations VALUES (27, 0);
            INSERT INTO tenants (id, name, business) VALUES (1, '升级验证', 'test');
            INSERT INTO knowledge_bases (id, name, tenant_id, embedding_model_id, summary_model_id, category)
                VALUES ('kb', '原始知识库', 1, '', '', 'rca');
            INSERT INTO sessions (id, tenant_id, title, agent_id) VALUES ('s', 1, '原始会话', 'agent');
            INSERT INTO messages (id, request_id, session_id, role, content, execution_result)
                VALUES ('m', 'r', 's', 'assistant', '中文结果 🧪', '{"status":"failed"}');
        """)

    def snapshot(self):
        return list(self.db.iterdump())

    def test_upgrade_preserves_data_and_is_repeatable(self):
        upgrade.upgrade_sqlite(self.path)
        self.assertEqual((31, 0), self.db.execute("SELECT * FROM schema_migrations").fetchone())
        self.assertEqual(('rca',), self.db.execute("SELECT category FROM knowledge_bases WHERE id='kb'").fetchone())
        self.assertEqual(('中文结果 🧪', '{"status":"failed"}'), self.db.execute(
            "SELECT content, execution_result FROM messages WHERE id='m'").fetchone())
        self.assertEqual(('agent', 0, None), self.db.execute(
            "SELECT agent_id, sandbox_config_tenant_id, host_workspace_dir FROM sessions WHERE id='s'").fetchone())
        self.db.execute("INSERT INTO tenant_skill_catalog(id,tenant_id,name) VALUES ('skill',1,'new skill')")
        self.db.commit()
        before = self.snapshot()
        upgrade.upgrade_sqlite(self.path)
        self.assertEqual(before, self.snapshot())

    def test_dirty_or_wrong_version_is_rejected_without_changes(self):
        for version, dirty in [(27, 1), (26, 0), (30, 0), (31, 0)]:
            with self.subTest(version=version, dirty=dirty):
                self.db.execute("UPDATE schema_migrations SET version=?, dirty=?", (version, dirty))
                self.db.commit()
                before = self.snapshot()
                with self.assertRaises(RuntimeError):
                    upgrade.upgrade_sqlite(self.path)
                self.assertEqual(before, self.snapshot())

    def test_incomplete_rca_schema_is_rejected_without_changes(self):
        self.db.execute("ALTER TABLE messages DROP COLUMN execution_result")
        self.db.commit()
        before = self.snapshot()
        with self.assertRaises(RuntimeError):
            upgrade.upgrade_sqlite(self.path)
        self.assertEqual(before, self.snapshot())

    def test_sql_failure_rolls_back_schema_and_version(self):
        # The real 000029 will fail; even the earlier 000027/28 must roll back.
        self.db.execute("ALTER TABLE sessions ADD COLUMN host_workspace_dir TEXT")
        self.db.commit()
        before = self.snapshot()
        with self.assertRaises((RuntimeError, sqlite3.DatabaseError)):
            upgrade.upgrade_sqlite(self.path)
        self.assertEqual(before, self.snapshot())

    def test_missing_database_is_not_created(self):
        missing = self.path.with_name("missing.db")
        with self.assertRaises((RuntimeError, sqlite3.DatabaseError)):
            upgrade.upgrade_sqlite(missing)
        self.assertFalse(missing.exists())


if __name__ == "__main__":
    unittest.main()
