# 数据库设计（PostgreSQL 16）

> 命名规范：蛇形复数表名；主键 `id BIGINT GENERATED ALWAYS AS IDENTITY`；通用审计列嵌入每表：
> `tenant_id BIGINT NOT NULL, factory_id BIGINT NULL, created_by BIGINT, created_at TIMESTAMPTZ, updated_by BIGINT, updated_at TIMESTAMPTZ, version INT DEFAULT 1`（version 用于乐观锁，仅业务单据表）。
> 多工厂归属表才含 factory_id。

## 1. 平台域

### tenants 租户
`id, code UNIQUE, name, contact_name, contact_phone, status(1启用/0停用), expired_at, config jsonb, …审计列(无tenant_id)`

### factories 工厂
`id, tenant_id, code, name, address, manager_user_id, status` — UNIQUE(tenant_id, code)

### warehouses 仓库
`id, tenant_id, factory_id, code, name, type enum(原料/成品/半成品/不良/在途), status` — UNIQUE(factory_id, code)

### locations 货位
`id, tenant_id, warehouse_id, code, zone, rack, level_no, bin, frozen BOOL` — UNIQUE(warehouse_id, code)

### users 用户
`id, tenant_id, username, password_hash, real_name, mobile, email, status, last_login_at, locked_until` — UNIQUE(tenant_id, username)

### user_factories 用户-工厂
`user_id, factory_id, is_default` — PK(user_id, factory_id)

### roles / role_apis / user_roles
`roles: id, tenant_id, code, name, data_scope enum(all/factory/self), built_in BOOL`
`role_apis: role_id, perm_code` — perm_code 如 `wms:stock:export`
`user_roles: user_id, role_id`

### menus 菜单
`id, parent_id, name, path, icon, sort, type enum(目录/页面/按钮), perm_code NULL, visible` — 超管初始化全量，按角色过滤返回

### api_permissions 权限点字典
`code PK, module, resource, action, description` — 代码启动时同步（单一定义源）

### audit_logs 审计日志（仅追加）
`id BIGIDENTITY, tenant_id, factory_id, user_id, username, action, object_type, object_id, object_desc, before jsonb, after jsonb, ip, created_at` — **REVOKE UPDATE,DELETE**

### sys_params 系统参数
`id, tenant_id NULL(全局), factory_id NULL, key, value jsonb, remark` — UNIQUE(tenant_id, factory_id, key)

### doc_number_rules 单号规则
`id, tenant_id, doc_type, prefix, has_factory_code BOOL, date_format, seq_len, current_seq` — UNIQUE(tenant_id, doc_type)
> 并发安全：`UPDATE … SET current_seq=current_seq+1 RETURNING current_seq`

### idempotency_keys 幂等键
`key PK, tenant_id, user_id, request_hash, response_status, response_body, created_at` — 24h 清理 job

## 2. ERP 主数据域

### materials 物料
`id, tenant_id, factory_id NULL(集团共享物料), code, name, spec, category enum(原料/半成品/成品/辅料/包材), unit, source_type enum(自制/外购/委外), batch_managed BOOL, safety_stock NUMERIC(18,4), default_warehouse_id, barcode, price NUMERIC(18,6), status, extra jsonb` — UNIQUE(tenant_id, code)

### boms / bom_lines
`boms: id, tenant_id, material_id, version_no, status enum(草稿/启用/停用), effective_from, remark, …version`
`bom_lines: id, bom_id, line_no, material_id, qty NUMERIC(18,6), scrap_rate NUMERIC(5,2), remark` — UNIQUE(bom_id, line_no)
约束：同 material_id 同时仅一条 status='启用'（部分唯一索引）

### routings 工艺路线 / routing_ops 工序
`routings: id, tenant_id, material_id, bom_id NULL, version_no, status, remark`
`routing_ops: id, routing_id, seq INT, name, workcenter_id, prep_time_s INT, run_time_per_unit_s NUMERIC(10,3), is_outsource BOOL, piece_rate NUMERIC(12,2)`

### work_centers 工作中心
`id, tenant_id, factory_id, code, name, type enum(人工/设备/产线), capacity_per_day NUMERIC, cost_rate NUMERIC(12,2), status`

### customers / customer_addresses / suppliers
`customers: id, tenant_id, code, name, contact, phone, tax_no, credit_limit NUMERIC, status, extra jsonb`
`customer_addresses: id, customer_id, receiver, phone, province, city, detail, is_default`
`suppliers: id, tenant_id, code, name, contact, phone, lead_time_days INT, payment_term, status`

## 3. ERP 交易域

### sales_orders / sales_order_lines
`sales_orders: id, tenant_id, factory_id, doc_no UNIQUE, customer_id, order_date, delivery_date, currency DEFAULT CNY, tax_rate NUMERIC(5,2), amount NUMERIC(18,2), status enum(草稿/已审核/部分发货/已完成/已关闭/作废), audited_by, audited_at, remark, …version`
`sales_order_lines: id, order_id, line_no, material_id, qty, price, tax_rate, amount, delivery_date, shipped_qty DEFAULT 0`

### purchase_requests / purchase_request_lines（请购）
同 sales 结构，status enum(草稿/已审核/已转采购/已关闭/作废)，行含 `purchased_qty`

### purchase_orders / purchase_order_lines
`purchase_orders: id, …, supplier_id, doc_no, status enum(草稿/已审核/到货中/部分入库/已完成/已关闭/作废)`
`purchase_order_lines: …, qty, price, received_qty DEFAULT 0, delivery_date`

## 4. MES 域

### work_orders 工单
`id, tenant_id, factory_id, doc_no UNIQUE, material_id, qty, bom_id, bom_snapshot jsonb, routing_id, routing_snapshot jsonb, source_type enum(手工/MRP/销售直发), source_doc, priority INT, plan_start_at, plan_end_at, actual_start_at, actual_end_at, status enum(草稿/已下达/执行中/暂停/已完工/已入库/已关闭/作废), reported_qty DEFAULT 0, good_qty DEFAULT 0, defect_qty DEFAULT 0, remark, …version`

### wo_operations 工单工序
`id, work_order_id, seq INT, name, workcenter_id, plan_hours NUMERIC(10,2), status enum(待派工/已派工/执行中/已完成/跳过), assignee_group_id NULL, assignee_user_id NULL, started_at, ended_at, qty_ok DEFAULT 0, qty_defect DEFAULT 0`

### production_reports 报工记录（高并发写热点）
`id BIGIDENTITY, tenant_id, factory_id, work_order_id, wo_operation_id, user_id, qty_ok INT, qty_defect INT, defect_codes text[], hours NUMERIC(8,2), client_token UNIQUE(幂等), reported_at, remark` — 索引(work_order_id, reported_at), (user_id, reported_at)

### defect_codes 不良代码字典
`id, tenant_id, code, name, category, sort, status` — UNIQUE(tenant_id, code)

### qc_schemes 检验方案 / qc_records 质检单
`qc_schemes: id, tenant_id, material_id, check_type enum(IQC/IPQC/FQC), sample_ratio NUMERIC(5,4), items jsonb[{name,std,method}]`
`qc_records: id, tenant_id, factory_id, doc_no, source_type enum(到货/工序/完工/手工), source_doc_type, source_doc_id, work_order_id NULL, material_id, batch_no NULL, inspector_id, result enum(待检/合格/不合格/让步), items_result jsonb, disposal enum(返工/报废/让步) NULL, created_at, audited_at`

## 5. WMS 域

### stocks 库存台账（唯一维度行）
`id, tenant_id, factory_id, warehouse_id, location_id NULL, material_id, batch_no NULL, qty_on_hand NUMERIC(18,4) DEFAULT 0, qty_reserved NUMERIC(18,4) DEFAULT 0, qty_frozen NUMERIC(18,4) DEFAULT 0, qty_defective NUMERIC(18,4) DEFAULT 0, updated_at`
— **UNIQUE(factory_id, warehouse_id, COALESCE(location_id,0), material_id, COALESCE(batch_no,''))**（表达式唯一索引）
— 索引(material_id)

### stock_movements 库存流水（仅追加）
`id BIGIDENTITY, tenant_id, factory_id, warehouse_id, location_id, material_id, batch_no, direction enum(IN/OUT), qty NUMERIC(18,4), qty_type enum(可用/预占/冻结/不良/待检), biz_type enum(采购入库/生产入库/销售出库/领料出库/退货/移库/盘盈/盘亏/报废/调拨在途), source_doc_type, source_doc_id, source_doc_no, before_qty, after_qty, op_user_id, created_at` — 索引(material_id, created_at), (source_doc_id)

### stock_orders 出入库单（单表双类型）
`id, tenant_id, factory_id, doc_type enum(RCV入库/SHP出库/MV移库/STK_ADJ盘点调整), doc_no UNIQUE, warehouse_id, source_type enum(采购/生产/销售/领料/报废/盘盈/盘亏/移库/手工/调拨), source_doc_type NULL, source_doc_id NULL, to_warehouse_id NULL(移库/调拨), status enum(草稿/执行中/已完成/作废), posted_by, posted_at, remark, …version`

### stock_order_lines
`id, order_id, line_no, material_id, batch_no NULL, qty, done_qty DEFAULT 0, from_location_id NULL, to_location_id NULL, price NUMERIC(18,6) NULL(采购成本), remark`

### stocktakings 盘点单 / stocktaking_lines
`stocktakings: id, tenant_id, factory_id, doc_no, warehouse_id, scope enum(全盘/抽盘), method enum(明盘/盲盘), status enum(草稿/盘点中/差异确认/已调整), snapshot_at, remark, …version`
`stocktaking_lines: id, stocktaking_id, location_id, material_id, batch_no, book_qty(快照), counted_qty NULL, diff_qty GENERATED ALWAYS AS (counted_qty-book_qty) STORED, adjusted BOOL`

### stock_reservations 预占明细
`id, tenant_id, factory_id, material_id, warehouse_id NULL, qty, biz_type enum(销售/工单), source_doc_type, source_doc_id, source_line_id, status enum(占用/已核销/已释放), created_at, released_at` — 核销/释放由出库过账或单据作废驱动

## 6. 索引与约束清单（核心）

| 表 | 约束/索引 | 目的 |
|---|---|---|
| stocks | 表达式唯一索引（维度） | 并发 INSERT 防重 |
| stocks | SELECT FOR UPDATE 行锁路径 | 过账防超卖 |
| stock_movements | (source_doc_id) | 单据穿透流水 |
| production_reports | UNIQUE(client_token) | 报工幂等 |
| boms | 部分唯一索引(material_id) WHERE status='启用' | 单一启用版本 |
| 全业务表 | (tenant_id[, factory_id]) 复合索引开头 | 租户过滤必走 |

## 7. 迁移策略

- `migrations/0001_init.sql` 起，纯 SQL、向前兼容（加列可空、新枚举先加后用）；
- 应用启动校验 schema_migrations 版本，缺失则提示执行 `evolvermon migrate`；
- 种子数据独立（seed 包幂等 upsert），与结构迁移分离。
