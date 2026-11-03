# RCA 私有分支升级到 v0.9.0

## 本阶段范围

上游固定为 `v0.9.0`（`f17ee8a8da901bd658a4cb25f40b5fd41adbfdd7`），
私有起点为 `9189e087ceff15130d785d1771ee7caeeba21146`，不混入发布后的 main。

2026-11-03 用户要求先升级 WeKnora，Steel 当前处于其他开发分支，暂不能修改。
因此本阶段只合并 WeKnora、验证服务端并记录交接事项；Steel、mcp-app 和运行环境不变。
现有结构化追问继续使用 `user_input_enabled`、`request_user_input`、`user_input_answer`。
这项阶段安排来自本次用户要求；配套切换时删除被上游替代的实现，不长期维护两套业务协议。

本页是本次合并的技术交接记录，不另立智能诊断路线图或排期。
后续开始跨仓开发时，将下表的未完成事项纳入
[Steel 当前建设计划](../../../work/steel-platform-v-3/docs/intelligent-diagnosis-current-plan.md)，
本次不修改该计划所在工作区。

## 合并行为

- 保留资产/告警引用、指定技能加载、会话绑定与执行配置来源、分类检索、
  原图直传、工具禁用和 MCP 追踪元数据。
- 保留现有追问及报告终态契约。请求启用现有追问时只提供 `request_user_input`，
  不同时提供上游 `ask_user`；未启用该契约的客户端使用上游机制。
- 接入上游人工交互暂停与恢复。等待输入的消息保持未完成，关闭 SSE 不宣布诊断成功；
  恢复时沿用消息内保存的工具禁用配置。
- 将上游新增的协作、自动化、后台任务和恢复调用接入现有执行占用接口，
  释放时校验消息和请求标识；不保留旧执行接口别名。
- 合入模型流式处理、上下文恢复、MCP 调用重试、文档处理与检索修复。
  快速问答失败保存已生成内容及安全错误说明，供应商地址和原始错误不返回浏览器。
- 保留 AnyDoc/Rust 构建缓存和下载镜像配置。上游 frontend 随版本合入，
  本阶段没有为 Steel 定制 WeKnora 界面。

## 数据库边界

上游新增 PostgreSQL `119–134`、SQLite `37–51`，与原私有 RCA 迁移重号。
RCA 迁移内容不变，移到 PostgreSQL `135`、SQLite `52`，供新数据库顺序初始化。

**现有私有 v0.8.3 数据库（PostgreSQL `119` / SQLite `37`）不能直接启动本版本。**
旧编号已经代表 RCA 字段，而上游同号代表人工交互表。仅重命名文件或强制修改迁移版本
都会漏执行上游迁移，不能作为升级方法。

本阶段不操作运行数据库，也不新增历史数据兼容桥接。需要保留现有数据时，部署前明确
数据保留范围，在隔离副本验证上游缺失迁移、RCA 字段保留、数据清理、失败回滚和重复执行，
之后再实施离线升级。飞书数据源重同步及历史空图片分块清理也需纳入数据核对。
如果后续选择新建数据库，资源初始化和原数据处置同样需单独确认。

旧 `upgrade_rca_083.py` 只适用于旧版本目标，已从当前代码移除。
历史升级记录和工具可从 `9189e087c` 读取，不能用于本版本数据库。

## 跨仓交接与部署前待办

| 待办 | 涉及位置 | 完成条件 | 当前状态 |
| --- | --- | --- | --- |
| 统一结构化追问 | WeKnora `user_input*`；mcp-app `ai_handler.go` / `rca_result.go`；Steel `actions/user-input/` | 切到上游 `ask_user` / prompt 回答接口；支持回答关联、取消、刷新和重启恢复；删除被替代的私有工具、请求字段与卡片解析 | 延后，现有协议保留 |
| 接入可信外部用户交互 | WeKnora QA 运行模式与 prompt 路由；mcp-app 同源代理 | 在 Steel 服务端会话校验基础上授权交互；API Key 不获得任意用户审批权；跨用户读写仍被拒绝 | 延后，上游 API Key 限制保留 |
| 展示暂停与恢复状态 | Steel `api/aiAssistant.ts`、工作区消息与报告操作；mcp-app 流事件消费 | 识别 `run_suspended`、`run_state=awaiting_input`、`pending_prompt`；等待输入不算执行完成，不开放生成报告 | 延后 |
| 核对权限与报告操作 | Agent `access_level`、MCP 风险策略、报告入口 | 只读证据查询正常；普通诊断不能调用报告提交；显式报告操作遵守用户授权；强制审批与拒绝有可见反馈 | 后端原有约束保留，Steel 联调待做 |
| 验证配置保存 | Steel Agent / 知识库配置；mcp-app 字段代理 | 工具目录、权限、图片流程和模型配置保存不覆盖新字段；现有显式模型优先级与技能范围不变 | 延后 |
| 验证现有私有数据库升级 | PostgreSQL / SQLite 升级工具及隔离数据副本 | 不漏迁移、不丢 RCA 分类和执行结果；失败可恢复；重复执行安全 | 未实施，部署前阻塞项 |
| 完成联合验收与发布 | Steel、mcp-app、WeKnora | 桌面端验证对话、追问、停止、刷新、模型切换、附件、报告及两个用户隔离；记录版本和结果，再单独获得部署授权 | 未实施 |

多智能体、定时自动化、工作助手管理入口和按用户复用沙箱不纳入本阶段产品接入。
普通自定义 Agent 的空 `access_level` 仍按上游解释为 `auto_write`；
不能假定升级会自动让所有写操作请求确认，也不能简单设置全局只读而阻断报告提交。

## 验证记录

2026-11-03 使用 Go 1.27.1 / Linux amd64，模块要求仍为 Go 1.26。

| 检查 | 范围与结果 |
| --- | --- |
| RCA bootstrap | 6 项通过 |
| SQLite | 数据库包检查通过，新库迁移到 `52`，迁移号无重复 |
| PostgreSQL | 隔离 `paradedb/paradedb:v0.25.11-pg17` 新库迁移到 `135`；4 项真实数据库测试通过，覆盖迁移往返、索引、工具默认值、OAuth 时间和飞书重同步 |
| Agent 与会话 | 受影响包检查及主模块复测通过；模型选择保留“显式指定不可用模型则报错”的私有契约，并通过专项复测 |
| Go 主模块 | `go test -p 2 ./... -timeout 5m` 全部通过 |
| 服务端构建 | `CGO_ENABLED=1 go build` 通过 |
| HTTP/SSE 专项 | 10 项通过：API Key 对话持久化、现有追问回答、禁用工具、外部用户隔离及 prompt 路由拒绝、附件按需读取、跨会话附件拒绝、五张原图、MCP 报告及追踪元数据、上游暂停恢复且保留禁用工具、重启后历史读取 |

```bash
python3 scripts/test_rca_bootstrap.py
CGO_ENABLED=1 go test -p 2 ./... -timeout 5m
CGO_ENABLED=1 go build -p 2 -o /tmp/weknora-v090-verification-20261103/server ./cmd/server
python3 /tmp/weknora-v090-verification-20261103/e2e/verify_api.py
```

HTTP/SSE 脚本从 v0.8.3 的隔离验证脚本调整，使用本地确定性模型和 MCP 服务，
监听 `127.0.0.1:18090`、`127.0.0.1:18091`，自动创建合成账号、配置和 SQLite 数据库，
完成后停止服务。运行前需释放两个端口并安装 Python `requests`。
脚本、该版本配置/迁移快照、结果 JSON 和日志留在上述目录；验证数据库已清理，
再次执行会重新初始化。模型及 MCP 不接入生产服务，未验证真实模型质量、Steel 页面、
现有业务数据库升级或发布镜像构建，不能据此标记联合验收或部署完成。

PostgreSQL 用例要求独立可丢弃数据库，测试会执行上下迁移，禁止指向运行数据库：

```bash
docker run --detach --rm --name weknora-v090-pg-verification-20261103 \
  --shm-size=256m -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=weknora_verify \
  -p 127.0.0.1:55490:5432 paradedb/paradedb:v0.25.11-pg17
# 等待 pg_isready 成功后执行。
WEKNORA_MIGRATION_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:55490/weknora_verify?sslmode=disable' \
  CGO_ENABLED=1 go test -p 2 ./internal/database -run TestPostgres -timeout 5m -v
docker rm -f weknora-v090-pg-verification-20261103
```

本次 PostgreSQL 验证容器已删除；运行库、部署容器和 Steel / mcp-app 工作区未修改。
