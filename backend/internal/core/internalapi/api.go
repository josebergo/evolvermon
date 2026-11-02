// Package internalapi core 服务内部 API（Phase 2 分库）：integration 拉取事件（webhook 扇出）、
// identity 校验工厂停用（查 core 单据表）。/internal/* 仅服务间调用。
package internalapi

import (
	"encoding/json"
	"fmt"
	"io"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"strings"
	"time"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/evolvermon/evolvermon/internal/wms/posting"
	wapi "github.com/evolvermon/evolvermon/internal/wms/wapi"
	"github.com/evolvermon/evolvermon/internal/data"
	wmssvc "github.com/evolvermon/evolvermon/internal/wms/svc"
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
	g.GET("/wms/stock-sums", a.StockSums)
	g.GET("/wms/reserved-sums", a.ReservedSums)
	g.GET("/wms/stock-single", a.StockSingle)
	g.GET("/wms/stockorder-todos", a.StockOrderTodos)
	g.POST("/wms/create-order", a.WmsCreateOrder)
	g.POST("/wms/reserve", a.WmsReserve)
	g.POST("/wms/release", a.WmsRelease)
	g.POST("/mes/create-wo", a.MesCreateWO)
	g.GET("/mes/has-iqc", a.HasIqc)
	g.GET("/wms/pick-agg", a.PickAgg)
	g.GET("/wms/trace-serial", a.TraceSerial)
	g.GET("/wms/trace-batch", a.TraceBatch)
	g.POST("/write-back", a.WriteBackEp)
	g.POST("/wms/transfer-bucket", a.TransferBucketEp)
	g.POST("/wms/release-pending", a.ReleasePendingEp)
	g.GET("/mes/wos", a.MesWorkOrders)
	g.GET("/mes/wo-ops", a.MesWoOps)
	g.GET("/mes/wo-counts", a.MesWoCounts)
	g.GET("/mes/wip-sums", a.MesWipSums)
	g.GET("/erp/order-counts", a.ErpOrderCounts)
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
	if erpURL := os.Getenv("EM_ERP_URL"); erpURL != "" {
		q, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/core/erp/mat-open-count?tenantId=%d&materialId=%d", erpURL, tenantID, materialID), nil)
		q.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(q); err == nil {
			var out struct {
				Data struct {
					N int64 `json:"n"`
				} `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			so, po = out.Data.N, 0 // 端点返回 so+po 合计（po 并入 N）
		}
	} else {
		a.DB.Raw(`SELECT COUNT(*) FROM erp.sales_order_lines l JOIN erp.sales_orders o ON o.id=l.sales_order_id
			WHERE l.material_id=? AND o.tenant_id=? AND o.status IN ('draft','audited','partial') AND o.deleted_at IS NULL`, materialID, tenantID).Scan(&so)
		a.DB.Raw(`SELECT COUNT(*) FROM erp.purchase_order_lines l JOIN erp.purchase_orders o ON o.id=l.purchase_order_id
			WHERE l.material_id=? AND o.tenant_id=? AND o.status IN ('draft','arriving','receiving') AND o.deleted_at IS NULL`, materialID, tenantID).Scan(&po)
	}
	if mesURL := os.Getenv("EM_MES_URL"); mesURL != "" {
		q, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/core/mes/wo-open-count?factoryId=%d", mesURL, tenantID), nil)
		q.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(q); err == nil {
			var out struct {
				Data struct {
					N int64 `json:"n"`
				} `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			wo = out.Data.N
		}
	} else {
		a.DB.Model(&model.WorkOrder{}).Where("tenant_id=? AND material_id=? AND status IN ('draft','released','running','paused')", tenantID, materialID).Count(&wo)
	}
	response.OK2(c, gin.H{"openDocs": so + po + wo})
}

// Insights 智能建议数据源（integration 代理调用；core 直查 wms/mes + master 走通道）。
func (a *API) Insights(c *gin.Context) {
	// 三库：insights 聚合读 mes+wms——转发 mes 进程（归属域）；共享库直接执行
	if mesURL := os.Getenv("EM_MES_URL"); mesURL != "" {
		q, _ := http.NewRequest(http.MethodGet, mesURL+"/internal/core/insights?tenantId="+c.Query("tenantId")+"&factoryId="+c.Query("factoryId"), nil)
		q.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(q); err == nil {
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			c.Data(http.StatusOK, "application/json", b)
			return
		}
	}
		tenantID, _ := strconv.ParseInt(c.Query("tenantId"), 10, 64)
	factoryID, _ := strconv.ParseInt(c.Query("factoryId"), 10, 64)
	type stockRow struct {
		MaterialID int64
		Q          float64
	}
	var stocks []stockRow
	if wmsURL := os.Getenv("EM_WMS_URL"); wmsURL != "" {
		q2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/core/wms/stock-sums?tenantId=%d&factoryId=%d", wmsURL, tenantID, factoryID), nil)
		q2.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(q2); err == nil {
			var out struct {
				Data struct {
					List []struct {
						MaterialID int64   `json:"materialId"`
						Qty        float64 `json:"qty"`
					} `json:"list"`
				} `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			for _, x := range out.Data.List {
				stocks = append(stocks, stockRow{MaterialID: x.MaterialID, Q: x.Qty})
			}
		}
	} else {
		a.DB.Raw(`SELECT material_id, SUM(qty_on_hand) AS q FROM wms.stocks WHERE factory_id=? GROUP BY material_id`, factoryID).Scan(&stocks)
	}
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

// StockSums 按物料汇总现有量（erp MRP/plan、mes board 经通道）。
func (a *API) StockSums(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	var rows []struct {
		MaterialID int64   `json:"materialId"`
		Qty        float64 `json:"qty"`
	}
	a.DB.Raw(`SELECT material_id, SUM(qty_on_hand) qty FROM wms.stocks WHERE tenant_id=? AND factory_id=? GROUP BY material_id`, tenantID, factoryID).Scan(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// ReservedSums 按物料汇总预占 held。
func (a *API) ReservedSums(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	var rows []struct {
		MaterialID int64   `json:"materialId"`
		Qty        float64 `json:"qty"`
	}
	a.DB.Raw(`SELECT material_id, SUM(qty) qty FROM wms.stock_reservations WHERE tenant_id=? AND factory_id=? AND status='held' GROUP BY material_id`, tenantID, factoryID).Scan(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// StockSingle 单物料现有量。
func (a *API) StockSingle(c *gin.Context) {
	tenantID, factoryID, matID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId"), queryInt64(c, "materialId")
	var q float64
	a.DB.Raw(`SELECT COALESCE(SUM(qty_on_hand),0) FROM wms.stocks WHERE tenant_id=? AND factory_id=? AND material_id=?`, tenantID, factoryID, matID).Scan(&q)
	response.OK2(c, gin.H{"qty": q})
}

// StockOrderTodos 出入库待办计数（board）。
func (a *API) StockOrderTodos(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	var rcv, shp int64
	a.DB.Raw(`SELECT COUNT(*) FROM wms.stock_orders WHERE tenant_id=? AND factory_id=? AND status IN ('draft','doing') AND doc_type='rcv'`, tenantID, factoryID).Scan(&rcv)
	a.DB.Raw(`SELECT COUNT(*) FROM wms.stock_orders WHERE tenant_id=? AND factory_id=? AND status IN ('draft','doing') AND doc_type='shp'`, tenantID, factoryID).Scan(&shp)
	response.OK2(c, gin.H{"rcv": rcv, "shp": shp})
}

// WmsCreateOrder 幂等建单（mes/erp 经 Writer 通道；幂等键入 Remark）。
func (a *API) WmsCreateOrder(c *gin.Context) {
	var req wapi.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	res, err := wapi.CreateOrder(a.DB, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, res)
}

// WmsReserve/WmsRelease 预占建释放（erp 审核链）。
func (a *API) WmsReserve(c *gin.Context) {
	var req wapi.ReserveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	if err := wapi.Reserve(a.DB, req); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

func (a *API) WmsRelease(c *gin.Context) {
	var req wapi.ReleaseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	if err := wapi.Release(a.DB, req); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// ReleasePendingEp 待检量转目标桶（mes quality 联动；wms 侧原子判定+转移）。
func (a *API) ReleasePendingEp(c *gin.Context) {
	var req struct {
		TenantID    int64   `json:"tenantId"`
		FactoryID   int64   `json:"factoryId"`
		UserID      int64   `json:"userId"`
		SourceDocID int64   `json:"sourceDocId"`
		MaterialID  int64   `json:"materialId"`
		BatchNo     string  `json:"batchNo"`
		Qty         float64 `json:"qty"`
		To          string  `json:"to"`
		BizType     string  `json:"bizType"`
		SrcNo       string  `json:"srcNo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MaterialID == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var order model.StockOrder
	if err := a.DB.First(&order, req.SourceDocID).Error; err != nil {
		response.OK2(c, gin.H{"ok": false})
		return
	}
	ac := &middleware.AuthCtx{TenantID: req.TenantID, FactoryID: req.FactoryID, UserID: req.UserID}
	var stock model.Stock
	if err := a.DB.Where("tenant_id=? AND factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=?",
		req.TenantID, req.FactoryID, order.WarehouseID, req.MaterialID, req.BatchNo).
		First(&stock).Error; err != nil || stock.QtyPending < req.Qty-1e-9 {
		response.OK2(c, gin.H{"ok": false}) // 无待检量：跳过
		return
	}
	if err := wmssvc.TransferBucket(a.DB, ac, order.WarehouseID, model.StockOrderLine{MaterialID: req.MaterialID},
		req.BatchNo, req.Qty, "pending", req.To, req.BizType, req.SrcNo); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// TransferBucketEp QC 桶间转移（mes quality 处置联动；Phase 3 ③B 收尾）。
func (a *API) TransferBucketEp(c *gin.Context) {
	var req struct {
		TenantID   int64  `json:"tenantId"`
		FactoryID  int64  `json:"factoryId"`
		UserID     int64  `json:"userId"`
		WarehouseID int64 `json:"warehouseId"`
		MaterialID int64  `json:"materialId"`
		BatchNo    string `json:"batchNo"`
		Qty        float64 `json:"qty"`
		From       string `json:"from"`
		To         string `json:"to"`
		BizType    string `json:"bizType"`
		SrcNo      string `json:"srcNo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MaterialID == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	ac := &middleware.AuthCtx{TenantID: req.TenantID, FactoryID: req.FactoryID, UserID: req.UserID}
	line := model.StockOrderLine{MaterialID: req.MaterialID}
	if err := wmssvc.TransferBucket(a.DB, ac, req.WarehouseID, line, req.BatchNo, req.Qty, req.From, req.To, req.BizType, req.SrcNo); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// WriteBackEp 过账回写端点（Phase 3 ③B-③：wms 过账事务内回写 erp/mes；source 分派）。
func (a *API) WriteBackEp(c *gin.Context) {
	var req posting.WBReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	if err := posting.WriteBackLocal(a.DB, req); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// PickAgg 工单领料/退料聚合（mes ReturnMaterial 经通道；三库）。
func (a *API) PickAgg(c *gin.Context) {
	tenantID, woID := queryInt64(c, "tenantId"), queryInt64(c, "workOrderId")
	type aggRow struct {
		MaterialID int64   `json:"materialId"`
		Qty        float64 `json:"qty"`
	}
	var picked, returned []aggRow
	a.DB.Raw(`SELECT l.material_id, SUM(l.qty) qty FROM wms.stock_order_lines l
		JOIN wms.stock_orders o ON o.id=l.stock_order_id
		WHERE o.tenant_id=? AND o.source_type='picking' AND o.source_doc_id=? AND o.status='done'
		GROUP BY l.material_id`, tenantID, woID).Scan(&picked)
	a.DB.Raw(`SELECT l.material_id, SUM(l.done_qty) qty FROM wms.stock_order_lines l
		JOIN wms.stock_orders o ON o.id=l.stock_order_id
		WHERE o.tenant_id=? AND o.source_type='return' AND o.source_doc_id=? AND o.status IN ('done','doing')
		GROUP BY l.material_id`, tenantID, woID).Scan(&returned)
	var lastB []aggRow
	_ = lastB
	var batches []struct {
		MaterialID int64   `json:"materialId"`
		Batch      string  `json:"batch"`
	}
	a.DB.Raw(`SELECT DISTINCT ON (material_id) material_id, batch_no AS batch FROM wms.stock_movements
		WHERE tenant_id=? AND biz_type='领料出库'
		AND source_doc_id IN (SELECT id FROM wms.stock_orders WHERE source_type='picking' AND source_doc_id=?)
		ORDER BY material_id, id DESC`, tenantID, woID).Scan(&batches)
	response.OK2(c, gin.H{"picked": picked, "returned": returned, "batches": batches})
}

// HasIqc 物料是否配置 IQC 方案（wms 过账判定）。
func (a *API) HasIqc(c *gin.Context) {
	tenantID, matID := queryInt64(c, "tenantId"), queryInt64(c, "materialId")
	var n int64
	a.DB.Raw(`SELECT COUNT(*) FROM mes.qc_schemes WHERE tenant_id=? AND material_id=? AND check_type='iqc'`, tenantID, matID).Scan(&n)
	response.OK2(c, gin.H{"has": n > 0})
}

// MesCreateWO 幂等建工单（mrp 转单；幂等键入 Remark）。
func (a *API) MesCreateWO(c *gin.Context) {
	var req struct {
		TenantID   int64  `json:"tenantId"`
		FactoryID  int64  `json:"factoryId"`
		UserID     int64  `json:"userId"`
		MaterialID int64  `json:"materialId"`
		Qty        float64 `json:"qty"`
		SourceType string `json:"sourceType"`
		SourceDocNo string `json:"sourceDocNo"`
		PlanStartAt *time.Time `json:"planStartAt"`
		PlanEndAt   *time.Time `json:"planEndAt"`
		IdemKey    string `json:"idemKey"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MaterialID == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, "materialId required"))
		return
	}
	idem := ""
	if req.IdemKey != "" {
		idem = "idem:" + req.IdemKey
		var exist model.WorkOrder
		if err := a.DB.Where("tenant_id=? AND remark=?", req.TenantID, idem).First(&exist).Error; err == nil {
			response.OK2(c, gin.H{"woId": exist.ID, "docNo": exist.DocNo, "reused": true})
			return
		}
	}
	docNo, err := data.NextDocNo(a.DB, req.TenantID, "WO")
	if err != nil {
		response.Fail(c, err)
		return
	}
	two := model.WorkOrder{
		MaterialID: req.MaterialID, Qty: req.Qty, SourceType: req.SourceType,
		SourceDocNo: req.SourceDocNo, PlanStartAt: req.PlanStartAt, PlanEndAt: req.PlanEndAt,
		Remark: idem,
	}
	two.TenantID = req.TenantID
	two.FactoryID = &req.FactoryID
	two.DocNo = docNo
	if err := a.DB.Create(&two).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"woId": two.ID, "docNo": two.DocNo})
}

// MesWorkOrders 工单列表（erp 计划/MRP 经通道；statuses 逗号分隔可选）。
func (a *API) MesWorkOrders(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	q := a.DB.Table("mes.work_orders").
		Select("id, tenant_id, factory_id, doc_no, material_id, qty, good_qty, priority, plan_start_at, plan_end_at, status").
		Where("tenant_id=? AND deleted_at IS NULL", tenantID)
	if factoryID > 0 {
		q = q.Where("factory_id=?", factoryID)
	}
	if st := c.Query("statuses"); st != "" {
		q = q.Where("status IN ('" + strings.ReplaceAll(st, ",", "','") + "')")
	}
	var rows []map[string]any
	q.Order("id").Limit(2000).Find(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// MesWoOps 工序列表（plan 排产/派工板）。
func (a *API) MesWoOps(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	q := a.DB.Table("mes.wo_operations").
		Select("id, work_order_id, work_center_id, seq, name, status, qty_ok, plan_start_at, plan_end_at, plan_hours").
		Where("tenant_id=?", tenantID)
	if factoryID > 0 {
		q = q.Where("factory_id=?", factoryID)
	}
	if st := c.Query("statuses"); st != "" {
		q = q.Where("status IN ('" + strings.ReplaceAll(st, ",", "','") + "')")
	}
	var rows []map[string]any
	q.Order("id").Limit(5000).Find(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// MesWoCounts 工单活跃/超期计数（board）。
func (a *API) MesWoCounts(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	var woActive, woOverdue int64
	a.DB.Raw(`SELECT COUNT(*) FROM mes.work_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('released','running','paused')`, tenantID, factoryID).Scan(&woActive)
	a.DB.Raw(`SELECT COUNT(*) FROM mes.work_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('released','running','paused') AND plan_end_at < now()`, tenantID, factoryID).Scan(&woOverdue)
	response.OK2(c, gin.H{"woActive": woActive, "woOverdue": woOverdue})
}

// MesWipSums 按物料汇总在制剩余（mrp 供给计算）。
func (a *API) MesWipSums(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	var rows []struct {
		MaterialID int64   `json:"materialId"`
		Qty        float64 `json:"qty"`
	}
	a.DB.Raw(`SELECT material_id, COALESCE(SUM(qty-good_qty),0) qty FROM mes.work_orders
		WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('released','running','paused','finished') GROUP BY material_id`, tenantID, factoryID).Scan(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// ErpOrderCounts 销售/采购待办计数（board）。
func (a *API) ErpOrderCounts(c *gin.Context) {
	tenantID, factoryID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId")
	var salesPending, poArriving, poOverdue int64
	a.DB.Raw(`SELECT COUNT(*) FROM erp.sales_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status='draft'`, tenantID, factoryID).Scan(&salesPending)
	a.DB.Raw(`SELECT COUNT(*) FROM erp.purchase_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('arriving','receiving')`, tenantID, factoryID).Scan(&poArriving)
	a.DB.Raw(`SELECT COUNT(*) FROM erp.purchase_orders po WHERE po.tenant_id=? AND po.factory_id=? AND po.deleted_at IS NULL AND po.status IN ('arriving','receiving')
		AND EXISTS (SELECT 1 FROM erp.purchase_order_lines l WHERE l.purchase_order_id=po.id AND l.delivery_date < now())`, tenantID, factoryID).Scan(&poOverdue)
	response.OK2(c, gin.H{"salesPending": salesPending, "poArriving": poArriving, "poOverdue": poOverdue})
}

// FactoryCheck 工厂停用校验（identity 侧远程调用）：返回未结单据数，>0 禁止停用。
func (a *API) FactoryCheck(c *gin.Context) {
	factoryID, _ := strconv.ParseInt(c.Query("factoryId"), 10, 64)
	if factoryID == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, "factoryId required"))
		return
	}
	var wo, so int64
	// 三库：工单计数经 mes 端点（EM_MES_URL）；出入库单在 wms 自域
	if mesURL := os.Getenv("EM_MES_URL"); mesURL != "" {
		q, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/core/mes/wo-open-count?factoryId=%d", mesURL, factoryID), nil)
		q.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(q); err == nil {
			var out struct {
				Data struct {
					N int64 `json:"n"`
				} `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			wo = out.Data.N
		}
	} else {
		a.DB.Model(&model.WorkOrder{}).Where("factory_id=? AND status IN ('draft','released','running','paused','finished')", factoryID).Count(&wo)
	}
	a.DB.Model(&model.StockOrder{}).Where("factory_id=? AND status IN ('draft','doing')", factoryID).Count(&so)
	response.OK2(c, gin.H{"openDocs": wo + so})
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

// TraceSerial 按 SN 查序列号+关联流水（mes trace 经通道）。
func (a *API) TraceSerial(c *gin.Context) {
	tenantID := queryInt64(c, "tenantId")
	var sn model.SerialNumber
	if err := a.DB.Where("tenant_id=? AND serial=?", tenantID, c.Query("sn")).First(&sn).Error; err != nil {
		response.OK2(c, gin.H{"sn": nil})
		return
	}
	var moves []model.StockMovement
	a.DB.Raw("SELECT * FROM wms.stock_movements WHERE batch_no=? ORDER BY id", sn.BatchNo).Scan(&moves)
	response.OK2(c, gin.H{"sn": sn, "moves": moves})
}

// TraceBatch 按批次查流水。
func (a *API) TraceBatch(c *gin.Context) {
	tenantID := queryInt64(c, "tenantId")
	var moves []model.StockMovement
	a.DB.Raw("SELECT * FROM wms.stock_movements WHERE tenant_id=? AND batch_no=? ORDER BY id", tenantID, c.Query("no")).Scan(&moves)
	response.OK2(c, gin.H{"moves": moves})
}

// ErpMatOpenCount 物料未结销售+采购单据数（material-check 经通道；合计返回）。
func (a *API) ErpMatOpenCount(c *gin.Context) {
	tenantID, matID := queryInt64(c, "tenantId"), queryInt64(c, "materialId")
	var so, po int64
	a.DB.Raw(`SELECT COUNT(*) FROM erp.sales_order_lines l JOIN erp.sales_orders o ON o.id=l.sales_order_id
		WHERE l.material_id=? AND o.tenant_id=? AND o.status IN ('draft','audited','partial') AND o.deleted_at IS NULL`, matID, tenantID).Scan(&so)
	a.DB.Raw(`SELECT COUNT(*) FROM erp.purchase_order_lines l JOIN erp.purchase_orders o ON o.id=l.purchase_order_id
		WHERE l.material_id=? AND o.tenant_id=? AND o.status IN ('draft','arriving','receiving') AND o.deleted_at IS NULL`, matID, tenantID).Scan(&po)
	response.OK2(c, gin.H{"n": so + po})
}

// ErpDemand 物料在手需求（已审未发；ATP 经通道）。
func (a *API) ErpDemand(c *gin.Context) {
	tenantID, matID := queryInt64(c, "tenantId"), queryInt64(c, "materialId")
	var q float64
	a.DB.Raw(`SELECT COALESCE(SUM(l.qty-l.shipipped_qty),0) FROM erp.sales_order_lines l
		JOIN erp.sales_orders o ON o.id=l.sales_order_id AND o.status IN ('audited','partial')
		WHERE l.material_id=? AND o.tenant_id=?`, matID, tenantID).Scan(&q)
	response.OK2(c, gin.H{"qty": q})
}

// ErpInTransit 物料在途（已审未入库采购；ATP 经通道）。
func (a *API) ErpInTransit(c *gin.Context) {
	tenantID, factoryID, matID := queryInt64(c, "tenantId"), queryInt64(c, "factoryId"), queryInt64(c, "materialId")
	var out struct {
		Qty  float64 `json:"qty"`
	}
	a.DB.Raw(`SELECT COALESCE(SUM(l.qty-l.received_qty),0) qty FROM erp.purchase_order_lines l
		JOIN erp.purchase_orders po ON po.id=l.purchase_order_id
		WHERE po.tenant_id=? AND po.factory_id=? AND l.material_id=? AND po.status IN ('arriving','receiving')`,
		tenantID, factoryID, matID).Scan(&out)
	response.OK2(c, gin.H{"qty": out.Qty})
}
