// Package model — schema 归属（微服务方案 §2.4 阶段 A：单库 7 schema，Phase 2/3 拆库的落位依据）。
//
// 铁律：表只属于一个 schema；跨 schema 的 JOIN/FK/共事务禁止
// （internal/data/schema_test.go 静态检查；存量跨域 Raw SQL 随 Phase 2/3 按
// docs/04-delivery/microservices-execution-plan.md 附录 A/B 销号改走 API/事件）。
package model

// 域 schema 名（与部署单元一一对应；ops 为 Phase 1 ops-admin 预留）。
const (
	SchemaPlatform    = "platform"    // identity：租户/用户/RBAC/审计/单号/附件
	SchemaMaster      = "master"      // master-data：物料/BOM/工艺/工作中心/客商/仓区
	SchemaErp         = "erp"         // core-erp：销售/采购/请购
	SchemaMes         = "mes"         // core-mes：工单/报工/质检/工序
	SchemaWms         = "wms"         // core-wms：库存/流水/盘点/预占/序列号
	SchemaIntegration = "integration" // integration：webhook/设备遥测
	SchemaOps         = "ops"         // ops-admin：服务注册/运维审计（Phase 1 启用）
)

// tableSchema 全部业务表的 schema 归属（键 = GORM 默认表名）。
// 变更流程：先改本映射 → schema_test 会强制迁移 0002 覆盖与 Raw SQL 限定同步更新。
var tableSchema = map[string]string{
	// platform
	"attachments": SchemaPlatform, "audit_logs": SchemaPlatform, "doc_number_rules": SchemaPlatform,
	"factories": SchemaPlatform, "idempotency_keys": SchemaPlatform, "menus": SchemaPlatform,
	"roles": SchemaPlatform, "sys_params": SchemaPlatform, "tenants": SchemaPlatform,
	"user_factories": SchemaPlatform, "user_roles": SchemaPlatform, "users": SchemaPlatform,
	"outbox_messages": SchemaPlatform, "consumed_messages": SchemaPlatform,
	// master
	"bom_lines": SchemaMaster, "boms": SchemaMaster, "customers": SchemaMaster,
	"locations": SchemaMaster, "materials": SchemaMaster, "routing_ops": SchemaMaster,
	"routings": SchemaMaster, "suppliers": SchemaMaster, "warehouses": SchemaMaster,
	"work_centers": SchemaMaster,
	// erp
	"purchase_order_lines": SchemaErp, "purchase_orders": SchemaErp,
	"purchase_request_lines": SchemaErp, "purchase_requests": SchemaErp,
	"sales_order_lines": SchemaErp, "sales_orders": SchemaErp,
	// mes
	"defect_codes": SchemaMes, "production_reports": SchemaMes, "qc_records": SchemaMes,
	"qc_schemes": SchemaMes, "wo_operations": SchemaMes, "work_orders": SchemaMes,
	// wms
	"serial_numbers": SchemaWms, "stock_movements": SchemaWms, "stock_order_lines": SchemaWms,
	"stock_orders": SchemaWms, "stock_reservations": SchemaWms, "stocks": SchemaWms,
	"stocktaking_lines": SchemaWms, "stocktakings": SchemaWms,
	// integration
	"device_telemetries": SchemaIntegration, "integration_pull_cursors": SchemaIntegration, "webhook_deliveries": SchemaIntegration,
	"webhook_endpoints": SchemaIntegration,
	// ops（Phase 1.5 ops-admin；Lite 部署同样建表，空置无副作用）
	"ops_audit_logs": SchemaOps, "ops_alerts": SchemaOps,
}

// SchemaOf 表所属 schema；未登记的表（如 schema_migrations 元表）返回空串。
func SchemaOf(table string) string { return tableSchema[table] }

// TableSchemaForTest 暴露映射副本（仅供 data/schema_test.go 静态检查用）。
func TableSchemaForTest() map[string]string {
	out := make(map[string]string, len(tableSchema))
	for k, v := range tableSchema {
		out[k] = v
	}
	return out
}

// Schemas 全部 schema（建库/迁移用，含尚未启用的 ops）。
func Schemas() []string {
	return []string{SchemaPlatform, SchemaMaster, SchemaErp, SchemaMes, SchemaWms, SchemaIntegration, SchemaOps}
}

// QualifiedTable 业务表限定名（schema.table）；未登记表原样返回。
func QualifiedTable(table string) string {
	if s := tableSchema[table]; s != "" {
		return s + "." + table
	}
	return table
}
