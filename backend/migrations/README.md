# 版本化迁移（NFR-021）

- 首次部署：启动时自动执行基线建表（AutoMigrate），记为 `schema_migrations.version = 1`。
- 增量变更：新建 `NNNN_说明.up.sql`（NNNN 从 0002 递增），启动时按序执行并记录版本。
  - 文件内容为普通 DDL/DML，以分号分隔；**不要**包含函数体等内部分号的复合语句。
- 回滚：为对应变更提供 `NNNN_说明.down.sql` 并人工执行，随后将 `schema_migrations` 版本回拨。
  - 发布约束：任何迁移须与上一版本向前兼容（加列可空、新枚举先加后用），保证滚动升级不掉单。
- 仅执行迁移不启动服务：`EM_MIGRATE_ONLY=1 ./evolvermon`。
