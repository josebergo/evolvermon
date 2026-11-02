// Package data：GORM 初始化、租户作用域、单号服务、审计助手。
package data

import (
	"context"
	"gorm.io/gorm/schema"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
)

func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Warn),
		NamingStrategy: domainNaming{base: schema.NamingStrategy{}}, // Phase 0.2：表按域入 schema
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(60)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return db, nil
}

// MigrateProfile 按服务迁移的库内表集（Phase 2 分库）：all=Lite 单库全量（=Migrate）；
// identity/core/integration/ops = 各自独立 DB 应建的表（core 库的 platform schema 仅保留事件底座表）。
func MigrateProfile(db *gorm.DB, profile string) error {
	switch profile {
	case "identity":
		return db.AutoMigrate(
			&model.Tenant{}, &model.Factory{}, &model.User{}, &model.UserFactory{},
			&model.Role{}, &model.UserRole{}, &model.Menu{}, &model.AuditLog{},
			&model.SysParam{}, &model.DocNumberRule{}, &model.IdempotencyKey{}, &model.Attachment{},
		)
	case "erp":
		return db.AutoMigrate(
			&model.SalesOrder{}, &model.SalesOrderLine{}, &model.PurchaseRequest{}, &model.PurchaseRequestLine{},
			&model.PurchaseOrder{}, &model.PurchaseOrderLine{},
			&model.OutboxMessage{}, &model.ConsumedMessage{},
		)
	case "mes":
		return db.AutoMigrate(
			&model.WorkOrder{}, &model.WoOperation{}, &model.ProductionReport{},
			&model.DefectCode{}, &model.QcScheme{}, &model.QcRecord{},
			&model.OutboxMessage{}, &model.ConsumedMessage{},
		)
	case "wms":
		return db.AutoMigrate(
			&model.Stock{}, &model.StockMovement{}, &model.StockOrder{}, &model.StockOrderLine{},
			&model.Stocktaking{}, &model.StocktakingLine{}, &model.StockReservation{}, &model.SerialNumber{},
			&model.OutboxMessage{}, &model.ConsumedMessage{},
		)
	case "core": // core 共享库（mono 形态）：erp+mes+wms 全量
		return db.AutoMigrate(
			&model.SalesOrder{}, &model.SalesOrderLine{}, &model.PurchaseRequest{}, &model.PurchaseRequestLine{},
			&model.PurchaseOrder{}, &model.PurchaseOrderLine{}, &model.WorkOrder{}, &model.WoOperation{},
			&model.ProductionReport{}, &model.DefectCode{}, &model.QcScheme{}, &model.QcRecord{},
			&model.Stock{}, &model.StockMovement{}, &model.StockOrder{}, &model.StockOrderLine{},
			&model.Stocktaking{}, &model.StocktakingLine{}, &model.StockReservation{}, &model.SerialNumber{},
			// 事件底座（core 库的 platform schema 仅此两表）
			&model.OutboxMessage{}, &model.ConsumedMessage{},
		)
	case "integration":
		return db.AutoMigrate(
			&model.WebhookEndpoint{}, &model.WebhookDelivery{}, &model.DeviceTelemetry{},
			&model.IntegrationPullCursor{},
			&model.ConsumedMessage{}, // 扇出幂等（拉取方去重）
		)
	case "ops":
		return db.AutoMigrate(&model.OpsAuditLog{}, &model.OpsAlert{})
	default: // all：Lite 单库全量
		return Migrate(db)
	}
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		// 平台
		&model.Tenant{}, &model.Factory{}, &model.Warehouse{}, &model.Location{},
		&model.User{}, &model.UserFactory{}, &model.Role{}, &model.UserRole{},
		&model.Menu{}, &model.AuditLog{}, &model.SysParam{}, &model.DocNumberRule{},
		&model.IdempotencyKey{},
		// 主数据
		&model.Material{}, &model.Bom{}, &model.BomLine{},
		&model.Routing{}, &model.RoutingOp{}, &model.WorkCenter{},
		&model.Customer{}, &model.Supplier{},
		// ERP
		&model.SalesOrder{}, &model.SalesOrderLine{},
		&model.PurchaseRequest{}, &model.PurchaseRequestLine{},
		&model.PurchaseOrder{}, &model.PurchaseOrderLine{},
		// MES
		&model.WorkOrder{}, &model.WoOperation{}, &model.ProductionReport{},
		&model.DefectCode{}, &model.QcScheme{}, &model.QcRecord{},
		// WMS
		&model.Stock{}, &model.StockMovement{}, &model.StockOrder{}, &model.StockOrderLine{},
		&model.Stocktaking{}, &model.StocktakingLine{}, &model.StockReservation{},
		// M2+
		&model.SerialNumber{},
		// M6
		&model.WebhookEndpoint{}, &model.WebhookDelivery{}, &model.DeviceTelemetry{},
		// 补齐
		&model.Attachment{},
		// ops（Phase 1.5 ops-admin）
		&model.OpsAuditLog{}, &model.OpsAlert{},
		// 事件一致性底座（Phase 2.2）
		&model.OutboxMessage{}, &model.ConsumedMessage{},
	)
}

// TenantScope 所有业务查询强制注入租户/工厂过滤（架构 §4，越权测试门禁的对象）。
// 参数 factoryScoped=true 时按当前工厂过滤（需挂厂的业务表）；主数据（物料等）仅租户级。
func TenantScope(ac *middleware.AuthCtx, factoryScoped bool) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		d := db.Where("tenant_id = ?", ac.TenantID)
		if factoryScoped && ac.FactoryID > 0 {
			d = d.Where("factory_id = ?", ac.FactoryID)
		}
		return d
	}
}

// ScopeFor 泛型便捷：表是否含 factory_id 由调用方声明。
func ScopeFor(ac *middleware.AuthCtx, hasFactory bool) func(db *gorm.DB) *gorm.DB {
	return TenantScope(ac, hasFactory)
}

// ---------------- 单号服务（并发安全，数据库设计 §1 doc_number_rules） ----------------

var docPrefixes = map[string]string{
	"SO": "SO", "PR": "PR", "PO": "PO", "WO": "WO",
	"RCV": "RCV", "SHP": "SHP", "MV": "MV", "STK": "STK", "QC": "QC",
}

// NextDocNo 原子递增序列并格式化：前缀-日期-流水。
// RemoteParamPut 远程写系统参数钩子（分库形态 wire 注入；sys_params 归 identity 库）。
var RemoteParamPut func(tenantID, factoryID int64, key, value string)

// RemoteDocNo 远程取号钩子（Phase 2 第二批：EM_IDENTITY_URL 时由 wire 注入；identity 自身不设）。
var RemoteDocNo func(tenantID int64, docType string) (string, error)

func NextDocNo(db *gorm.DB, tenantID int64, docType string) (string, error) {
	if RemoteDocNo != nil {
		return RemoteDocNo(tenantID, docType) // 单号规则归 identity（行锁强一致在服务端）
	}
	prefix, ok := docPrefixes[docType]
	if !ok {
		return "", fmt.Errorf("unknown doc type %s", docType)
	}
	// 规则不存在则初始化（种子亦会预置）
	rule := model.DocNumberRule{TenantID: tenantID, DocType: docType, Prefix: prefix, SeqLen: 4}
	if err := db.Where("tenant_id=? AND doc_type=?", tenantID, docType).FirstOrCreate(&rule).Error; err != nil {
		return "", err
	}
	if err := db.Model(&model.DocNumberRule{}).
		Where("id=?", rule.ID).
		Update("current_seq", gorm.Expr("current_seq+1")).Error; err != nil {
		return "", err
	}
	var seq int64
	if err := db.Raw("SELECT current_seq FROM platform.doc_number_rules WHERE id=?", rule.ID).Scan(&seq).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%0*d", prefix, time.Now().Format("060102"), rule.SeqLen, seq), nil
}

// ---------------- 定时 MRP（FR-ERP-077）：运行并将摘要写入 sys_param ----------------

func RunScheduledMrp(db *gorm.DB, tenantID, factoryID int64) error {
	// 复用 mrp 计数逻辑的最小实现：统计缺口物料数（详细建议仍走 /mrp/run）
	type row struct {
		Cnt int64
	}
	// master 分库拆解：安全库存走通道；库存/在途 core 直查（Go 合并）
	type mrow struct {
		MaterialID int64
		Q          float64
	}
	matMap, merr := client.M().MaterialMap(context.Background(), tenantID)
	if merr != nil {
		return merr
	}
	matIDs := make([]int64, 0, len(matMap))
	for id, mt := range matMap {
		if mt.Status == 1 && mt.SafetyStock > 0 {
			matIDs = append(matIDs, id)
		}
	}
	cnt := 0
	if len(matIDs) > 0 {
		var stocks []mrow
		db.Raw(`SELECT material_id, SUM(qty_on_hand) q FROM wms.stocks WHERE factory_id=? GROUP BY material_id`, factoryID).Scan(&stocks)
		onHand := map[int64]float64{}
		for _, r := range stocks {
			onHand[r.MaterialID] = r.Q
		}
		var transit []mrow
		db.Raw(`SELECT l.material_id, COALESCE(SUM(l.qty-l.received_qty),0) q FROM erp.purchase_order_lines l
			JOIN erp.purchase_orders po ON po.id=l.purchase_order_id
			WHERE po.factory_id=? AND po.status IN ('arriving','receiving') AND l.material_id IN ? GROUP BY l.material_id`, factoryID, matIDs).Scan(&transit)
		inT := map[int64]float64{}
		for _, r := range transit {
			inT[r.MaterialID] = r.Q
		}
		for _, id := range matIDs {
			if onHand[id]+inT[id] < matMap[id].SafetyStock {
				cnt++
			}
		}
	}
	r := row{Cnt: int64(cnt)}
	summary := fmt.Sprintf(`{"shortageMaterials":%d,"ranAt":%q}`, r.Cnt, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO platform.sys_params(tenant_id, factory_id, key, value, updated_at)
		VALUES(?,?, 'mrp_last_run', ?, now())
		ON CONFLICT (tenant_id, factory_id, key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, tenantID, factoryID, summary)
	return nil
}

// ---------------- 审计助手 ----------------

// RemoteAudit 远程审计钩子（尽力而为；wire 注入）。
var RemoteAudit func(payload map[string]any)

func Audit(db *gorm.DB, ac *middleware.AuthCtx, ip, action, objType, objID, objDesc, before, after string) {
	if ac == nil {
		return
	}
	// before/after 存 jsonb：普通字符串按 JSON 字符串编码，空值落库默认 {}
	jstr := func(s string) string {
		if s == "" {
			return ""
		}
		b, err := json.Marshal(s)
		if err != nil {
			return ""
		}
		return string(b)
	}
	if err := db.Create(&model.AuditLog{
		TenantID: ac.TenantID, FactoryID: &ac.FactoryID, UserID: ac.UserID, Username: ac.Username,
		Action: action, ObjectType: objType, ObjectID: objID, ObjectDesc: objDesc,
		Before: jstr(before), After: jstr(after), IP: ip,
	}).Error; err != nil {
		log.Printf("audit write failed: %v", err)
	}
}

// BoolParam 读取租户级布尔参数（jsonb 值兼容 true/false 与 "true"/"false"）。
// RemoteBoolParam 远程参数钩子（分库：sys_params 归 identity 库；wire 注入，60s 进程内缓存）。
var RemoteBoolParam func(tenantID int64, key string) bool

func BoolParam(db *gorm.DB, tenantID int64, key string) bool {
	if RemoteBoolParam != nil {
		return RemoteBoolParam(tenantID, key)
	}
	var p model.SysParam
	if err := db.Where("tenant_id=? AND factory_id IS NULL AND key=?", tenantID, key).First(&p).Error; err != nil {
		return false
	}
	var b bool
	if err := json.Unmarshal([]byte(p.Value), &b); err == nil {
		return b
	}
	var str string
	if err := json.Unmarshal([]byte(p.Value), &str); err == nil {
		return str == "true" || str == "1"
	}
	return false
}

// domainNaming 包装默认命名策略：解析出默认表名后按 model.tableSchema 加 schema 前缀
// （platform.tenants、master.materials…）。模型上显式 TableName() 的（schema_migrations）不走本策略。
type domainNaming struct {
	base schema.NamingStrategy
}

func (n domainNaming) UniqueName(table, column string) string { return n.base.UniqueName(table, column) }

// SchemaName 返回空：schema 前缀已由 TableName 内联（避免 gorm 再拼一层）。
func (n domainNaming) SchemaName(table string) string { return "" }

func (n domainNaming) TableName(name string) string {
	return model.QualifiedTable(n.base.TableName(name))
}

// 其余命名行为（列名/连接表等）保持默认。
func (n domainNaming) ColumnName(table, column string) string { return n.base.ColumnName(table, column) }
func (n domainNaming) JoinTableName(joinTable string) string {
	return model.QualifiedTable(n.base.JoinTableName(joinTable))
}
func (n domainNaming) RelationshipFKName(rel schema.Relationship) string {
	return n.base.RelationshipFKName(rel)
}
func (n domainNaming) CheckerName(table, column string) string { return n.base.CheckerName(table, column) }
func (n domainNaming) IndexName(table, column string) string   { return n.base.IndexName(table, column) }

// EnsureSchemas 幂等建 schema（迁移 baseline 与升级 0002 前调用）。

func EnsureSchemas(db *gorm.DB) error {
	for _, sc := range model.Schemas() {
		if err := db.Exec("CREATE SCHEMA IF NOT EXISTS " + sc).Error; err != nil {
			return fmt.Errorf("create schema %s: %w", sc, err)
		}
	}
	return nil
}
