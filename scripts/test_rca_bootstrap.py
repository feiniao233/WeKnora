import sys
import tempfile
import unittest
from pathlib import Path


sys.path.insert(0, str(Path(__file__).parent))

from rca_bootstrap import (  # noqa: E402
    AGENT_NAME,
    AGENT_TOOLS,
    KB_INDEXING_STRATEGY,
    KB_NAME,
    LEGACY_AGENT_NAME,
    LEGACY_EMBED_CHANNEL_NAME,
    LEGACY_KB_NAME,
    OPS_TOOLS,
    RCAConfig,
    RCABootstrapper,
)


class FakeClient:
    def __init__(self):
        self.calls = []
        self.kbs = []
        self.docs = {}
        self.mcps = []
        self.agents = []
        self.channels = []
        self.next_id = 1

    def response(self, data):
        return {"success": True, "data": data}

    def new_id(self, prefix):
        value = f"{prefix}-{self.next_id}"
        self.next_id += 1
        return value

    def find(self, rows, row_id):
        return next(row for row in rows if row["id"] == row_id)

    def get_json(self, path):
        self.calls.append(("GET", path, None))
        parts = path.split("?")[0].strip("/").split("/")
        if parts == ["api", "v1", "knowledge-bases"]:
            return self.response(self.kbs)
        if parts[:3] == ["api", "v1", "knowledge-bases"] and parts[-1] == "knowledge":
            return self.response(self.docs.get(parts[3], []))
        if parts == ["api", "v1", "mcp-services"]:
            return self.response(self.mcps)
        if parts == ["api", "v1", "agents"]:
            return self.response(self.agents)
        if parts[:3] == ["api", "v1", "embed-channels"]:
            return self.response(self.find(self.channels, parts[3]))
        raise AssertionError(f"unexpected GET {path}")

    def post_json(self, path, payload):
        self.calls.append(("POST", path, payload))
        parts = path.strip("/").split("/")
        if parts == ["api", "v1", "knowledge-bases"]:
            row = {"id": self.new_id("kb"), "name": payload["name"], "category": payload["category"]}
            self.kbs.append(row)
            self.docs[row["id"]] = []
            return self.response(row)
        if parts == ["api", "v1", "mcp-services"]:
            row = {"id": self.new_id("mcp"), **payload}
            self.mcps.append(row)
            return self.response(row)
        if parts[:3] == ["api", "v1", "mcp-services"] and parts[-1] == "test":
            return self.response({"success": True, "tools": [{"name": name} for name in OPS_TOOLS]})
        if parts == ["api", "v1", "agents"]:
            row = {"id": self.new_id("agent"), **payload}
            self.agents.append(row)
            return self.response(row)
        raise AssertionError(f"unexpected POST {path}")

    def put_json(self, path, payload):
        self.calls.append(("PUT", path, payload))
        parts = path.strip("/").split("/")
        if parts[:3] == ["api", "v1", "mcp-services"]:
            row = self.find(self.mcps, parts[3])
            if parts[-1] == "credentials":
                row["credentials_configured"] = bool(payload.get("api_key"))
                return self.response({"fields": {"api_key": {"configured": True}}})
            row.update(payload)
            return self.response(row)
        if parts[:3] == ["api", "v1", "knowledge-bases"]:
            row = self.find(self.kbs, parts[3])
            row.update(payload)
            return self.response(row)
        if parts[:3] == ["api", "v1", "agents"]:
            row = self.find(self.agents, parts[3])
            row.update(payload)
            return self.response(row)
        raise AssertionError(f"unexpected PUT {path}")

    def delete_json(self, path):
        self.calls.append(("DELETE", path, None))
        parts = path.strip("/").split("/")
        if parts[:3] == ["api", "v1", "embed-channels"]:
            self.channels = [row for row in self.channels if row["id"] != parts[3]]
            return self.response(None)
        raise AssertionError(f"unexpected DELETE {path}")

    def post_multipart_file(self, path, file_path):
        self.calls.append(("FILE", path, file_path.name))
        kb_id = path.strip("/").split("/")[3]
        row = {"id": self.new_id("doc"), "file_name": file_path.name, "title": file_path.name}
        self.docs[kb_id].append(row)
        return self.response(row)


class BootstrapTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        root = Path(self.temp.name)
        self.state_file = root / "state.json"
        self.knowledge_dir = root / "knowledge"
        self.knowledge_dir.mkdir()
        (self.knowledge_dir / "runbook.md").write_text("runbook", encoding="utf-8")
        self.client = FakeClient()

    def tearDown(self):
        self.temp.cleanup()

    def run_bootstrap(self):
        config = RCAConfig(
            base_url="http://127.0.0.1",
            model_id="chat-model",
            rerank_model_id="rerank-model",
            embedding_model_id="embedding-model",
            knowledge_dir=str(self.knowledge_dir),
            state_file=self.state_file,
            ops_mcp_url="https://172.16.20.230/back/rca/mcp",
        )
        runner = RCABootstrapper(config, "workspace-secret", "ops-secret", self.client)
        result = runner.run()
        runner.state.save(self.state_file)
        return result

    def test_idempotent_contract(self):
        self.client.channels.append({"id": "legacy-channel", "name": LEGACY_EMBED_CHANNEL_NAME})
        self.state_file.write_text(
            '{"embed_channel_id":"legacy-channel","embed_publish_token":"legacy-token"}',
            encoding="utf-8",
        )
        first = self.run_bootstrap()
        second = self.run_bootstrap()

        for resource in ("knowledge_base_id", "mcp_service_id", "agent_id"):
            self.assertEqual(first["resource_ids"][resource], second["resource_ids"][resource])
        self.assertEqual(len(self.client.kbs), 1)
        self.assertEqual(len(self.client.docs[first["resource_ids"]["knowledge_base_id"]]), 1)
        self.assertEqual(len(self.client.mcps), 1)
        self.assertEqual(len(self.client.agents), 1)
        self.assertEqual(self.client.channels, [])
        self.assertEqual(self.state_file.stat().st_mode & 0o777, 0o600)
        self.assertNotIn("embed_channel_id", self.state_file.read_text(encoding="utf-8"))
        self.assertNotIn("embed_publish_token", self.state_file.read_text(encoding="utf-8"))

        kb_create = next(call for call in self.client.calls if call[:2] == ("POST", "/api/v1/knowledge-bases"))
        self.assertEqual(kb_create[2]["indexing_strategy"], KB_INDEXING_STRATEGY)
        self.assertEqual(kb_create[2]["category"], "general")
        mcp_create = next(call for call in self.client.calls if call[:2] == ("POST", "/api/v1/mcp-services"))
        self.assertNotIn("api_key", mcp_create[2]["auth_config"])
        credential_calls = [call for call in self.client.calls if call[0] == "PUT" and call[1].endswith("/credentials")]
        self.assertEqual(credential_calls[-1][2], {"api_key": "ops-secret"})
        self.assertEqual(self.client.agents[0]["config"]["allowed_tools"], AGENT_TOOLS)
        self.assertIn("submit_rca_report", OPS_TOOLS)
        self.assertIn("submit_rca_report", self.client.agents[0]["config"]["system_prompt"])
        self.assertEqual(self.client.agents[0]["config"]["rerank_model_id"], "rerank-model")
        agent_put_calls = [call for call in self.client.calls if call[0] == "PUT" and call[1].startswith("/api/v1/agents/")]
        self.assertTrue(agent_put_calls)
        self.assertEqual(agent_put_calls[-1][2]["config"]["rerank_model_id"], "rerank-model")

    def test_empty_knowledge_directory_creates_resources_without_uploads(self):
        for item in self.knowledge_dir.iterdir():
            item.unlink()
        result = self.run_bootstrap()
        self.assertEqual(result["status"], "ok")
        self.assertFalse(any(call[0] == "FILE" for call in self.client.calls))

    def test_renames_legacy_resources_without_creating_duplicates(self):
        self.client.kbs.append({"id": "kb-legacy", "name": LEGACY_KB_NAME, "category": "general"})
        self.client.docs["kb-legacy"] = []
        self.client.agents.append({"id": "agent-legacy", "name": LEGACY_AGENT_NAME})

        self.run_bootstrap()

        self.assertEqual(len(self.client.kbs), 1)
        self.assertEqual(self.client.kbs[0]["name"], KB_NAME)
        self.assertEqual(len(self.client.agents), 1)
        self.assertEqual(self.client.agents[0]["name"], AGENT_NAME)

    def test_does_not_delete_unrelated_embed_channel(self):
        self.client.channels.append({"id": "legacy-channel", "name": "Other channel"})
        self.state_file.write_text('{"embed_channel_id":"legacy-channel"}', encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "not created by RCA bootstrap"):
            self.run_bootstrap()
        self.assertEqual(len(self.client.channels), 1)

    def test_ip_configuration_change_routes_to_conflict_verification(self):
        skill = (Path(__file__).parents[1] / "skills/preloaded/rca-diagnosis/SKILL.md").read_text(encoding="utf-8")
        self.assertIn("IP 地址配置新增、删除或地址列表变化 -> `ip-conflict`", skill)
        self.assertIn("只表示选择 IP 冲突核验流程，不代表已确认存在地址冲突", skill)

    def test_registered_peer_ip_is_ip_conflict_scene_match(self):
        reference = (
            Path(__file__).parents[1] / "skills/preloaded/rca-diagnosis/references/ip-conflict.md"
        ).read_text(encoding="utf-8")
        self.assertIn("配置变更新增地址与另一资产登记地址重复", reference)
        self.assertIn("结论等级为 `场景匹配`", reference)
        self.assertIn("不得因缺少动态重叠声明降为 `未知`", reference)
        self.assertIn("不等于人工确认两台设备同时在线使用", reference)
        self.assertIn("无候选资产时，结论为 `未知`", reference)
        self.assertIn("多个候选资产或重新分配证据相互矛盾时，结论为 `存在歧义`", reference)


if __name__ == "__main__":
    unittest.main()
