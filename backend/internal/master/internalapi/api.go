// Package internalapi master-data 服务内部 API（Phase 2.3/2.4）：供 core/scheduler
// 经网内调用读取主数据，/internal/* 不对外路由；X-Internal-Token 服务间认证。
// BOM 展开为 MRP 试点（执行方案 2.4：scheduler 的 MRP 改走 master-data API）。
package internalapi

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

// BomEdge BOM 边（MRP 净需求展开输入：parent→child + 单耗 + 损耗率）。
type BomEdge struct {
	ParentID int64   `json:"parentId"`
	ChildID  int64   `json:"childId"`
	Qty      float64 `json:"qty"`
	Scrap    float64 `json:"scrap"`
}

// MaterialCols 物料精简视图列（Local/Remote/内部 API 三方共用）。
const MaterialCols = "id, tenant_id, code, name, category, spec, unit, source_type, price, safety_stock, serial_managed, status"

// MaterialLite 物料精简视图（跨域读通道 DTO；覆盖 core 引用读所需字段）。
type MaterialLite struct {
	ID            int64   `json:"id"`
	TenantID      int64   `json:"tenantId"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Category      string  `json:"category"`
	Spec          string  `json:"spec"`
	Unit          string  `json:"unit"`
	SourceType    string  `json:"sourceType"`
	Price         float64 `json:"price"`
	SafetyStock   float64 `json:"safetyStock"`
	SerialManaged bool    `json:"serialManaged"`
	Status        int     `json:"status"`
}

// WCLite 工作中心精简视图。
type WCLite struct {
	ID             int64   `json:"id"`
	TenantID       int64   `json:"tenantId"`
	FactoryID      *int64  `json:"factoryId"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	CapacityPerDay float64 `json:"capacityPerDay"`
	CostRate       float64 `json:"costRate"`
	Status         int     `json:"status"`
}

// PartnerLite 客商精简视图（customer/supplier）。
type PartnerLite struct {
	ID       int64  `json:"id"`
	TenantID int64  `json:"tenantId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Status   int    `json:"status"`
}

// WhLite 仓库精简视图。
type WhLite struct {
	ID        int64  `json:"id"`
	TenantID  int64  `json:"tenantId"`
	FactoryID *int64 `json:"factoryId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    int    `json:"status"`
}

// RoutingOpLite 工序精简视图。
type RoutingOpLite struct {
	ID           int64   `json:"id"`
	RoutingID    int64   `json:"routingId"`
	Seq          int     `json:"seq"`
	Name         string  `json:"name"`
	WorkCenterID int64   `json:"workCenterId"`
	StdHours     float64 `json:"stdHours"`
}

// RoutingDTO 工艺路线（含工序）——工单创建快照用。
type RoutingDTO struct {
	ID         int64            `json:"id"`
	TenantID   int64            `json:"tenantId"`
	MaterialID int64            `json:"materialId"`
	VersionNo  string           `json:"versionNo"`
	Status     string           `json:"status"`
	Ops        []RoutingOpLite  `json:"ops"`
}

type API struct{ DB *gorm.DB }

// Register 挂载 /internal/master/*（独立于业务路由，仅服务间调用）。
func (a *API) Register(r *gin.Engine) {
	g := r.Group("/internal/master", internalToken())
	g.GET("/bom-edges", a.BomEdges)
	g.GET("/materials", a.Materials)
	g.GET("/work-centers", a.WorkCenters)
	g.GET("/routing", a.RoutingOf)
	g.GET("/bom", a.BomOf)
	g.GET("/partner", a.Partner)
	g.GET("/warehouse", a.Warehouse)
	g.GET("/warehouses-by-type", a.WarehousesByType)
}

// internalToken 服务间令牌（EM_INTERNAL_TOKEN，各服务配置一致；fail-fast：未配置拒绝）。
func internalToken() gin.HandlerFunc {
	token := os.Getenv("EM_INTERNAL_TOKEN")
	return func(c *gin.Context) {
		if token == "" || c.GetHeader("X-Internal-Token") != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "bad internal token"})
			return
		}
		c.Next()
	}
}

// BomEdges 全量启用 BOM 边（MRP 每轮一次批量拉取）。
func (a *API) BomEdges(c *gin.Context) {
	tenantID := queryInt64(c, "tenantId")
	var edges []BomEdge
	a.DB.Raw(`SELECT b.material_id parent_id, l.material_id child_id, l.qty, l.scrap_rate
		FROM master.boms b JOIN master.bom_lines l ON l.bom_id=b.id
		WHERE b.tenant_id=? AND b.status='active' AND b.deleted_at IS NULL`, tenantID).Scan(&edges)
	response.OK2(c, gin.H{"list": edges})
}

// Materials 按 ids 批量取物料（逗号分隔；空=全量精简视图）。
func (a *API) Materials(c *gin.Context) {
	tenantID := queryInt64(c, "tenantId")
	q := a.DB.Table("master.materials").
		Select(MaterialCols).
		Where("tenant_id=? AND deleted_at IS NULL", tenantID)
	if ids := c.Query("ids"); ids != "" {
		q = q.Where("id IN (" + ids + ")") // ids 为内部服务传参（int64 逗号串），不做用户输入
	}
	var rows []MaterialLite
	q.Scan(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// WorkCenters 全量工作中心（core 排产/成本/看板）。
func (a *API) WorkCenters(c *gin.Context) {
	tenantID := queryInt64(c, "tenantId")
	var rows []WCLite
	a.DB.Table("master.work_centers").
		Select("id, tenant_id, factory_id, code, name, type, capacity_per_day, cost_rate, status").
		Where("tenant_id=? AND deleted_at IS NULL", tenantID).Scan(&rows)
	if rows == nil {
		rows = []WCLite{}
	}
	response.OK2(c, gin.H{"list": rows})
}

// BomLineLite BOM 行。
type BomLineLite struct {
	ID         int64   `json:"id"`
	BomID      int64   `json:"bomId"`
	MaterialID int64   `json:"materialId"`
	Qty        float64 `json:"qty"`
	ScrapRate  float64 `json:"scrapRate"`
}

// BomDTO BOM（工单快照源；core 侧转 model.Bom 保 JSON 形状）。
type BomDTO struct {
	ID         int64         `json:"id"`
	TenantID   int64         `json:"tenantId"`
	MaterialID int64         `json:"materialId"`
	VersionNo  string        `json:"versionNo"`
	Status     string        `json:"status"`
	Lines      []BomLineLite `json:"lines"`
}

// BomOf 按 id 或 materialId（启用版本）取 BOM。
func (a *API) BomOf(c *gin.Context) {
	tenantID, id, matID := queryInt64(c, "tenantId"), queryInt64(c, "id"), queryInt64(c, "materialId")
	var b BomDTO
	if id > 0 {
		a.DB.Raw(`SELECT b.id, b.tenant_id, b.material_id, b.version_no, b.status
			FROM master.boms b WHERE b.id=? AND b.deleted_at IS NULL`, id).Scan(&b)
	} else {
		a.DB.Raw(`SELECT b.id, b.tenant_id, b.material_id, b.version_no, b.status
			FROM master.boms b WHERE b.tenant_id=? AND b.material_id=? AND b.status='active' AND b.deleted_at IS NULL
			ORDER BY b.id DESC LIMIT 1`, tenantID, matID).Scan(&b)
	}
	if b.ID > 0 {
		a.DB.Raw(`SELECT l.id, l.bom_id, l.material_id, l.qty, l.scrap_rate
			FROM master.bom_lines l WHERE l.bom_id=? ORDER BY l.id`, b.ID).Scan(&b.Lines)
	}
	response.OK2(c, gin.H{"value": b})
}

// RoutingOf 物料的启用工艺路线（工单创建快照源）。
func (a *API) RoutingOf(c *gin.Context) {
	tenantID, matID := queryInt64(c, "tenantId"), queryInt64(c, "materialId")
	var r RoutingDTO
	a.DB.Raw(`SELECT r.id, r.tenant_id, r.material_id, r.version_no, r.status
		FROM master.routings r WHERE r.tenant_id=? AND r.material_id=? AND r.status='active' AND r.deleted_at IS NULL
		ORDER BY r.id DESC LIMIT 1`, tenantID, matID).Scan(&r)
	if r.ID > 0 {
		a.DB.Raw(`SELECT o.id, o.routing_id, o.seq, o.name, o.work_center_id, o.std_hours
			FROM master.routing_ops o WHERE o.routing_id=? ORDER BY o.seq`, r.ID).Scan(&r.Ops)
	}
	response.OK2(c, gin.H{"value": r})
}

// Partner 客商（kind=customer|supplier）。
func (a *API) Partner(c *gin.Context) {
	kind, id := c.DefaultQuery("kind", "customer"), queryInt64(c, "id")
	tbl := "customers"
	if kind == "supplier" {
		tbl = "suppliers"
	}
	var p PartnerLite
	a.DB.Raw(`SELECT id, tenant_id, code, name, status FROM master.`+tbl+` WHERE id=? AND deleted_at IS NULL`, id).Scan(&p)
	response.OK2(c, gin.H{"value": p})
}

// Warehouse 仓库单查。
func (a *API) Warehouse(c *gin.Context) {
	var w WhLite
	a.DB.Raw(`SELECT id, tenant_id, factory_id, code, name, type, status FROM master.warehouses WHERE id=? AND deleted_at IS NULL`, queryInt64(c, "id")).Scan(&w)
	response.OK2(c, gin.H{"value": w})
}

// WarehousesByType 按工厂+类型取可用仓（首仓）。
func (a *API) WarehousesByType(c *gin.Context) {
	var rows []WhLite
	a.DB.Raw(`SELECT id, tenant_id, factory_id, code, name, type, status FROM master.warehouses
		WHERE tenant_id=? AND factory_id=? AND type=? AND status=1 AND deleted_at IS NULL ORDER BY id LIMIT 1`,
		queryInt64(c, "tenantId"), queryInt64(c, "factoryId"), c.Query("type")).Scan(&rows)
	if rows == nil {
		rows = []WhLite{}
	}
	response.OK2(c, gin.H{"list": rows})
}

func queryInt64(c *gin.Context, k string) int64 {
	var v int64
	for _, ch := range c.Query(k) {
		if ch < '0' || ch > '9' {
			return 0
		}
		v = v*10 + int64(ch-'0')
	}
	return v
}
