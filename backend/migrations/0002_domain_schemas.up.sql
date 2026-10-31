-- v0.8.0 Phase 0.2：单库分域 schema（微服务方案 §2.4 阶段 A）
-- 存量库：public 下的业务表迁入各自域 schema；全新库走 baseline（AutoMigrate 已带 schema，本迁移幂等空转）

CREATE SCHEMA IF NOT EXISTS platform;
CREATE SCHEMA IF NOT EXISTS master;
CREATE SCHEMA IF NOT EXISTS erp;
CREATE SCHEMA IF NOT EXISTS mes;
CREATE SCHEMA IF NOT EXISTS wms;
CREATE SCHEMA IF NOT EXISTS integration;
CREATE SCHEMA IF NOT EXISTS ops;

-- platform
ALTER TABLE IF EXISTS public.attachments SET SCHEMA platform;
ALTER TABLE IF EXISTS public.audit_logs SET SCHEMA platform;
ALTER TABLE IF EXISTS public.doc_number_rules SET SCHEMA platform;
ALTER TABLE IF EXISTS public.factories SET SCHEMA platform;
ALTER TABLE IF EXISTS public.idempotency_keys SET SCHEMA platform;
ALTER TABLE IF EXISTS public.menus SET SCHEMA platform;
ALTER TABLE IF EXISTS public.roles SET SCHEMA platform;
ALTER TABLE IF EXISTS public.sys_params SET SCHEMA platform;
ALTER TABLE IF EXISTS public.tenants SET SCHEMA platform;
ALTER TABLE IF EXISTS public.user_factories SET SCHEMA platform;
ALTER TABLE IF EXISTS public.user_roles SET SCHEMA platform;
ALTER TABLE IF EXISTS public.users SET SCHEMA platform;

-- master
ALTER TABLE IF EXISTS public.bom_lines SET SCHEMA master;
ALTER TABLE IF EXISTS public.boms SET SCHEMA master;
ALTER TABLE IF EXISTS public.customers SET SCHEMA master;
ALTER TABLE IF EXISTS public.locations SET SCHEMA master;
ALTER TABLE IF EXISTS public.materials SET SCHEMA master;
ALTER TABLE IF EXISTS public.routing_ops SET SCHEMA master;
ALTER TABLE IF EXISTS public.routings SET SCHEMA master;
ALTER TABLE IF EXISTS public.suppliers SET SCHEMA master;
ALTER TABLE IF EXISTS public.warehouses SET SCHEMA master;
ALTER TABLE IF EXISTS public.work_centers SET SCHEMA master;

-- erp
ALTER TABLE IF EXISTS public.purchase_order_lines SET SCHEMA erp;
ALTER TABLE IF EXISTS public.purchase_orders SET SCHEMA erp;
ALTER TABLE IF EXISTS public.purchase_request_lines SET SCHEMA erp;
ALTER TABLE IF EXISTS public.purchase_requests SET SCHEMA erp;
ALTER TABLE IF EXISTS public.sales_order_lines SET SCHEMA erp;
ALTER TABLE IF EXISTS public.sales_orders SET SCHEMA erp;

-- mes
ALTER TABLE IF EXISTS public.defect_codes SET SCHEMA mes;
ALTER TABLE IF EXISTS public.production_reports SET SCHEMA mes;
ALTER TABLE IF EXISTS public.qc_records SET SCHEMA mes;
ALTER TABLE IF EXISTS public.qc_schemes SET SCHEMA mes;
ALTER TABLE IF EXISTS public.wo_operations SET SCHEMA mes;
ALTER TABLE IF EXISTS public.work_orders SET SCHEMA mes;

-- wms
ALTER TABLE IF EXISTS public.serial_numbers SET SCHEMA wms;
ALTER TABLE IF EXISTS public.stock_movements SET SCHEMA wms;
ALTER TABLE IF EXISTS public.stock_order_lines SET SCHEMA wms;
ALTER TABLE IF EXISTS public.stock_orders SET SCHEMA wms;
ALTER TABLE IF EXISTS public.stock_reservations SET SCHEMA wms;
ALTER TABLE IF EXISTS public.stocks SET SCHEMA wms;
ALTER TABLE IF EXISTS public.stocktaking_lines SET SCHEMA wms;
ALTER TABLE IF EXISTS public.stocktakings SET SCHEMA wms;

-- integration
ALTER TABLE IF EXISTS public.device_telemetries SET SCHEMA integration;
ALTER TABLE IF EXISTS public.webhook_deliveries SET SCHEMA integration;
ALTER TABLE IF EXISTS public.webhook_endpoints SET SCHEMA integration;
-- ops（Phase 1.5 新增；存量库由 ops-admin AutoMigrate 直建于 schema，此处幂等空转）
ALTER TABLE IF EXISTS public.ops_audit_logs SET SCHEMA ops;
ALTER TABLE IF EXISTS public.ops_alerts SET SCHEMA ops;
