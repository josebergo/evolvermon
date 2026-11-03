package api

// M6：Webhook 管理、设备遥测接入、智能建议（启发式 AI-lite）。

import (
	"encoding/json"
	"net/http"
	"fmt"
	"os"
	"evolvermon/backend/internal/pkg/conv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"evolvermon/backend/internal/middleware"
	"evolvermon/backend/internal/model"
	"evolvermon/backend/internal/pkg/response"
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
		FROM integration.device_telemetries
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

	// 1) 缺料预警（master 分库：经 core /internal/core/insights 取数）
	if data, ierr := coreInsights(c, ac); ierr == nil {
		for _, x := range data.Shortages {
			out = append(out, insight{"shortage", "high",
				"缺料预警：" + x.Name,
				"现存量 " + conv.F64(x.OnHand) + " 已低于安全库存 " + conv.F64(x.Safety),
				"运行 MRP 生成补货建议，或人工创建请购"})
		}
	}

// 2) 交期风险（core 代理：integration 库无 mes 表）
	if data, ierr := coreInsights(c, ac); ierr == nil {
		for _, w := range data.DueRisks {
			out = append(out, insight{"due-risk", "high",
				"交期风险：" + w.DocNo,
				"进度仅 " + conv.F64(w.Progress) + "%，计划完工 " + conv.Ts(w.PlanEndAt),
				"提升优先级插单排产，或与客户协商改期"})
		}
	}

// 3) 产能洞察：负载最高的工作中心
	// 3) 产能洞察（core 代理：integration 库无跨域表）
	if data, ierr := coreInsights(c, ac); ierr == nil {
		for _, l := range data.WcLoads {
			if l.Ops >= 2 && l.Cap > 0 && float64(l.Ops) > l.Cap {
				out = append(out, insight{"capacity", "medium",
					"产能瓶颈：" + l.Name,
					"待执行工序 " + conv.I64s(l.Ops) + " 道，超出日产能 " + conv.F64(l.Cap),
					"安排加班、临时转移工序或调整排产优先级"})
			}
		}
	}

	response.OK2(c, gin.H{"insights": out, "generatedAt": time.Now().Format(time.RFC3339)})
}

// coreInsights 从 core 取智能建议数据源（EM_CORE_URL；Lite 由本进程内 core 逻辑等价执行）。
func coreInsights(c *gin.Context, ac *middleware.AuthCtx) (*insightsData, error) {
	base := os.Getenv("EM_CORE_URL")
	if base == "" {
		base = "http://127.0.0.1:8082"
	}
	req, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/internal/core/insights?tenantId=%d&factoryId=%d", base, ac.TenantID, ac.FactoryID), nil)
	req.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("core insights: http %d", resp.StatusCode)
	}
	var out struct {
		Data insightsData `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

type insightsData struct {
	Shortages []struct {
		Code   string  `json:"code"`
		Name   string  `json:"name"`
		OnHand float64 `json:"onHand"`
		Safety float64 `json:"safety"`
	} `json:"shortages"`
	DueRisks []struct {
		DocNo     string     `json:"docNo"`
		Progress  float64    `json:"progress"`
		PlanEndAt *time.Time `json:"planEndAt"`
	} `json:"dueRisks"`
	WcLoads []struct {
		Name string  `json:"name"`
		Ops  int64   `json:"ops"`
		Cap  float64 `json:"cap"`
	} `json:"wcLoads"`
}
