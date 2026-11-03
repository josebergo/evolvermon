package api

// M3 生产计划域：ATP 可承诺量、有限产能排产、甘特数据、派工看板（FR-ERP/PRD M3）。

import (
	"encoding/json"
	"fmt"
	"os"
	"net/http"
	"evolvermon/backend/internal/client"
	"evolvermon/backend/internal/pkg/conv"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"evolvermon/backend/internal/data"
	"evolvermon/backend/internal/middleware"
	"evolvermon/backend/internal/model"
	"evolvermon/backend/internal/pkg/response"
)

type PlanAPI struct{ DB *gorm.DB }

// Atp 可承诺量（FR M3）：可用 = 现有 −预占 + 在途 + 在制 − 其他在手需求。
func (p *PlanAPI) Atp(c *gin.Context) {
	ac := middleware.GetAuth(c)
	matID := c.Query("material_id")
	if matID == "" {
		response.Fail(c, response.NewBiz(response.ErrParam, "material_id 必填"))
		return
	}
	qty := 0.0
	if v := c.Query("qty"); v != "" {
		qty, _ = strconv.ParseFloat(v, 64)
	}
	mid, _ := strconv.ParseInt(matID, 10, 64)
	type agg struct{ Q float64 }
	var a agg
	if q, err := client.WMS().StockSingle(c.Request.Context(), ac.TenantID, ac.FactoryID, mid); err == nil {
		a.Q = q
	}
	onHand := a.Q
	a.Q = 0
	if sums, err := client.WMS().ReservedSums(c.Request.Context(), ac.TenantID, ac.FactoryID); err == nil {
		for _, r := range sums {
			if r.MaterialID == mid {
				a.Q = r.Qty
			}
		}
	}
	held := a.Q
	// 在途：已审未入库采购（取最早交期）
	var inTransit struct {
		Qty  float64
		Date *time.Time
	}
	inTransit.Qty = 0
	if erpURL := os.Getenv("EM_ERP_URL"); erpURL != "" {
		q3, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/core/erp/intransit?tenantId=%d&factoryId=%d&materialId=%d", erpURL, ac.TenantID, ac.FactoryID, mid), nil)
		q3.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(q3); err == nil {
			var out3 struct {
				Data struct {
					Qty float64 `json:"qty"`
				} `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out3)
			resp.Body.Close()
			inTransit.Qty = out3.Data.Qty
		}
	} else {
	p.DB.Raw(`SELECT COALESCE(SUM(l.qty-l.received_qty),0) qty, MIN(l.delivery_date) date
		FROM erp.purchase_order_lines l JOIN erp.purchase_orders po ON po.id=l.purchase_order_id
		WHERE po.tenant_id=? AND po.factory_id=? AND l.material_id=? AND po.status IN ('arriving','receiving')`,
		ac.TenantID, ac.FactoryID, matID).Scan(&inTransit)
	}
	// 在制：未完工工单剩余产出
	p.DB.Model(&model.WorkOrder{}).Select("COALESCE(SUM(qty-good_qty),0) q").
		Where("tenant_id=? AND factory_id=? AND material_id=? AND status IN ('released','running','paused')",
			ac.TenantID, ac.FactoryID, matID).Scan(&a)
	wip := a.Q
	// 其他在手需求（其他已审核订单未发量）
	demand := 0.0
	if erpURL := os.Getenv("EM_ERP_URL"); erpURL != "" {
		q, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/core/erp/demand?tenantId=%d&materialId=%d", erpURL, ac.TenantID, mid), nil)
		q.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(q); err == nil {
			var out struct {
				Data struct {
					Qty float64 `json:"qty"`
				} `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			demand = out.Data.Qty
		}
	} else {
	a.Q = 0
	p.DB.Model(&model.SalesOrderLine{}).
		Joins("JOIN erp.sales_orders so ON so.id = sales_order_lines.sales_order_id AND so.status IN ('audited','partial')").
		Select("COALESCE(SUM(sales_order_lines.qty - sales_order_lines.shipped_qty),0) q").
		Where("sales_order_lines.material_id = ?", matID).Scan(&a)
		demand = a.Q
	}

	available := onHand - held + inTransit.Qty + wip - demand
	earliest := time.Now()
	if onHand-held >= qty {
		earliest = time.Now() // 现货
	} else if inTransit.Date != nil && inTransit.Date.Before(earliest) == false {
		earliest = *inTransit.Date
	}
	mat, _ := client.MatRef(c.Request.Context(), ac.TenantID, mid)
	feasible := qty == 0 || available >= qty
	if !feasible && mat.ID != 0 {
		earliest = time.Now().AddDate(0, 0, mat.LeadTimeDays)
	}
	response.OK2(c, gin.H{
		"materialId": matID, "qty": qty,
		"onHand": onHand, "reserved": held, "inTransit": inTransit.Qty, "wip": wip,
		"otherDemand": demand, "available": math.Max(available, 0),
		"feasible": feasible, "earliestDate": earliest.Format("2006-01-02"),
	})
}

// Schedule 有限产能前推排产：按优先级+交期排序，工作中心按日产能分配时间桶。
func (p *PlanAPI) Schedule(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var wos []model.WorkOrder
	p.DB.Scopes(data.ScopeFor(ac, true)).Preload("Ops", "status IN ('waiting','assigned','running','done')").
		Where("status IN ('released','running')").Find(&wos)
	if len(wos) == 0 {
		response.OK2(c, gin.H{"scheduled": 0})
		return
	}
	sort.Slice(wos, func(i, j int) bool {
		if wos[i].Priority != wos[j].Priority {
			return wos[i].Priority < wos[j].Priority
		}
		return woDue(wos[i]).Before(woDue(wos[j]))
	})
	wcs, werr := client.M().WorkCenters(c.Request.Context(), ac.TenantID)
	if werr != nil {
		response.Fail(c, werr)
		return
	}
	capPerDay := map[int64]float64{}
	for _, w := range wcs {
		capPerDay[w.ID] = w.CapacityPerDay
		if capPerDay[w.ID] <= 0 {
			capPerDay[w.ID] = 480 // 默认 8h×60 件兜底
		}
	}
	// 时间桶：wcID → 日期 → 剩余产能（件）
	buckets := map[int64]map[string]float64{}
	remCap := func(wc int64, day time.Time) float64 {
		k := day.Format("2006-01-02")
		if buckets[wc] == nil {
			buckets[wc] = map[string]float64{}
		}
		if _, ok := buckets[wc][k]; !ok {
			buckets[wc][k] = capPerDay[wc]
		}
		return buckets[wc][k]
	}
	takeCap := func(wc int64, day time.Time, need float64) float64 {
		k := day.Format("2006-01-02")
		remCap(wc, day)
		take := math.Min(buckets[wc][k], need)
		buckets[wc][k] -= take
		return take
	}

	now := time.Now()
	scheduled := 0
	for _, wo := range wos {
		cur := now
		ops := wo.Ops
		sort.Slice(ops, func(i, j int) bool { return ops[i].Seq < ops[j].Seq })
		woStart, woEnd := (*time.Time)(nil), (*time.Time)(nil)
		for _, op := range ops {
			remaining := math.Max(wo.Qty-wo.GoodQty, 0)
			if remaining <= 0 && op.QtyOk+op.QtyDefect <= 0 {
				continue // 已完成且无剩余
			}
			need := math.Max(remaining, 1)
			day := truncateDay(cur)
			var start, end time.Time
			allocated := 0.0
			for d := 0; d < 180; d++ {
				r := remCap(op.WorkCenterID, day)
				if r > 0 {
					if allocated == 0 {
						start = day
					}
					got := takeCap(op.WorkCenterID, day, need-allocated)
					allocated += got
					end = day.Add(23 * time.Hour)
				}
				if allocated >= need-1e-9 {
					break
				}
				day = day.AddDate(0, 0, 1)
			}
			if allocated > 0 {
				if woStart == nil || start.Before(*woStart) {
					woStart = &start
				}
				if woEnd == nil || end.After(*woEnd) {
					woEnd = &end
				}
				p.DB.Model(&model.WoOperation{}).Where("id=?", op.ID).
					Updates(map[string]any{"plan_start_at": &start, "plan_end_at": &end})
			}
			if woEnd != nil {
				cur = woEnd.Add(time.Hour) // 后道工序不早于前道完成
			}
		}
		if woStart != nil {
			p.DB.Model(&model.WorkOrder{}).Where("id=?", wo.ID).
				Updates(map[string]any{"plan_start_at": woStart, "plan_end_at": woEnd})
			scheduled++
		}
	}
	data.Audit(p.DB, ac, c.ClientIP(), "schedule", "plan", "", "", "", conv.I64s(int64(scheduled)))
	response.OK2(c, gin.H{"scheduled": scheduled})
}

// Gantt 甘特数据：按工作中心分组。
func (p *PlanAPI) Gantt(c *gin.Context) {
	ac := middleware.GetAuth(c)
	wcs, werr := client.M().WorkCenters(c.Request.Context(), ac.TenantID)
	if werr != nil {
		response.Fail(c, werr)
		return
	}
	var wos []model.WorkOrder
	p.DB.Scopes(data.ScopeFor(ac, true)).Where("status IN ('released','running')").Find(&wos)
	woBy := map[int64]model.WorkOrder{}
	for _, w := range wos {
		woBy[w.ID] = w
	}
	type bar struct {
		WoNo      string  `json:"woNo"`
		Seq       int     `json:"seq"`
		Name      string  `json:"name"`
		Start     string  `json:"start"`
		End       string  `json:"end"`
		Priority  int     `json:"priority"`
		PlanEnd   string  `json:"planEnd"`
		Overdue   bool    `json:"overdue"`
	}
	out := []gin.H{}
	now := time.Now()
	for _, wc := range wcs {
		var ops []model.WoOperation
		p.DB.Where("work_center_id=?", wc.ID).Find(&ops)
		bars := []bar{}
		for _, op := range ops {
			wo, ok := woBy[op.WorkOrderID]
			if !ok || op.PlanStartAt == nil {
				continue
			}
			bars = append(bars, bar{
				WoNo: wo.DocNo, Seq: op.Seq, Name: op.Name,
				Start: op.PlanStartAt.Format("2006-01-02"), End: op.PlanEndAt.Format("2006-01-02"),
				Priority: wo.Priority, PlanEnd: conv.Ts(wo.PlanEndAt),
				Overdue: wo.PlanEndAt != nil && wo.PlanEndAt.Before(now),
			})
		}
		sort.Slice(bars, func(i, j int) bool {
			if bars[i].Start != bars[j].Start {
				return bars[i].Start < bars[j].Start
			}
			return bars[i].Priority < bars[j].Priority
		})
		out = append(out, gin.H{"workCenterId": wc.ID, "workCenter": wc.Name, "capacityPerDay": wc.CapacityPerDay, "bars": bars})
	}
	response.OK2(c, out)
}

// DispatchBoard 派工看板（FR-MES-023）：工作中心聚合待派/待开工。
func (p *PlanAPI) DispatchBoard(c *gin.Context) {
	ac := middleware.GetAuth(c)
	type row struct {
		WorkCenterID int64   `json:"workCenterId"`
		WorkCenter   string  `json:"workCenter"`
		Waiting      int64   `json:"waiting"`
		Assigned     int64   `json:"assigned"`
		Running      int64   `json:"running"`
		DoneToday    float64 `json:"doneToday"`
	}
	// master 分库拆解：工作中心走通道；工序 core 直查（Go 合并）
	wcs, werr := client.M().WorkCenters(c.Request.Context(), ac.TenantID)
	if werr != nil {
		response.Fail(c, werr)
		return
	}
	var opRows []struct {
		WorkCenterID int64
		Status       string
		QtyOk        float64
	}
	opRows = wmsOpsFallback(p.DB, ac, opRows)
	type agg struct {
		waiting, assigned, running int64
		doneToday                  float64
	}
	bywc := map[int64]*agg{}
	for _, o := range opRows {
		a := bywc[o.WorkCenterID]
		if a == nil {
			a = &agg{}
			bywc[o.WorkCenterID] = a
		}
		switch o.Status {
		case "waiting":
			a.waiting++
		case "assigned":
			a.assigned++
		case "running":
			a.running++
		}
		a.doneToday += o.QtyOk
	}
	var rows []row
	for _, wc := range wcs {
		if wc.FactoryID == nil || *wc.FactoryID != ac.FactoryID || wc.Status != 1 {
			continue
		}
		a := bywc[wc.ID]
		if a == nil {
			a = &agg{}
		}
		rows = append(rows, row{WorkCenterID: wc.ID, WorkCenter: wc.Name,
			Waiting: a.waiting, Assigned: a.assigned, Running: a.running, DoneToday: a.doneToday})
	}
	response.OK2(c, rows)
}

func woDue(w model.WorkOrder) time.Time {
	if w.PlanEndAt != nil {
		return *w.PlanEndAt
	}
	return time.Now().AddDate(0, 0, 7)
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// wmsOpsFallback DispatchBoard 工序读（core-mes 自域直查；其它进程库无 mes 表时空结果兜底）。
func wmsOpsFallback(db *gorm.DB, ac *middleware.AuthCtx, cur []struct {
	WorkCenterID int64
	Status       string
	QtyOk        float64
}) []struct {
	WorkCenterID int64
	Status       string
	QtyOk        float64
} {
	if os.Getenv("EM_MES_URL") == "" {
		db.Raw(`SELECT work_center_id, status, COALESCE(qty_ok,0) qty_ok FROM mes.wo_operations WHERE tenant_id=? AND factory_id=?`,
			ac.TenantID, ac.FactoryID).Scan(&cur)
	}
	return cur
}
