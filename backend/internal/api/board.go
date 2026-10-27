package api

// 工作台与看板（FR-MES-050~051、dashboard §7 API）。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type BoardAPI struct{ DB *gorm.DB }

// KanbanBoard 在制工单墙（状态色块/进度/超期红牌 FR-MES-050）。
func (b *BoardAPI) KanbanBoard(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var wos []model.WorkOrder
	b.DB.Scopes(data.ScopeFor(ac, true)).
		Where("status IN ('released','running','paused')").Order("plan_end_at").Limit(100).Find(&wos)
	type card struct {
		ID         int64     `json:"id"`
		DocNo      string    `json:"docNo"`
		Status     string    `json:"status"`
		Priority   int       `json:"priority"`
		Qty        float64   `json:"qty"`
		GoodQty    float64   `json:"goodQty"`
		Progress   float64   `json:"progress"`
		Overdue    bool      `json:"overdue"`
		PlanEndAt  *time.Time `json:"planEndAt"`
	}
	now := time.Now()
	var cards []card
	for _, wo := range wos {
		progress := 0.0
		if wo.Qty > 0 {
			progress = wo.GoodQty / wo.Qty * 100
		}
		cards = append(cards, card{
			ID: wo.ID, DocNo: wo.DocNo, Status: string(wo.Status), Priority: wo.Priority,
			Qty: wo.Qty, GoodQty: wo.GoodQty, Progress: progress,
			Overdue: wo.PlanEndAt != nil && wo.PlanEndAt.Before(now) && wo.Status != model.WoClosed,
			PlanEndAt: wo.PlanEndAt,
		})
	}
	response.OK2(c, cards)
}

// KanbanToday 今日产量与合格率（FR-MES-051）。
func (b *BoardAPI) KanbanToday(c *gin.Context) {
	ac := middleware.GetAuth(c)
	start := time.Now().Truncate(24 * time.Hour)
	var rows []struct {
		WorkOrderID int64   `json:"workOrderId"`
		DocNo       string  `json:"docNo"`
		QtyOk       float64 `json:"qtyOk"`
		QtyDefect   float64 `json:"qtyDefect"`
	}
	b.DB.Raw(`SELECT r.work_order_id, w.doc_no, SUM(r.qty_ok) AS qty_ok, SUM(r.qty_defect) AS qty_defect
		FROM production_reports r JOIN work_orders w ON w.id=r.work_order_id
		WHERE r.factory_id=? AND r.reported_at >= ?
		GROUP BY r.work_order_id, w.doc_no ORDER BY qty_ok DESC`, ac.FactoryID, start).Scan(&rows)
	var totalOK, totalDefect float64
	for _, r := range rows {
		totalOK += r.QtyOk
		totalDefect += r.QtyDefect
	}
	rate := 100.0
	if totalOK+totalDefect > 0 {
		rate = totalOK / (totalOK + totalDefect) * 100
	}
	response.OK2(c, gin.H{"rows": rows, "totalOk": totalOK, "totalDefect": totalDefect, "passRate": rate})
}

// DashboardSummary 工作台卡片（§7 API）。
func (b *BoardAPI) DashboardSummary(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var counts struct {
		SalesPending  int64 `json:"salesPending"`
		WoActive      int64 `json:"woActive"`
		WoOverdue     int64 `json:"woOverdue"`
		PoArriving    int64 `json:"poArriving"`
		PoOverdue     int64 `json:"poOverdue"`
		RcvTodo       int64 `json:"rcvTodo"`
		ShpTodo       int64 `json:"shpTodo"`
		QcPending     int64 `json:"qcPending"`
		MaterialCount int64 `json:"materialCount"`
	}
	sc := data.ScopeFor(ac, true)
	b.DB.Model(&model.SalesOrder{}).Scopes(sc).Where("status='draft'").Count(&counts.SalesPending)
	b.DB.Model(&model.WorkOrder{}).Scopes(sc).Where("status IN ('released','running','paused')").Count(&counts.WoActive)
	b.DB.Model(&model.WorkOrder{}).Scopes(sc).
		Where("status IN ('released','running','paused') AND plan_end_at < now()").Count(&counts.WoOverdue)
	b.DB.Model(&model.PurchaseOrder{}).Scopes(sc).Where("status IN ('arriving','receiving')").Count(&counts.PoArriving)
	b.DB.Model(&model.PurchaseOrder{}).Scopes(sc).
		Where("status IN ('arriving','receiving') AND EXISTS (SELECT 1 FROM purchase_order_lines l WHERE l.purchase_order_id = purchase_orders.id AND l.delivery_date < now())").
		Count(&counts.PoOverdue)
	b.DB.Model(&model.StockOrder{}).Scopes(sc).Where("status IN ('draft','doing') AND doc_type='rcv'").Count(&counts.RcvTodo)
	b.DB.Model(&model.StockOrder{}).Scopes(sc).Where("status IN ('draft','doing') AND doc_type='shp'").Count(&counts.ShpTodo)
	b.DB.Model(&model.QcRecord{}).Scopes(sc).Where("result='pending'").Count(&counts.QcPending)
	b.DB.Model(&model.Material{}).Scopes(data.ScopeFor(ac, false)).Count(&counts.MaterialCount)

	// 安全库存预警
	var alerts []struct {
		ID    int64   `json:"id"`
		Code  string  `json:"code"`
		Name  string  `json:"name"`
		OnHand float64 `json:"onHand"`
		Safety float64 `json:"safety"`
	}
	b.DB.Raw(`SELECT mt.id, mt.code, mt.name, COALESCE(s.q,0) AS on_hand, mt.safety_stock AS safety
		FROM materials mt
		LEFT JOIN (SELECT material_id, SUM(qty_on_hand) AS q FROM stocks WHERE factory_id=? GROUP BY material_id) s
		  ON s.material_id = mt.id
		WHERE mt.tenant_id=? AND mt.status=1 AND mt.safety_stock > 0 AND COALESCE(s.q,0) < mt.safety_stock
		LIMIT 20`, ac.FactoryID, ac.TenantID).Scan(&alerts)

	// 近 7 日报工趋势
	var trend []struct {
		Day   string  `json:"day"`
		QtyOk float64 `json:"qtyOk"`
	}
	b.DB.Raw(`SELECT to_char(reported_at,'MM-DD') AS day, SUM(qty_ok) AS qty_ok
		FROM production_reports WHERE factory_id=? AND reported_at > now() - interval '7 days'
		GROUP BY 1 ORDER BY 1`, ac.FactoryID).Scan(&trend)

	response.OK2(c, gin.H{"counts": counts, "stockAlerts": alerts, "reportTrend": trend})
}
