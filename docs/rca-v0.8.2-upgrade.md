# RCA 私有分支升级到 v0.8.2

私有分支原 `000108_rca_schema`（PostgreSQL）和 `000027_rca_schema`（SQLite）
与上游 v0.8.2 新增迁移重号。合并后 RCA 迁移已改为 `000111` / `000031`。
新数据库正常运行标准迁移；使用旧编号的数据库必须在启动新版本前执行一次桥接。

直接启动新版本会让 PostgreSQL 跳过 `sandbox_config_tenant_id`，SQLite 还会重复添加
RCA 字段。不要仅手工修改版本号，也不要对现有 RCA 数据执行 down 迁移。

## 操作

1. 先将数据库备份恢复到隔离数据库，验证以下操作。备份包含用户数据和配置，目录使用
   `0700`、文件使用 `0600`，不得提交。
2. 停止 app，备份数据库、`.env`、`config/` 和当前不可变镜像 tag。
3. 使用目标提交中的工具执行桥接。

```bash
# PostgreSQL：凭据从现有容器内部读取；默认使用 POSTGRES_DB。
python3 scripts/upgrade_legacy_rca_082.py --postgres-container WeKnora-postgres

# 验证库可显式选择数据库名。
python3 scripts/upgrade_legacy_rca_082.py --postgres-container WeKnora-postgres --database <验证库名>

# SQLite：停止所有访问该文件的 app 进程后执行。
python3 scripts/upgrade_legacy_rca_082.py --sqlite /path/to/weknora.db
```

工具仅接受 clean 的旧 RCA `108` / `27` 或已完成升级的 `111` / `31`。
它检查 RCA 字段和索引，在一个事务中补齐上游迁移，验证目标字段后更新版本；
dirty、错误编号、缺少 RCA 字段或 SQL 执行失败都会拒绝或回滚。
重复执行会检查目标结构。PostgreSQL 同时按上游 SQL 回填共享沙箱的所属空间。

4. 检查 `schema_migrations` 为 PostgreSQL `111,false` 或 SQLite `31,false`，
   再按 Steel 仓库的部署说明切换 app 镜像、reload 内部 Nginx，并完成接口和登录页面回归。
5. 如需回滚，停止 app，恢复本次停机备份及原镜像 tag，然后重新检查健康状态。

## 验证

```bash
python3 scripts/test_upgrade_legacy_rca_082.py
```

SQLite 测试使用完整历史基础迁移和旧 RCA 表结构，覆盖数据保留、重复执行、
dirty/错误编号、残缺 RCA 结构、事务中途失败和不存在的数据库文件。
PostgreSQL 发布前使用真实数据库备份恢复副本，逐表比较升级前后的原始字段数据摘要，
检查共享沙箱回填，并验证重复执行、dirty 拒绝和注入失败后的事务回滚。
