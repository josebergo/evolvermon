// Package client — core 域间读通道（Phase 3 批2下）：erp 读 mes（工单/工序/计数）、mes 读 erp（订单计数）。
// 三库拆分后为唯一路径；单进程/共享库 = Local 直查。EM_MES_URL/EM_ERP_URL 配置远端。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"gorm.io/gorm"

	"evolvermon/backend/internal/model"
)

type gormWO = model.WorkOrder

// woDocNo 取号钩子（wire 装配 data.NextDocNo，避免 client→data 环）。
var woDocNo func(db *gorm.DB, tenantID int64) (string, error)

// SetWODocNo 注入取号函数。
func SetWODocNo(f func(db *gorm.DB, tenantID int64) (string, error)) { woDocNo = f }

// WOCreateResult 建工单结果。
type WOCreateResult struct {
	WoID  int64  `json:"woId"`
	DocNo string `json:"docNo"`
	Reused bool  `json:"reused"`
}

// WOCreateReq 建工单请求（mrp 转单；幂等键入 Remark）。
type WOCreateReq struct {
	TenantID   int64      `json:"tenantId"`
	FactoryID  int64      `json:"factoryId"`
	UserID     int64      `json:"userId"`
	MaterialID int64      `json:"materialId"`
	Qty        float64    `json:"qty"`
	SourceType string     `json:"sourceType"`
	SourceDocNo string    `json:"sourceDocNo"`
	PlanStartAt interface{} `json:"planStartAt"`
	PlanEndAt   interface{} `json:"planEndAt"`
	IdemKey    string     `json:"idemKey"`
}

// CoreMes mes 域通道。
type CoreMes interface {
	CreateWO(ctx context.Context, req WOCreateReq) (*WOCreateResult, error)
	WorkOrders(ctx context.Context, tenantID, factoryID int64, statuses string) ([]map[string]any, error)
	WoOps(ctx context.Context, tenantID, factoryID int64, statuses string) ([]map[string]any, error)
	WoCounts(ctx context.Context, tenantID, factoryID int64) (active, overdue int64, err error)
	WipSums(ctx context.Context, tenantID, factoryID int64) ([]StockSum, error)
}

// CoreErp erp 域读通道。
type CoreErp interface {
	OrderCounts(ctx context.Context, tenantID, factoryID int64) (salesPending, poArriving, poOverdue int64, err error)
}

type localMes struct{ db *gorm.DB }

func (l *localMes) CreateWO(_ context.Context, req WOCreateReq) (*WOCreateResult, error) {
	idem := ""
	if req.IdemKey != "" {
		idem = "idem:" + req.IdemKey
		var exist gormWO
		if err := l.db.Where("tenant_id=? AND remark=?", req.TenantID, idem).First(&exist).Error; err == nil {
			return &WOCreateResult{WoID: exist.ID, DocNo: exist.DocNo, Reused: true}, nil
		}
	}
	if woDocNo == nil {
		return nil, fmt.Errorf("woDocNo not wired")
	}
	docNo, err := woDocNo(l.db, req.TenantID)
	if err != nil {
		return nil, err
	}
	two := gormWO{
		MaterialID: req.MaterialID, Qty: req.Qty, SourceType: req.SourceType,
		SourceDocNo: req.SourceDocNo, Remark: idem,
	}
	two.TenantID = req.TenantID
	two.FactoryID = &req.FactoryID
	two.DocNo = docNo
	if err := l.db.Create(&two).Error; err != nil {
		return nil, err
	}
	return &WOCreateResult{WoID: two.ID, DocNo: two.DocNo}, nil
}

func (l *localMes) WorkOrders(_ context.Context, tenantID, factoryID int64, statuses string) ([]map[string]any, error) {
	q := l.db.Table("mes.work_orders").
		Select("id, tenant_id, factory_id, doc_no, material_id, qty, good_qty, priority, plan_start_at, plan_end_at, status").
		Where("tenant_id=? AND deleted_at IS NULL", tenantID)
	if factoryID > 0 {
		q = q.Where("factory_id=?", factoryID)
	}
	if statuses != "" {
		q = q.Where("status IN ?", splitStatuses(statuses))
	}
	var rows []map[string]any
	return rows, q.Order("id").Limit(2000).Find(&rows).Error
}

func (l *localMes) WoOps(_ context.Context, tenantID, factoryID int64, statuses string) ([]map[string]any, error) {
	q := l.db.Table("mes.wo_operations").
		Select("id, work_order_id, work_center_id, seq, name, status, qty_ok, plan_start_at, plan_end_at, plan_hours").
		Where("tenant_id=?", tenantID)
	if factoryID > 0 {
		q = q.Where("factory_id=?", factoryID)
	}
	if statuses != "" {
		q = q.Where("status IN ?", splitStatuses(statuses))
	}
	var rows []map[string]any
	return rows, q.Order("id").Limit(5000).Find(&rows).Error
}

func (l *localMes) WoCounts(_ context.Context, tenantID, factoryID int64) (int64, int64, error) {
	var a, o int64
	l.db.Raw(`SELECT COUNT(*) FROM mes.work_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('released','running','paused')`, tenantID, factoryID).Scan(&a)
	l.db.Raw(`SELECT COUNT(*) FROM mes.work_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('released','running','paused') AND plan_end_at < now()`, tenantID, factoryID).Scan(&o)
	return a, o, nil
}

func (l *localMes) WipSums(_ context.Context, tenantID, factoryID int64) ([]StockSum, error) {
	var rows []StockSum
	err := l.db.Raw(`SELECT material_id, COALESCE(SUM(qty-good_qty),0) qty FROM mes.work_orders
		WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('released','running','paused','finished') GROUP BY material_id`, tenantID, factoryID).Scan(&rows).Error
	return rows, err
}

type localErp struct{ db *gorm.DB }

func (l *localErp) OrderCounts(_ context.Context, tenantID, factoryID int64) (int64, int64, int64, error) {
	var s, a, o int64
	l.db.Raw(`SELECT COUNT(*) FROM erp.sales_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status='draft'`, tenantID, factoryID).Scan(&s)
	l.db.Raw(`SELECT COUNT(*) FROM erp.purchase_orders WHERE tenant_id=? AND factory_id=? AND deleted_at IS NULL AND status IN ('arriving','receiving')`, tenantID, factoryID).Scan(&a)
	l.db.Raw(`SELECT COUNT(*) FROM erp.purchase_orders po WHERE po.tenant_id=? AND po.factory_id=? AND po.deleted_at IS NULL AND po.status IN ('arriving','receiving')
		AND EXISTS (SELECT 1 FROM erp.purchase_order_lines l WHERE l.purchase_order_id=po.id AND l.delivery_date < now())`, tenantID, factoryID).Scan(&o)
	return s, a, o, nil
}

func splitStatuses(s string) []string {
	var out []string
	cur := ""
	for _, ch := range s {
		if ch == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(ch)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// ---- remote ----

type remoteCore struct {
	base, token string
	hc          *http.Client
}

func (r *remoteCore) CreateWO(ctx context.Context, req WOCreateReq) (*WOCreateResult, error) {
	body, _ := json.Marshal(req)
	q, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+"/internal/core/mes/create-wo", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	q.Header.Set("Content-Type", "application/json")
	q.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(q)
	if err != nil {
		return nil, fmt.Errorf("mes create-wo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("mes create-wo: http %d", resp.StatusCode)
	}
	var out struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    WOCreateResult  `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("mes create-wo: %s", out.Message)
	}
	return &out.Data, nil
}

func (r *remoteCore) list(ctx context.Context, path string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("core %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("core %s: http %d", path, resp.StatusCode)
	}
	var out struct {
		Data struct {
			List []map[string]any `json:"list"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

func (r *remoteCore) nums(ctx context.Context, path string, keys ...string) (map[string]int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("core %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("core %s: http %d", path, resp.StatusCode)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := map[string]int64{}
	for _, k := range keys {
		if v, ok := out.Data[k].(float64); ok {
			res[k] = int64(v)
		}
	}
	return res, nil
}

func (r *remoteCore) WorkOrders(ctx context.Context, tenantID, factoryID int64, statuses string) ([]map[string]any, error) {
	return r.list(ctx, fmt.Sprintf("/internal/mes/wos?tenantId=%d&factoryId=%d&statuses=%s", tenantID, factoryID, statuses))
}

func (r *remoteCore) WoOps(ctx context.Context, tenantID, factoryID int64, statuses string) ([]map[string]any, error) {
	return r.list(ctx, fmt.Sprintf("/internal/mes/wo-ops?tenantId=%d&factoryId=%d&statuses=%s", tenantID, factoryID, statuses))
}

func (r *remoteCore) WoCounts(ctx context.Context, tenantID, factoryID int64) (int64, int64, error) {
	m, err := r.nums(ctx, fmt.Sprintf("/internal/mes/wo-counts?tenantId=%d&factoryId=%d", tenantID, factoryID), "woActive", "woOverdue")
	if err != nil {
		return 0, 0, err
	}
	return m["woActive"], m["woOverdue"], nil
}

func (r *remoteCore) OrderCounts(ctx context.Context, tenantID, factoryID int64) (int64, int64, int64, error) {
	m, err := r.nums(ctx, fmt.Sprintf("/internal/erp/order-counts?tenantId=%d&factoryId=%d", tenantID, factoryID), "salesPending", "poArriving", "poOverdue")
	if err != nil {
		return 0, 0, 0, err
	}
	return m["salesPending"], m["poArriving"], m["poOverdue"], nil
}

func (r *remoteCore) WipSums(ctx context.Context, tenantID, factoryID int64) ([]StockSum, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/internal/mes/wip-sums?tenantId=%d&factoryId=%d", r.base, tenantID, factoryID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("core wip-sums: http %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			List []StockSum `json:"list"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

// ---- 装配 ----

var (
	mesOnce sync.Once
	mesCh   CoreMes
	erpOnce sync.Once
	erpCh   CoreErp
)

// InitCoreMes/InitCoreErp 进程启动装配（main 调用；未设远端=Local）。
func InitCoreMes(db *gorm.DB) {
	mesOnce.Do(func() {
		if u := os.Getenv("EM_MES_URL"); u != "" {
			mesCh = &remoteCore{base: u, token: os.Getenv("EM_INTERNAL_TOKEN"), hc: &http.Client{Timeout: 5 * time.Second}}
		} else {
			mesCh = &localMes{db: db}
		}
	})
}

func InitCoreErp(db *gorm.DB) {
	erpOnce.Do(func() {
		if u := os.Getenv("EM_ERP_URL"); u != "" {
			erpCh = &remoteCore{base: u, token: os.Getenv("EM_INTERNAL_TOKEN"), hc: &http.Client{Timeout: 5 * time.Second}}
		} else {
			erpCh = &localErp{db: db}
		}
	})
}

// MES/ERP 默认通道（须先 Init）。
func MES() CoreMes {
	mesOnce.Do(func() { mesCh = &localMes{} })
	return mesCh
}

func ERP() CoreErp {
	erpOnce.Do(func() { erpCh = &localErp{} })
	return erpCh
}
