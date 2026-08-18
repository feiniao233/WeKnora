import sys
import tempfile
import unittest
from pathlib import Path


sys.path.insert(0, str(Path(__file__).parent))

from rca_bootstrap import (  # noqa: E402
    EMBED_AGENT_TOOLS,
    KB_INDEXING_STRATEGY,
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
        if parts[:3] == ["api", "v1", "agents"] and parts[-1] == "embed-channels":
            return self.response([row for row in self.channels if row["agent_id"] == parts[3]])
        if parts[:3] == ["api", "v1", "embed-channels"]:
            # Real management GET intentionally never returns publish_token.
            row = self.find(self.channels, parts[3])
            return self.response({key: value for key, value in row.items() if key != "publish_token"})
        raise AssertionError(f"unexpected GET {path}")

    def post_json(self, path, payload):
        self.calls.append(("POST", path, payload))
        parts = path.strip("/").split("/")
        if parts == ["api", "v1", "knowledge-bases"]:
            row = {"id": self.new_id("kb"), "name": payload["name"]}
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
        if parts[:3] == ["api", "v1", "agents"] and parts[-1] == "embed-channels":
            row = {
                "id": self.new_id("channel"),
                "agent_id": parts[3],
                "publish_token": "publish-secret",
                **payload,
            }
            self.channels.append(row)
            return self.response(row)
        if parts[:3] == ["api", "v1", "embed-channels"] and parts[-1] == "rotate-token":
            row = self.find(self.channels, parts[3])
            row["publish_token"] = "rotated-secret"
            return self.response({"publish_token": row["publish_token"]})
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
        if parts[:3] == ["api", "v1", "agents"]:
            row = self.find(self.agents, parts[3])
            row.update(payload)
            return self.response(row)
        if parts[:3] == ["api", "v1", "embed-channels"]:
            row = self.find(self.channels, parts[3])
            row.update(payload)
            return self.response(row)
        raise AssertionError(f"unexpected PUT {path}")

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
            steel_origin="https://172.16.20.230",
            embed_origin="https://172.16.20.230:8443",
            ops_mcp_url="https://172.16.20.230/back/rca/mcp",
            webhook_url="https://172.16.20.230/back/rca/assistant/webhook",
            webhook_secret="webhook-secret",
        )
        runner = RCABootstrapper(config, "workspace-secret", "ops-secret", self.client)
        result = runner.run()
        runner.state.save(self.state_file)
        return result

    def test_idempotent_contract_and_token_recovery(self):
        first = self.run_bootstrap()
        second = self.run_bootstrap()

        for resource in ("knowledge_base_id", "mcp_service_id", "agent_id", "embed_channel_id"):
            self.assertEqual(first["resource_ids"][resource], second["resource_ids"][resource])
        self.assertEqual(len(self.client.kbs), 1)
        self.assertEqual(len(self.client.docs[first["resource_ids"]["knowledge_base_id"]]), 1)
        self.assertEqual(len(self.client.mcps), 1)
        self.assertEqual(len(self.client.agents), 1)
        self.assertEqual(len(self.client.channels), 1)
        self.assertEqual(self.state_file.stat().st_mode & 0o777, 0o600)

        kb_create = next(call for call in self.client.calls if call[:2] == ("POST", "/api/v1/knowledge-bases"))
        self.assertEqual(kb_create[2]["indexing_strategy"], KB_INDEXING_STRATEGY)
        mcp_create = next(call for call in self.client.calls if call[:2] == ("POST", "/api/v1/mcp-services"))
        self.assertNotIn("api_key", mcp_create[2]["auth_config"])
        credential_calls = [call for call in self.client.calls if call[0] == "PUT" and call[1].endswith("/credentials")]
        self.assertEqual(credential_calls[-1][2], {"api_key": "ops-secret"})
        self.assertEqual(self.client.agents[0]["config"]["allowed_tools"], EMBED_AGENT_TOOLS)
        self.assertIn("submit_rca_report", OPS_TOOLS)
        self.assertIn("submit_rca_report", self.client.agents[0]["config"]["system_prompt"])
        self.assertEqual(self.client.agents[0]["config"]["rerank_model_id"], "rerank-model")
        self.assertEqual(self.client.channels[0]["webhook_secret"], "webhook-secret")
        self.assertEqual(
            self.client.channels[0]["allowed_origins"],
            ["https://172.16.20.230", "https://172.16.20.230:8443"],
        )
        agent_put_calls = [call for call in self.client.calls if call[0] == "PUT" and call[1].startswith("/api/v1/agents/")]
        self.assertTrue(agent_put_calls)
        self.assertEqual(agent_put_calls[-1][2]["config"]["rerank_model_id"], "rerank-model")
        self.assertFalse(any(call[1].endswith("/rotate-token") for call in self.client.calls))

        state = self.state_file.read_text(encoding="utf-8").replace(
            '"embed_publish_token": "publish-secret"', '"embed_publish_token": ""'
        )
        self.state_file.write_text(state, encoding="utf-8")
        self.state_file.chmod(0o600)
        recovered = self.run_bootstrap()
        rotations = [call for call in self.client.calls if call[1].endswith("/rotate-token")]
        self.assertEqual(len(rotations), 1)
        self.assertEqual(recovered["embed"]["publish_token"], "rota...cret")

    def test_missing_knowledge_fails_before_api_writes(self):
        for item in self.knowledge_dir.iterdir():
            item.unlink()
        with self.assertRaisesRegex(RuntimeError, "No knowledge files"):
            self.run_bootstrap()
        self.assertEqual(self.client.calls, [])


if __name__ == "__main__":
    unittest.main()
