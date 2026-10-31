// Package internalapi core 服务内部 API（Phase 2 分库）：integration 拉取事件（webhook 扇出）、
// identity 校验工厂停用（查 core 单据表）。/internal/* 仅服务间调用。
package internalapi

import (
	"time"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/evolvermon/evolvermon/internal/client"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type API struct{ DB *gorm.DB }

// Register 挂载 /internal/core/*。
func (a *API) Register(r *gin.Engine) {
	g := r.Group("/internal/core", internalToken())
	g.GET("/events", a.Events)
	g.GET("/factory-check", a.FactoryCheck)
	g.GET("/material-check", a.MaterialCheck)
	g.GET("/insights", a.Insights)
}

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

// Events integration 拉取事件（outbox 增量：id>after，limit≤500）。扇出方按 messageId 幂等。
func (a *API) Events(c *gin.Context) {
	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.OutboxMessage
	a.DB.Select("id, message_id, tenant_id, event, payload").
		Where("id > ? AND sent_at IS NOT NULL", after).Order("id").Limit(limit).Find(&rows)
	if rows == nil {
		rows = []model.OutboxMessage{}
	}
	response.OK2(c, gin.H{"list": rows})
}

// MaterialCheck 物料停用校验（master 侧远程调用）：返回未结单据数。
func (a *API) MaterialCheck(c *gin.Context) {
	materialID, _ := strconv.ParseInt(c.Query("materialId"), 10, 64)
	tenantID, _ := strconv.ParseInt(c.Query("tenantId"), 10, 64)
	if materialID == 0 || tenantID == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, "materialId/tenantId required"))
		return
	}
	var so, po, wo int64
	a.DB.Raw(`SELECT COUNT(*) FROM erp.sales_order_lines l JOIN erp.sales_orders o ON o.id=l.sales_order_id
		WHERE l.material_id=? AND o.tenant_id=? AND o.status IN ('draft','audited','partial') AND o.deleted_at IS NULL`, materialID, tenantID).Scan(&so)
	a.DB.Raw(`SELECT COUNT(*) FROM erp.purchase_order_lines l JOIN erp.purchase_orders o ON o.id=l.purchase_order_id
		WHERE l.material_id=? AND o.tenant_id=? AND o.status IN ('draft','arriving','receiving') AND o.deleted_at IS NULL`, materialID, tenantID).Scan(&po)
	a.DB.Model(&model.WorkOrder{}).Where("tenant_id=? AND material_id=? AND status IN ('draft','released','running','paused')", tenantID, materialID).Count(&wo)
	response.OK2(c, gin.H{"openDocs": so + po + wo})
}

// Insights 智能建议数据源（integration 代理调用；core 直查 wms/mes + master 走通道）。
func (a *API) Insights(c *gin.Context) {
	tenantID, _ := strconv.ParseInt(c.Query("tenantId"), 10, 64)
	factoryID, _ := strconv.ParseInt(c.Query("factoryId"), 10, 64)
	type stockRow struct {
		MaterialID int64
		Q          float64
	}
	var stocks []stockRow
	a.DB.Raw(`SELECT material_id, SUM(qty_on_hand) AS q FROM wms.stocks WHERE factory_id=? GROUP BY material_id`, factoryID).Scan(&stocks)
	onHand := map[int64]float64{}
	for _, r := range stocks {
		onHand[r.MaterialID] = r.Q
	}
	var shorts []gin.H
	if matMap, err := client.M().MaterialMap(c.Request.Context(), tenantID); err == nil {
		for id, mt := range matMap {
			if mt.Status == 1 && mt.SafetyStock > 0 && onHand[id] < mt.SafetyStock {
				shorts = append(shorts, gin.H{"code": mt.Code, "name": mt.Name, "onHand": onHand[id], "safety": mt.SafetyStock})
				if len(shorts) >= 10 {
					break
				}
			}
		}
	}
	var loads []struct {
		WorkCenterID int64
		Ops          int64
	}
	a.DB.Raw(`SELECT o.work_center_id, COUNT(o.id) AS ops FROM mes.wo_operations o
		WHERE o.status IN ('waiting','assigned','running') GROUP BY o.work_center_id`).Scan(&loads)
	opCount := map[int64]int64{}
	for _, l := range loads {
		opCount[l.WorkCenterID] = l.Ops
	}
	wcl := []gin.H{}
	if wcs, err := client.M().WorkCenters(c.Request.Context(), tenantID); err == nil {
		for _, wc := range wcs {
			if wc.FactoryID != nil && *wc.FactoryID == factoryID && wc.Status == 1 {
				wcl = append(wcl, gin.H{"name": wc.Name, "ops": opCount[wc.ID], "cap": wc.CapacityPerDay})
			}
		}
	}
	// 交期风险（原 m6 Insights 逻辑迁移：core 直查 mes）
	type dueRisk struct {
		DocNo     string  `json:"docNo"`
		Progress  float64 `json:"progress"`
		PlanEndAt *time.Time `json:"planEndAt"`
	}
	var risks []dueRisk
	var wos []model.WorkOrder
	a.DB.Where("tenant_id=? AND factory_id=? AND status IN ('released','running') AND plan_end_at IS NOT NULL AND plan_end_at < now() + interval '2 days'", tenantID, factoryID).Find(&wos)
	for _, w := range wos {
		progress := 0.0
		if w.Qty > 0 {
			progress = w.GoodQty / w.Qty * 100
		}
		if progress < 50 {
			risks = append(risks, dueRisk{DocNo: w.DocNo, Progress: progress, PlanEndAt: w.PlanEndAt})
		}
	}
	response.OK2(c, gin.H{"shortages": shorts, "dueRisks": risks, "wcLoads": wcl})
}

// FactoryCheck 工厂停用校验（identity 侧远程调用）：返回未结单据数，>0 禁止停用。
func (a *API) FactoryCheck(c *gin.Context) {
	factoryID, _ := strconv.ParseInt(c.Query("factoryId"), 10, 64)
	if factoryID == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, "factoryId required"))
		return
	}
	var wo, so int64
	a.DB.Model(&model.WorkOrder{}).Where("factory_id=? AND status IN ('draft','released','running','paused','finished')", factoryID).Count(&wo)
	a.DB.Model(&model.StockOrder{}).Where("factory_id=? AND status IN ('draft','doing')", factoryID).Count(&so)
	response.OK2(c, gin.H{"openDocs": wo + so})
}
