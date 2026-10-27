package api

// M6：Webhook 管理、设备遥测接入、智能建议（启发式 AI-lite）。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type ExtAPI struct{ DB *gorm.DB }

// ---- Webhook 管理 ----

func (e *ExtAPI) ListWebhooks(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var eps []model.WebhookEndpoint
	e.DB.Where("tenant_id=?", ac.TenantID).Find(&eps)
	response.OK2(c, eps)
}

func (e *ExtAPI) CreateWebhook(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var ep model.WebhookEndpoint
	if err := c.ShouldBindJSON(&ep); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	ep.TenantID = ac.TenantID
	ep.Status = 1
	if err := e.DB.Create(&ep).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, err.Error()))
		return
	}
	response.OK2(c, ep)
}

func (e *ExtAPI) DeleteWebhook(c *gin.Context) {
	ac := middleware.GetAuth(c)
	e.DB.Where("id=? AND tenant_id=?", c.Param("id"), ac.TenantID).Delete(&model.WebhookEndpoint{})
	response.OK2(c, gin.H{"deleted": true})
}

func (e *ExtAPI) ListDeliveries(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := e.DB.Model(&model.WebhookDelivery{}).Where("tenant_id=?", ac.TenantID)
	if v := c.Query("status"); v != "" {
		q = q.Where("status=?", v)
	}
	var list []model.WebhookDelivery
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(50).Find(&list)
	response.Page(c, list, total)
}

// ---- 设备遥测（推送式网关接口：设备/网关侧采集后推送） ----

func (e *ExtAPI) PushTelemetry(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		Metric     string  `json:"metric"`
		Value      float64 `json:"value"`
		WorkCenter string  `json:"workCenter"`
		ReportedAt string  `json:"reportedAt"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Metric == "" {
		response.Fail(c, response.NewBiz(response.ErrParam, "metric/value 必填"))
		return
	}
	t := time.Now()
	if req.ReportedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, req.ReportedAt); err == nil {
			t = parsed
		}
	}
	row := model.DeviceTelemetry{
		TenantID: ac.TenantID, FactoryID: ac.FactoryID,
		DeviceCode: c.Param("code"), Metric: req.Metric, Value: req.Value,
		WorkCenter: req.WorkCenter, ReportedAt: t,
	}
	if err := e.DB.Create(&row).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

func (e *ExtAPI) LatestTelemetry(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var rows []model.DeviceTelemetry
	e.DB.Raw(`SELECT DISTINCT ON (device_code, metric) *
		FROM device_telemetries
		WHERE tenant_id=? AND factory_id=?
		ORDER BY device_code, metric, reported_at DESC`, ac.TenantID, ac.FactoryID).Scan(&rows)
	response.OK2(c, rows)
}

// ---- 智能建议（启发式 AI-lite：缺料/交期/产能三类洞察） ----

func (e *ExtAPI) Insights(c *gin.Context) {
	ac := middleware.GetAuth(c)
	type insight struct {
		Kind     string `json:"kind"` // shortage | due-risk | capacity
		Level    string `json:"level"` // high | medium
		Title    string `json:"title"`
		Detail   string `json:"detail"`
		Action   string `json:"action"`
	}
	out := []insight{}

	// 1) 缺料预警：低于安全库存且无在途在制覆盖
	var shorts []struct {
		Code   string  `json:"code"`
		Name   string  `json:"name"`
		OnHand float64 `json:"onHand"`
		Safety float64 `json:"safety"`
	}
	e.DB.Raw(`SELECT mt.code, mt.name,
		COALESCE((SELECT SUM(s.qty_on_hand) FROM stocks s WHERE s.material_id=mt.id AND s.factory_id=?),0) AS on_hand,
		mt.safety_stock AS safety
		FROM materials mt
		WHERE mt.tenant_id=? AND mt.status=1 AND mt.safety_stock>0
		  AND COALESCE((SELECT SUM(s.qty_on_hand) FROM stocks s WHERE s.material_id=mt.id AND s.factory_id=?),0) < mt.safety_stock
		LIMIT 10`, ac.FactoryID, ac.TenantID, ac.FactoryID).Scan(&shorts)
	for _, s := range shorts {
		out = append(out, insight{"shortage", "high",
			"缺料预警：" + s.Name,
			"现存量 " + f64(s.OnHand) + " 已低于安全库存 " + f64(s.Safety),
			"运行 MRP 生成补货建议，或人工创建请购"})
	}

	// 2) 交期风险：距计划完工 <2 天且进度 <50%
	var risks []model.WorkOrder
	e.DB.Scopes(data.ScopeFor(ac, true)).
		Where("status IN ('released','running') AND plan_end_at IS NOT NULL AND plan_end_at < now() + interval '2 days'").Find(&risks)
	for _, w := range risks {
		progress := 0.0
		if w.Qty > 0 {
			progress = w.GoodQty / w.Qty * 100
		}
		if progress < 50 {
			out = append(out, insight{"due-risk", "high",
				"交期风险：" + w.DocNo,
				"进度仅 " + f64(progress) + "%，计划完工 " + ts(w.PlanEndAt),
				"提升优先级插单排产，或与客户协商改期"})
		}
	}

	// 3) 产能洞察：负载最高的工作中心
	var loads []struct {
		Name  string  `json:"name"`
		Ops   int64   `json:"ops"`
		Cap   float64 `json:"cap"`
	}
	e.DB.Raw(`SELECT wc.name, COUNT(o.id) AS ops, wc.capacity_per_day AS cap
		FROM work_centers wc
		LEFT JOIN wo_operations o ON o.work_center_id=wc.id AND o.status IN ('waiting','assigned','running')
		WHERE wc.tenant_id=? AND wc.factory_id=? AND wc.status=1
		GROUP BY wc.id, wc.name, wc.capacity_per_day ORDER BY ops DESC LIMIT 3`, ac.TenantID, ac.FactoryID).Scan(&loads)
	for _, l := range loads {
		if l.Ops >= 2 && l.Cap > 0 && float64(l.Ops) > l.Cap {
			out = append(out, insight{"capacity", "medium",
				"产能瓶颈：" + l.Name,
				"待执行工序 " + i64s(l.Ops) + " 道，超出日产能 " + f64(l.Cap),
				"安排加班、临时转移工序或调整排产优先级"})
		}
	}
	response.OK2(c, gin.H{"insights": out, "generatedAt": time.Now().Format(time.RFC3339)})
}
