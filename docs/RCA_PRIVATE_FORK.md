# RCA 私有 Fork 维护约定

## 决策

长期维护 `Tencent/WeKnora` 的私有 fork，但不把 Steel 业务和运维数据查询迁入 WeKnora。

| 组件 | 唯一职责 |
| --- | --- |
| WeKnora | 知识入库与检索、Skill、ReAct Agent、MCP 编排、Embed 对话与报告制品 |
| Steel | 业务入口、登录态、页面上下文、用户确认、审计展示、知识库管理入口 |
| RCA 工具服务 | 四个只读 Ops MCP 工具、一个报告提交工具、Steel 会话校验、Embed 短期令牌交换；后续可改名 `mcp-app` |
| LLMAPI | OpenAI-compatible 模型入口，不承载业务状态 |

设备型号、告警类型和基础数据变化应由 Skill 过程资源、知识库事实材料及 MCP 适配解决，不为单一设备在 Agent 内写固定分支。

Skill 与知识库是两个独立模块。Skill 保存诊断步骤、停止条件和 SOP 引用，RCA 流程位于 `skills/preloaded/rca-diagnosis/references/`；知识库只保存厂家手册、故障案例等可检索事实材料。知识库分类默认支持 `manufacturer_manual`、`fault_case` 和 `general`，也允许符合命名规则的自定义分类。不要把 Skill/SOP 重复上传到知识库。

## 稳定接口

私有代码只依赖以下边界，不直接查询其他组件的数据库。

1. 模型使用 OpenAI-compatible API。
2. 运维证据只通过 MCP 获取，生产 Agent 只允许：
   - `resolve_alarm`
   - `get_asset_context`
   - `get_topology_context`
   - `query_operational_evidence`
   - `submit_rca_report`（仅提交待人工确认的报告，不执行处置）
3. Steel 使用 Embed SDK，并通过同源 `/back/rca/embed-token` 换取短期 `ems_` 令牌；发布令牌只存服务端 `0600` 文件。
4. 资源通过 `scripts/rca_bootstrap.py` 幂等创建，不通过 SQL 写入 WeKnora 数据库。
5. Agent 只给出诊断、证据、建议与报告，不声称已经执行处置。写操作和外部消息推送必须进入独立工具，并由 Steel 展示确认步骤。

## 保留与裁剪

单工作区部署保留 WeKnora 的租户/RBAC 数据模型，以减少上游合并冲突；关闭自助注册、跨租户访问和联网搜索即可，不删除表、路由或迁移。

首期保留：知识库、Agent、Skill、MCP、Embed、制品预览与下载。联网搜索、IM 渠道、公开注册、组织协作和多租户运营功能默认关闭。只有真实需求和验收用例出现后才启用，不提前二次开发。

Steel 不复制 WeKnora 的整套知识库编辑器。首期提供受权限控制的管理入口，跳转或嵌入 WeKnora 管理页；只有当统一交互被实际证明必要时，才按 API 重写高频操作。

## 用户确认边界

- 读取告警、资产、拓扑和证据不弹确认。
- 诊断结论确认由 Steel 登录用户提交，后端从服务端会话取得操作者，禁止信任前端传入的用户名。
- 停止任务、修订结论和发布报告分别记录操作者与时间。
- 设备配置、工单、封禁、重启、通知推送等外部副作用必须显示目标、参数和影响范围，确认后只执行一次，并保留结果与失败原因。
- Embed 上下文中的 `username`、页面路径等仅用于辅助理解，不作为授权依据。

## 上游升级流程

私有 `main` 只接收通过验收的版本。官方仓库作为 `upstream`，按发布标签合并到临时分支，不在私有 `main` 上直接 rebase。

```bash
git remote add upstream https://github.com/Tencent/WeKnora.git  # 首次执行
git fetch upstream --tags
git switch -c integrate/upstream-<version> main
git merge --no-ff <upstream-tag>
```

合并前备份 PostgreSQL、`.env`、`config/` 和当前 app/UI 镜像标签。合并后至少执行：

```bash
go test ./internal/handler ./internal/handler/session ./internal/router
npm --prefix frontend test -- src/utils/artifactPreview.test.ts src/utils/weknoraWidgetSdk.test.mjs
npm --prefix frontend run type-check
python3 scripts/test_rca_bootstrap.py
```

随后在隔离环境验证数据库迁移、知识入库、检索、Agent 工具白名单、Embed 短期令牌、HTML 沙箱预览和旧会话读取。通过一条真实告警试点后再更新生产镜像标签。

## 回滚与升级红线

- 数据库迁移未验证可恢复时不得升级生产。
- MCP 工具数量、名称或读写属性变化时不得直接升级。
- Embed 发布令牌出现在 HTML、配置 JS、日志或浏览器存储时立即回滚。
- Steel 用户身份无法由服务端会话确认时，诊断确认接口必须 fail-closed。
- 回滚先恢复 app/UI 镜像；若迁移已改变 schema，再按该版本迁移说明恢复数据库备份。

## 当前私有差异入口

- RCA Skill：`skills/preloaded/rca-diagnosis/`
- RCA 资源初始化：`scripts/rca_bootstrap.py`
- Embed 制品接口：`internal/handler/embed_channel.go`
- HTML 沙箱预览：`frontend/src/views/chat/components/ChatArtifactsDrawer.vue`
- 安全 Widget：`frontend/public/weknora-widget.js`

新增私有差异应优先落在这些窄入口；不得复制 Agent 循环、知识检索或会话体系形成第二套实现。
