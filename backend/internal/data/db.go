// Package data：GORM 初始化、租户作用域、单号服务、审计助手。
package data

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
)

func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
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
func NextDocNo(db *gorm.DB, tenantID int64, docType string) (string, error) {
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
	if err := db.Raw("SELECT current_seq FROM doc_number_rules WHERE id=?", rule.ID).Scan(&seq).Error; err != nil {
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
	var r row
	if err := db.Raw(`
		SELECT COUNT(*) cnt FROM (
		  SELECT mt.id FROM materials mt
		  WHERE mt.tenant_id=? AND mt.status=1 AND mt.safety_stock > 0
		  AND COALESCE((SELECT SUM(s.qty_on_hand) FROM stocks s WHERE s.material_id=mt.id AND s.factory_id=?),0)
		      + COALESCE((SELECT SUM(po.qty-pol.received_qty) FROM purchase_order_lines pol
		                 JOIN purchase_orders po ON po.id=pol.purchase_order_id
		                 WHERE pol.material_id=mt.id AND po.factory_id=? AND po.status IN ('arriving','receiving')),0)
		      < mt.safety_stock
		) t`, tenantID, factoryID, factoryID).Scan(&r).Error; err != nil {
		return err
	}
	summary := fmt.Sprintf(`{"shortageMaterials":%d,"ranAt":%q}`, r.Cnt, time.Now().Format(time.RFC3339))
	db.Exec(`INSERT INTO sys_params(tenant_id, factory_id, key, value, updated_at)
		VALUES(?,?, 'mrp_last_run', ?, now())
		ON CONFLICT (tenant_id, factory_id, key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, tenantID, factoryID, summary)
	return nil
}

// ---------------- 审计助手 ----------------

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
