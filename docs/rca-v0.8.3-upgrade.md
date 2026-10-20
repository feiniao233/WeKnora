# RCA 私有分支升级到 v0.8.3

合并基线为上游 `v0.8.3`（`3e22822c`）。当前私有库 PostgreSQL `111`、SQLite `31`
与上游模型目录迁移重号；RCA 迁移分别移到 `119`、`37`。

新数据库使用正常启动迁移。现有 v0.8.2 私有库使用
`scripts/upgrade_rca_083.py` 离线桥接：校验 RCA 字段和索引，执行上游
PostgreSQL `111–118` / SQLite `31–36`，检查目标结构，最后更新版本。
已有 RCA 字段不再重复添加。不要仅修改版本号，也不要执行 RCA down 迁移。
脚本只接受 clean 的源版本或已完成的目标版本，其他状态拒绝处理。

## 升级准备

1. 将备份恢复到隔离环境，验证下列流程及旧会话、报告和附件读取。
2. PostgreSQL 需配套 `paradedb/paradedb:v0.25.11-pg17`。先在旧镜像上恢复验证库，
   停止验证容器，再用新镜像启动同一验证数据卷，运行桥接，验证扩展和数据。
   这会覆盖真实的 `pg_search` SQL catalog 升级过程。
3. 执行实际部署时停止 app，备份 PostgreSQL、`.env`、`config/` 与原镜像标签。
   备份目录权限 `0700`，文件 `0600`。新镜像启动后立即执行桥接，再启动新 app；
   桥接前不要调用旧 catalog 的 `paradedb.version_info()` 等扩展函数。

```bash
python3 scripts/upgrade_rca_083.py --postgres-container WeKnora-postgres
# 验证库可用 --database 指定数据库。

# SQLite：先停止所有访问该文件的 app 进程。
python3 scripts/upgrade_rca_083.py --sqlite /path/to/weknora.db
```

桥接在一个事务内执行，包括扩展、表、数据回填和迁移标记。上游 `117` 的
`CREATE INDEX CONCURRENTLY` 在离线桥接中改为普通建索引，以支持事务回滚；
上游迁移文件本身保持不变。锁等待上限 10 秒，整个 PostgreSQL 桥接语句上限 15 分钟。
大型数据库应在副本上测量执行时间和磁盘占用。

升级后检查：

- PostgreSQL 为 `119,false`；本地 ParadeDB 的 `pg_search` 为 `0.25.11`、`vector` 为 `0.8.4`。
- SQLite 为 `37,false`。
- 模型目录、图片索引与附件索引存在，RCA 分类、报告和会话数据完整。
- 空 `allowed_tools` 的旧智能推理配置按上游规则显式写入默认工具；每轮禁用工具仍生效。

重复执行会验证目标结构。失败时事务回滚，不要强制修改迁移号继续启动。
需要回滚已完成的实际升级时，恢复停机备份和配套旧镜像；不要直接把新版数据卷交给旧镜像。

## 合并后的行为

- Agent 使用上游附件清单和 `read_file attachment://` 按需读取，历史附件可按会话权限再次读取。
- 保留原图直传、五张图片上限、模型能力复核；显式缺失或失败的附件不能静默丢弃。
  仍在解析的普通文档由读取工具等待，独立图片在开始本轮分析前等待并校验。
- 保留结构化追问、回答关联、会话执行状态、RCA 结果与来源记录、禁用工具约束和 MCP 追踪元数据。
- 删除被上游替代的旧解析副本、历史图片一律要求重传的提示和独立 EOF 判断。
- v0.8.2 桥接脚本及其说明已被本流程替代，不继续维护 `108/27` 的历史入口。

## Steel 与 mcp-app 配套适配

- Steel 通过 mcp-app 的 `GET /rca/ai/agents/tools` 获取上游工具目录，删除固定的旧工具名称。
  `allowed_tools: null` 跟随默认工具，`[]` 明确不选工具；目录加载失败时禁止覆盖配置，可重试。
- 智能推理允许发送已确认上传、仍在解析的附件；快速问答仍等待就绪。
  草稿首发、历史重试和更换模型后的附件复制遵循同一规则。
- mcp-app 依据上游会话内授权读取附件，不单凭 `expires_at` 拒绝仍被历史消息引用的文件。
  分叉会话可读取和预览获授权的原附件，不能通过分叉会话删除原附件。
- WeKnora 的跨用户会话管理员读取只对 Web 管理员开放。
  全权限 API Key 的旧角色上下文即使为 Owner，也不能让一个 Steel 外部用户读取另一个用户的会话。
  配套部署必须包含此权限修复，并验证两个 Steel 用户的隔离。

## 可重复验证

```bash
python3 scripts/test_upgrade_rca_083.py
python3 scripts/test_rca_bootstrap.py
CGO_ENABLED=1 go test -p 2 ./... -timeout 5m
CGO_ENABLED=1 go build -o /tmp/weknora-v083-server ./cmd/server
```

SQLite 用真实历史迁移构建 `31` 数据库，验证数据与图片回填、工具默认值、重复运行、
错误状态、残缺结构及 SQL 失败回滚。PostgreSQL 验证应恢复真实备份，比较升级前后
原始字段的数据摘要，并单独核对上游明确的数据变换（工具默认值、OAuth UTC 时间戳）。
验证日志和数据库备份保存在仓库外，不提交真实业务数据和凭据。

2026-10-20 合并验证：SQLite 桥接 7 项、bootstrap 6 项通过；真实 PostgreSQL
副本升级核对 72 张原有业务表，拒绝旧镜像、dirty 状态、事务失败回滚和重复执行均通过；
全新 PostgreSQL 的迁移用例通过。Go 主模块完整检查中的两个失败包修复后完整复测通过，
服务端构建通过。隔离 HTTP/SSE 流程验证覆盖对话、追问回答、禁用工具、附件按需读取、
跨会话附件拒绝、五张原图、MCP 报告及追踪元数据、重启后历史恢复。
模型和 MCP 使用本地确定性测试服务，未验证真实模型输出质量或 Steel 浏览器页面。
可重复执行的脚本、命令和结果保存在部署机器的
`/root/backups/weknora/v0.8.3-merge-20261020/verification/`；本次没有部署。
