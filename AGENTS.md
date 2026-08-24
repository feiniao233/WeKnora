# AGENTS.md

## 智能助手跨仓库开发

涉及 Steel、mcp-app、WeKnora 或 rca-app 的任务，开始实施前必须完整阅读
`../../work/steel-platform-v-3/AGENTS.md`，并遵守其中“智能助手跨仓库开发规范”。

- WeKnora 负责知识库、文档处理、检索和 Agent 能力；修改前先确认现有接口，禁止重复建模。
- 知识库目录只影响管理和筛选，不改变物理文件、向量索引或默认跨目录检索。
- 不将租户、联网、前端等当前未使用能力扩散到 Steel 或 mcp-app。
- 每项功能独立 commit 并 push，默认不创建 MR/PR，不处理无关工作区文件。
- Git commit 信息使用中文描述。
- Go 变更至少运行相关测试；安全边界变更运行更完整的测试与构建。
