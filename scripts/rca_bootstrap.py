#!/usr/bin/env python3
"""usage: rca-bootstrap [--dry-run] [--base-url BASE_URL] ...

Minimal idempotent bootstrap tool for WeKnora RCA resources.
"""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import ssl
import tempfile
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple


STATE_VERSION = 1
DEFAULT_BASE_URL = "http://127.0.0.1"
DEFAULT_API_KEY_FILE = "/root/.local/share/weknora/bootstrap-api-key"
DEFAULT_OPS_MCP_KEY_ENV = "OPS_MCP_API_KEY"
DEFAULT_KNOWLEDGE_DIR = "/root/code/work/rca-app/docs/knowledge"
DEFAULT_STATE_FILE = "/root/.local/share/weknora/rca-bootstrap.json"
DEFAULT_STEEL_ORIGIN = "https://172.16.20.230"
DEFAULT_EMBED_ORIGIN = "https://172.16.20.230:8443"
DEFAULT_MCP_URL = "https://172.16.20.230/back/rca/mcp"
DEFAULT_WEBHOOK_URL = "https://172.16.20.230/back/rca/assistant/webhook"
DEFAULT_WEBHOOK_SECRET_FILE = "/root/.local/share/weknora/weknora-webhook-secret"

KB_NAME = "RCA 运维知识库"
MCP_NAME = "Steel Ops MCP (只读)"
AGENT_NAME = "RCA 诊断助手"
EMBED_CHANNEL_NAME = "Steel RCA 助手"
EMBED_AGENT_TOOLS = [
    "thinking",
    "todo_write",
    "knowledge_search",
    "grep_chunks",
    "list_knowledge_chunks",
    "get_document_info",
]
OPS_TOOLS = [
    "resolve_alarm",
    "get_asset_context",
    "get_topology_context",
    "query_operational_evidence",
]
KB_INDEXING_STRATEGY = {
    "vector_enabled": True,
    "keyword_enabled": True,
    "wiki_enabled": False,
    "graph_enabled": False,
}


class ApiError(RuntimeError):
    """HTTP call failed."""

    def __init__(self, method: str, url: str, status: int, body: str) -> None:
        super().__init__(f"{method} {url} -> {status}: {body}")
        self.method = method
        self.url = url
        self.status = status
        self.body = body


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="rca_bootstrap.py",
        description="Bootstrap RCA knowledge base, MCP, Agent, and Embed channel.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
        add_help=True,
    )
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL, help="WeKnora API base URL.")
    parser.add_argument(
        "--api-key-file",
        default=DEFAULT_API_KEY_FILE,
        help="Workspace API key file path (secret never printed).",
    )
    parser.add_argument(
        "--ops-mcp-url",
        default=DEFAULT_MCP_URL,
        help="Ops MCP endpoint URL.",
    )
    parser.add_argument(
        "--ops-mcp-key-file",
        default=None,
        help="Ops MCP API key file path.",
    )
    parser.add_argument(
        "--model-id",
        required=True,
        help="Chat model id for the ReAct agent.",
    )
    parser.add_argument(
        "--rerank-model-id",
        required=True,
        help="Rerank model id for the agent.",
    )
    parser.add_argument(
        "--embedding-model-id",
        required=True,
        help="Embedding model id used by RCA knowledge base.",
    )
    parser.add_argument(
        "--knowledge-dir",
        default=DEFAULT_KNOWLEDGE_DIR,
        help="Directory containing local docs to ingest.",
    )
    parser.add_argument(
        "--state-file",
        default=DEFAULT_STATE_FILE,
        help="State cache file path, stored mode 0600.",
    )
    parser.add_argument(
        "--steel-origin",
        default=DEFAULT_STEEL_ORIGIN,
        help="Steel parent page allowed origin.",
    )
    parser.add_argument(
        "--embed-origin",
        default=DEFAULT_EMBED_ORIGIN,
        help="Public WeKnora iframe allowed origin.",
    )
    parser.add_argument("--webhook-url", default=DEFAULT_WEBHOOK_URL, help="Signed assistant event webhook URL.")
    parser.add_argument(
        "--webhook-secret-file",
        default=DEFAULT_WEBHOOK_SECRET_FILE,
        help="Webhook HMAC secret file path.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print endpoint plan only, no key/env/network calls.",
    )
    parser.add_argument(
        "--ops-mcp-key-env",
        default=DEFAULT_OPS_MCP_KEY_ENV,
        help="Fallback env var for ops MCP key when file missing.",
    )
    return parser.parse_args(argv)


def read_file_secret(path: str) -> Optional[str]:
    if not path:
        return None
    file_path = Path(path).expanduser()
    if not file_path.exists():
        return None
    try:
        value = file_path.read_text(encoding="utf-8").strip()
    except OSError:
        return None
    return value or None


def read_secret(
    file_path: Optional[str],
    env_name: str,
) -> Optional[str]:
    value = read_file_secret(file_path or "")
    if value:
        return value
    env_value = os.environ.get(env_name or "", "").strip() if env_name else ""
    return env_value or None


def redact_token(token: Optional[str]) -> Optional[str]:
    if not token:
        return None
    value = token.strip()
    if len(value) <= 8:
        return "***"
    return f"{value[:4]}...{value[-4:]}"


def unwrap_api_payload(payload: Any) -> Any:
    if not isinstance(payload, dict):
        return payload
    if "data" in payload and set(payload.keys()) <= {"success", "data"}:
        return payload.get("data")
    if "success" in payload and "data" in payload and isinstance(payload["data"], (list, dict)):
        return payload["data"]
    return payload


def as_list(payload: Any) -> List[Dict[str, Any]]:
    data = unwrap_api_payload(payload)
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        for key in ("items", "data", "knowledge_bases", "mcp_services", "agents", "embed_channels"):
            value = data.get(key)
            if isinstance(value, list):
                return value
        return []
    return []


def as_string_list(payload: Any) -> List[str]:
    data = unwrap_api_payload(payload)
    if isinstance(data, list):
        return [str(item) for item in data]
    if isinstance(data, dict):
        names = data.get("tools") or []
        if isinstance(names, list):
            normalized = []
            for item in names:
                if isinstance(item, str):
                    normalized.append(item)
                elif isinstance(item, dict):
                    for key in ("name", "tool_name", "id"):
                        value = item.get(key)
                        if isinstance(value, str):
                            normalized.append(value)
                            break
            return normalized
        if isinstance(data, dict) and data.get("success") is False:
            raise ApiError("POST", "<unknown>", 0, json.dumps(data))
    return []


def toolset_matches(expected: Iterable[str], got: Iterable[str]) -> bool:
    expected_set = set(expected)
    got_set = set(got)
    return expected_set == got_set


def dedupe_existing_names(rows: Iterable[Dict[str, Any]]) -> set:
    names = set()
    for row in rows:
        for key in ("file_name", "filename", "title", "name"):
            value = row.get(key)
            if isinstance(value, str) and value:
                names.add(value)
    return names


def collect_knowledge_files(root: str) -> List[Path]:
    root_path = Path(root)
    if not root_path.exists():
        return []
    return sorted([p for p in root_path.rglob("*") if p.is_file()])


def find_by_name(rows: Iterable[Dict[str, Any]], target: str) -> Optional[Dict[str, Any]]:
    for row in rows:
        if row.get("name") == target:
            return row
    return None


class ApiClient:
    """Small JSON+multipart client with API key auth."""

    def __init__(self, base_url: str, api_key: str, timeout: int = 30) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    def _join_url(self, path: str) -> str:
        if not path.startswith("/"):
            path = "/" + path
        if not path.startswith("/api/v1/"):
            raise ValueError(f"Only /api/v1/... paths are supported: {path}")
        return f"{self.base_url}{path}"

    def _build_headers(self, headers: Optional[Dict[str, str]] = None) -> Dict[str, str]:
        outgoing = {"User-Agent": "weknora-rca-bootstrap/1.0", "Accept": "application/json"}
        if self.api_key:
            outgoing["X-API-Key"] = self.api_key
        if headers:
            outgoing.update(headers)
        return outgoing

    def _request(
        self,
        method: str,
        path: str,
        body: Optional[bytes] = None,
        headers: Optional[Dict[str, str]] = None,
    ) -> Any:
        url = self._join_url(path)
        req = urllib.request.Request(url, data=body, method=method)
        for key, value in self._build_headers(headers).items():
            req.add_header(key, value)
        try:
            with urllib.request.urlopen(req, context=ssl.create_default_context(), timeout=self.timeout) as resp:
                status = int(getattr(resp, "status", 200))
                if status in (204, 205):
                    return None
                raw = resp.read().decode("utf-8", errors="replace")
                if not raw:
                    return None
                return json.loads(raw)
        except urllib.error.HTTPError as exc:
            try:
                body = exc.read().decode("utf-8", errors="replace")
            except Exception:
                body = ""
            raise ApiError(method, url, exc.code, body or str(exc)) from exc

    def get_json(self, path: str) -> Any:
        return self._request("GET", path, headers={"Accept": "application/json"})

    def post_json(self, path: str, payload: Dict[str, Any]) -> Any:
        return self._request(
            "POST",
            path,
            body=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json"},
        )

    def put_json(self, path: str, payload: Dict[str, Any]) -> Any:
        return self._request(
            "PUT",
            path,
            body=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json"},
        )

    def post_multipart_file(self, path: str, file_path: Path, field_name: str = "file") -> Any:
        boundary = "----weknora-bootstrap-boundary"
        filename = file_path.name
        ctype, _ = mimetypes.guess_type(filename)
        if not ctype:
            ctype = "application/octet-stream"
        with file_path.open("rb") as stream:
            payload = (
                f"--{boundary}\r\n"
                f'Content-Disposition: form-data; name="{field_name}"; filename="{filename}"\r\n'
                f"Content-Type: {ctype}\r\n\r\n"
            ).encode("utf-8")
            payload += stream.read()
            payload += f"\r\n--{boundary}--\r\n".encode("utf-8")

        return self._request(
            "POST",
            path,
            body=payload,
            headers={"Content-Type": f"multipart/form-data; boundary={boundary}"},
        )


class BootstrapState:
    def __init__(self, data: Optional[Dict[str, Any]] = None) -> None:
        self.data = data or {}

    @classmethod
    def load(cls, path: Path) -> "BootstrapState":
        if not path.exists():
            return cls({})
        try:
            raw = path.read_text(encoding="utf-8")
            return cls(json.loads(raw) if raw else {})
        except (OSError, json.JSONDecodeError):
            return cls({})

    def save(self, path: Path) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        payload = json.dumps(self.data, ensure_ascii=False, indent=2).encode("utf-8")
        fd, tmp_name = tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=str(path.parent))
        try:
            with os.fdopen(fd, "wb") as stream:
                fd = -1
                stream.write(payload)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(tmp_name, path)
        finally:
            if fd >= 0:
                os.close(fd)
            if os.path.exists(tmp_name):
                os.unlink(tmp_name)

    def get(self, key: str, default=None):
        return self.data.get(key, default)

    def set_id(self, key: str, value: str) -> None:
        self.data[key] = value


class RCAConfig:
    def __init__(
        self,
        base_url: str,
        model_id: str,
        rerank_model_id: str,
        embedding_model_id: str,
        knowledge_dir: str,
        state_file: Path,
        steel_origin: str,
        embed_origin: str,
        ops_mcp_url: str,
        webhook_url: str,
        webhook_secret: str,
        dry_run: bool = False,
    ) -> None:
        self.base_url = base_url
        self.model_id = model_id
        self.rerank_model_id = rerank_model_id
        self.embedding_model_id = embedding_model_id
        self.knowledge_dir = knowledge_dir
        self.state_file = state_file
        self.steel_origin = steel_origin
        self.embed_origin = embed_origin
        self.ops_mcp_url = ops_mcp_url
        self.webhook_url = webhook_url
        self.webhook_secret = webhook_secret
        self.dry_run = dry_run


class RCABootstrapper:
    def __init__(
        self,
        config: RCAConfig,
        workspace_api_key: str,
        ops_mcp_api_key: str,
        client: Optional[ApiClient] = None,
    ) -> None:
        self.config = config
        self.ops_mcp_api_key = ops_mcp_api_key
        self.client = client or ApiClient(config.base_url, workspace_api_key)
        self.state = BootstrapState.load(config.state_file)
        self.summary: Dict[str, Any] = {
            "dry_run": config.dry_run,
            "phases": [],
            "state_file": str(config.state_file),
            "status": "started",
            "resource_ids": {},
            "embed": {},
        }

    def _record(self, phase: str, **fields: Any) -> None:
        self.summary["phases"].append({"phase": phase, **fields})

    def _find_by_name(self, rows: List[Dict[str, Any]], name: str) -> Optional[Dict[str, Any]]:
        return find_by_name(rows, name)

    def _get(self, path: str) -> Any:
        return self.client.get_json(path)

    def _post(self, path: str, payload: Dict[str, Any]) -> Any:
        return self.client.post_json(path, payload)

    def _put(self, path: str, payload: Dict[str, Any]) -> Any:
        return self.client.put_json(path, payload)

    def _post_file(self, path: str, file_path: Path) -> Any:
        return self.client.post_multipart_file(path, file_path)

    def _ensure_kb(self) -> str:
        if self.config.dry_run:
            self._record(
                "knowledge_base",
                endpoint="/api/v1/knowledge-bases",
                action="ensure (planned)",
            )
            existing = self.state.get("knowledge_base_id")
            if existing:
                return existing
            dummy = "kb-placeholder"
            self.state.set_id("knowledge_base_id", dummy)
            return dummy

        self._record("knowledge_base", endpoint="/api/v1/knowledge-bases", action="ensure")
        rows = as_list(self._get("/api/v1/knowledge-bases"))
        found = self._find_by_name(rows, KB_NAME)
        if found:
            kb_id = str(found["id"])
            self._record(
                "knowledge_base",
                action="reused",
                endpoint="/api/v1/knowledge-bases",
                id=kb_id,
            )
        else:
            created = unwrap_api_payload(
                self._post(
                    "/api/v1/knowledge-bases",
                    {
                        "name": KB_NAME,
                        "description": "RCA 运维知识库（预置）",
                        "type": "document",
                        "embedding_model_id": self.config.embedding_model_id,
                        "indexing_strategy": KB_INDEXING_STRATEGY,
                    },
                )
            )
            if not isinstance(created, dict) or "id" not in created:
                raise RuntimeError("Unexpected knowledge base create payload")
            kb_id = str(created["id"])
            self._record("knowledge_base", action="created", endpoint="/api/v1/knowledge-bases", id=kb_id)

        known_rows = as_list(self._get(f"/api/v1/knowledge-bases/{kb_id}/knowledge"))
        existing = dedupe_existing_names(known_rows)
        for item in collect_knowledge_files(self.config.knowledge_dir):
            filename = item.name
            if filename in existing:
                self._record(
                    "knowledge_file",
                    action="skipped",
                    endpoint="/api/v1/knowledge-bases/{id}/knowledge/file",
                    name=filename,
                )
                continue
            uploaded = unwrap_api_payload(self._post_file(f"/api/v1/knowledge-bases/{kb_id}/knowledge/file", item))
            if not isinstance(uploaded, dict):
                raise RuntimeError(f"Knowledge upload unexpected payload for {filename}")
            existing.add(uploaded.get("file_name") or uploaded.get("title") or filename)
            self._record(
                "knowledge_file",
                action="uploaded",
                endpoint="/api/v1/knowledge-bases/{id}/knowledge/file",
                name=filename,
            )
        self.state.set_id("knowledge_base_id", kb_id)
        return kb_id

    def _ensure_mcp(self) -> str:
        if self.config.dry_run:
            self._record("mcp", endpoint="/api/v1/mcp-services", action="ensure (planned)")
            existing = self.state.get("mcp_service_id")
            return existing or "mcp-placeholder"

        self._record("mcp", endpoint="/api/v1/mcp-services", action="ensure")
        rows = as_list(self._get("/api/v1/mcp-services"))
        found = self._find_by_name(rows, MCP_NAME)
        if found:
            mcp_id = str(found["id"])
            self._record("mcp", action="reused", id=mcp_id)
            self._put(
                f"/api/v1/mcp-services/{urllib.parse.quote(mcp_id, safe='')}",
                {
                    "name": MCP_NAME,
                    "description": "Steel 运维 MCP 读权限代理",
                    "enabled": True,
                    "transport_type": "http-streamable",
                    "url": self.config.ops_mcp_url,
                    "auth_config": {
                        "auth_type": "api_key",
                        "api_key_header": "X-API-Key",
                    },
                    "advanced_config": {"timeout": 30, "retry_count": 2, "retry_delay": 2},
                },
            )
        else:
            created = unwrap_api_payload(
                self._post(
                    "/api/v1/mcp-services",
                    {
                        "name": MCP_NAME,
                        "description": "Steel 运维 MCP 只读入口",
                        "enabled": True,
                        "transport_type": "http-streamable",
                        "url": self.config.ops_mcp_url,
                        "auth_config": {
                            "auth_type": "api_key",
                            "api_key_header": "X-API-Key",
                        },
                        "advanced_config": {"timeout": 30, "retry_count": 2, "retry_delay": 2},
                    },
                )
            )
            if not isinstance(created, dict) or "id" not in created:
                raise RuntimeError("Unexpected MCP create payload")
            mcp_id = str(created["id"])
            self._record("mcp", action="created", id=mcp_id)

        # Store/rewrite ops MCP secret through dedicated credentials subresource.
        self._put(
            f"/api/v1/mcp-services/{urllib.parse.quote(mcp_id, safe='')}/credentials",
            {"api_key": self.ops_mcp_api_key},
        )

        test_result = unwrap_api_payload(self._post(f"/api/v1/mcp-services/{urllib.parse.quote(mcp_id, safe='')}/test", {}))
        if isinstance(test_result, dict) and test_result.get("success") is False:
            raise RuntimeError(f"MCP test failed: {json.dumps(test_result)}")
        tools = as_string_list(test_result)
        if not toolset_matches(OPS_TOOLS, tools):
            raise RuntimeError(
                "MCP tool allowlist mismatch. "
                f"expected={sorted(OPS_TOOLS)} actual={sorted(set(tools))}"
            )
        self.state.set_id("mcp_service_id", mcp_id)
        return mcp_id

    def _agent_payload(self, kb_id: str, mcp_id: str) -> Dict[str, Any]:
        return {
            "name": AGENT_NAME,
            "description": "RCA 运维诊断智能体（只读）",
            "avatar": "",
            "config": {
                "agent_mode": "smart-reasoning",
                "agent_type": "custom",
                "system_prompt": (
                    "你是 RCA 运维诊断助手。仅基于知识库内容与 MCP 只读工具给出可核验证据链结论，"
                    "外部只读 MCP 工具名为：resolve_alarm, get_asset_context, "
                    "get_topology_context, query_operational_evidence。"
                    "禁止发散，不执行或声称已经执行写操作；验证与处置建议必须明确交由人工执行。"
                ),
                "model_id": self.config.model_id,
                "rerank_model_id": self.config.rerank_model_id,
                "max_iterations": 8,
                "llm_call_timeout": 90,
                "mcp_selection_mode": "selected",
                "mcp_services": [mcp_id],
                "skills_selection_mode": "selected",
                "selected_skills": ["rca-diagnosis"],
                "kb_selection_mode": "selected",
                "knowledge_bases": [kb_id],
                "web_search_enabled": False,
                "image_upload_enabled": False,
                "audio_upload_enabled": False,
                "citation_enabled": True,
                "multi_turn_enabled": True,
                "memory_enabled": False,
                "temperature": 0.1,
                "allowed_tools": EMBED_AGENT_TOOLS,
            },
        }

    def _ensure_agent(self, kb_id: str, mcp_id: str) -> str:
        if self.config.dry_run:
            self._record("agent", endpoint="/api/v1/agents", action="ensure (planned)")
            existing = self.state.get("agent_id")
            return existing or "agent-placeholder"

        payload = self._agent_payload(kb_id, mcp_id)
        self._record("agent", endpoint="/api/v1/agents", action="ensure")
        rows = as_list(self._get("/api/v1/agents"))
        found = self._find_by_name(rows, AGENT_NAME)
        if found:
            agent_id = str(found["id"])
            self._put(f"/api/v1/agents/{urllib.parse.quote(agent_id, safe='')}", payload)
            self._record("agent", action="reused", endpoint="/api/v1/agents", id=agent_id)
        else:
            created = unwrap_api_payload(self._post("/api/v1/agents", payload))
            if not isinstance(created, dict) or "id" not in created:
                raise RuntimeError("Unexpected agent create payload")
            agent_id = str(created["id"])
            self._record("agent", action="created", endpoint="/api/v1/agents", id=agent_id)
        self.state.set_id("agent_id", agent_id)
        return agent_id

    def _ensure_embed_channel(self, agent_id: str) -> Tuple[str, str]:
        if self.config.dry_run:
            self._record(
                "embed_channel",
                endpoint="/api/v1/agents/:id/embed-channels",
                action="ensure (planned)",
            )
            existing = self.state.get("embed_channel_id")
            token = self.state.get("embed_publish_token") or "token-placeholder"
            return existing or "embed-channel-placeholder", token

        self._record(
            "embed_channel",
            endpoint="/api/v1/agents/:id/embed-channels",
            action="ensure",
        )
        rows = as_list(self._get(f"/api/v1/agents/{urllib.parse.quote(agent_id, safe='')}/embed-channels"))
        found = self._find_by_name(rows, EMBED_CHANNEL_NAME)
        cfg = {
            "name": EMBED_CHANNEL_NAME,
            "enabled": True,
            "allowed_origins": list(dict.fromkeys([self.config.steel_origin, self.config.embed_origin])),
            "rate_limit_per_minute": 120,
            "rate_limit_per_day": 10000,
            "allow_web_search": False,
            "allow_file_upload": False,
            "default_locale": "zh-CN",
            "webhook_url": self.config.webhook_url,
            "webhook_secret": self.config.webhook_secret,
        }
        if found:
            channel_id = str(found["id"])
            self._put(f"/api/v1/embed-channels/{urllib.parse.quote(channel_id, safe='')}", cfg)
            state_channel_id = str(self.state.get("embed_channel_id", ""))
            state_token = str(self.state.get("embed_publish_token", ""))
            if state_channel_id == channel_id and state_token:
                self.state.set_id("embed_channel_id", channel_id)
                self.state.set_id("embed_publish_token", state_token)
                self._record("embed_channel", action="updated", id=channel_id)
                return channel_id, state_token
            rotate = unwrap_api_payload(
                self._post(
                    f"/api/v1/embed-channels/{urllib.parse.quote(channel_id, safe='')}/rotate-token",
                    {},
                )
            )
            if not isinstance(rotate, dict) or "publish_token" not in rotate:
                raise RuntimeError("Rotate token response missing publish_token")
            server_token = str(rotate["publish_token"])
            self.state.set_id("embed_channel_id", channel_id)
            self.state.set_id("embed_publish_token", server_token)
            self._record("embed_channel", action="rotated", id=channel_id)
            self._record("embed_channel", action="updated", id=channel_id)
            return channel_id, server_token
        else:
            created = unwrap_api_payload(
                self._post(
                    f"/api/v1/agents/{urllib.parse.quote(agent_id, safe='')}/embed-channels",
                    cfg,
                )
            )
            if not isinstance(created, dict) or "id" not in created:
                raise RuntimeError("Unexpected embed channel create payload")
            channel_id = str(created["id"])
            publish_token = str(created.get("publish_token", ""))
            self.state.set_id("embed_channel_id", channel_id)
            if not publish_token:
                fresh = unwrap_api_payload(
                    self._post(
                        f"/api/v1/embed-channels/{urllib.parse.quote(channel_id, safe='')}/rotate-token",
                        {},
                    )
                )
                if not isinstance(fresh, dict) or "publish_token" not in fresh:
                    raise RuntimeError("Embed channel create response missing publish_token")
                publish_token = str(fresh["publish_token"])
            self.state.set_id("embed_publish_token", publish_token)
            self._record("embed_channel", action="created", id=channel_id)
            return channel_id, publish_token


    def run(self) -> Dict[str, Any]:
        if self.config.dry_run:
            self.summary["status"] = "dry_run"
            self.summary["resource_ids"]["knowledge_base_id"] = self.state.get("knowledge_base_id")
            self.summary["resource_ids"]["mcp_service_id"] = self.state.get("mcp_service_id")
            self.summary["resource_ids"]["agent_id"] = self.state.get("agent_id")
            self.summary["resource_ids"]["embed_channel_id"] = self.state.get("embed_channel_id")
            self.summary["embed"]["publish_token"] = redact_token(self.state.get("embed_publish_token"))
            return self.summary

        if (
            not self.config.model_id.strip()
            or not self.config.rerank_model_id.strip()
            or not self.config.embedding_model_id.strip()
        ):
            raise RuntimeError("chat, rerank, and embedding model ids are required")
        if not collect_knowledge_files(self.config.knowledge_dir):
            raise RuntimeError(f"No knowledge files found under {self.config.knowledge_dir}")

        kb_id = self._ensure_kb()
        mcp_id = self._ensure_mcp()
        agent_id = self._ensure_agent(kb_id, mcp_id)
        channel_id, publish_token = self._ensure_embed_channel(agent_id)
        self.state.set_id("version", STATE_VERSION)
        self.state.set_id("knowledge_base_id", kb_id)
        self.state.set_id("mcp_service_id", mcp_id)
        self.state.set_id("agent_id", agent_id)
        self.state.set_id("embed_channel_id", channel_id)
        self.state.set_id("embed_publish_token", publish_token)

        self.summary["resource_ids"] = {
            "knowledge_base_id": kb_id,
            "mcp_service_id": mcp_id,
            "agent_id": agent_id,
            "embed_channel_id": channel_id,
        }
        self.summary["embed"]["publish_token"] = redact_token(publish_token)
        self.summary["status"] = "ok"
        self.summary["ops_mcp_tools"] = OPS_TOOLS
        return self.summary


def dry_run_summary(config: RCAConfig, state: BootstrapState) -> Dict[str, Any]:
    return {
        "usage": "rca_bootstrap.py --model-id <id> --rerank-model-id <id> --embedding-model-id <id>",
        "dry_run": True,
        "phases": [
            {"phase": "knowledge_base", "endpoint": "/api/v1/knowledge-bases", "action": "get/create-by-name"},
            {"phase": "knowledge_files", "endpoint": "/api/v1/knowledge-bases/{id}/knowledge/file", "action": "dedupe-upload"},
            {"phase": "mcp", "endpoint": "/api/v1/mcp-services", "action": "get/create-by-name"},
            {"phase": "mcp_test", "endpoint": "/api/v1/mcp-services/{id}/test", "action": f"exact-match-tools {OPS_TOOLS}"},
            {"phase": "agent", "endpoint": "/api/v1/agents", "action": "get/create-by-name"},
            {"phase": "embed", "endpoint": "/api/v1/agents/{id}/embed-channels", "action": "get/create-by-name"},
            {"phase": "embed_rotate", "endpoint": "/api/v1/embed-channels/{id}/rotate-token", "action": "conditional"},
        ],
        "config": {
            "base_url": config.base_url,
            "kb": KB_NAME,
            "mcp_name": MCP_NAME,
            "agent_name": AGENT_NAME,
            "embed_channel_name": EMBED_CHANNEL_NAME,
            "steel_origin": config.steel_origin,
            "embed_origin": config.embed_origin,
            "rerank_model_id": config.rerank_model_id,
            "state_file": str(config.state_file),
            "state_has_token": bool(state.get("embed_publish_token")),
            "state_publish_token": redact_token(state.get("embed_publish_token")),
        },
    }


def build_bootstrap_config(args: argparse.Namespace) -> Tuple[RCAConfig, str, str]:
    workspace_key = read_secret(args.api_key_file, "WEKNORA_API_KEY")
    if not workspace_key:
        raise RuntimeError("Workspace API key missing: set --api-key-file or WEKNORA_API_KEY")

    ops_key = read_secret(args.ops_mcp_key_file, args.ops_mcp_key_env)
    if not args.dry_run and not ops_key:
        raise RuntimeError("Ops MCP key missing: set --ops-mcp-key-file or OPS_MCP_API_KEY")
    webhook_secret = read_file_secret(args.webhook_secret_file)
    if not webhook_secret:
        raise RuntimeError("Webhook secret missing: set --webhook-secret-file")

    return (
        RCAConfig(
            base_url=args.base_url.rstrip("/"),
            model_id=args.model_id,
            rerank_model_id=args.rerank_model_id,
            embedding_model_id=args.embedding_model_id,
            knowledge_dir=args.knowledge_dir,
            state_file=Path(args.state_file).expanduser(),
            steel_origin=args.steel_origin,
            embed_origin=args.embed_origin,
            ops_mcp_url=args.ops_mcp_url,
            webhook_url=args.webhook_url,
            webhook_secret=webhook_secret,
            dry_run=args.dry_run,
        ),
        workspace_key,
        ops_key or "",
    )


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)
    if args.dry_run:
        config = RCAConfig(
            base_url=args.base_url.rstrip("/"),
            model_id=args.model_id,
            rerank_model_id=args.rerank_model_id,
            embedding_model_id=args.embedding_model_id,
            knowledge_dir=args.knowledge_dir,
            state_file=Path(args.state_file).expanduser(),
            steel_origin=args.steel_origin,
            embed_origin=args.embed_origin,
            ops_mcp_url=args.ops_mcp_url,
            webhook_url=args.webhook_url,
            webhook_secret="",
            dry_run=True,
        )
        state = BootstrapState.load(config.state_file)
        print(json.dumps(dry_run_summary(config, state), ensure_ascii=False, indent=2))
        return 0

    config, workspace_key, ops_key = build_bootstrap_config(args)
    runner = RCABootstrapper(config, workspace_key, ops_key)
    summary = runner.run()
    runner.state.save(config.state_file)
    summary["state_file"] = str(config.state_file)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
