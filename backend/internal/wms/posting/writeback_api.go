// writeBack 跨域回写通道（Phase 3 ③B-③）：wms 过账事务内回写 erp（SO/PO）与 mes（WO/QC/成本）。
// Local（共享库）= 保留原同事务 SQL（零行为变化）；Remote（拆库后）= HTTP 端点（调用失败则整个过账回滚）。
package posting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"gorm.io/gorm"

	"evolvermon/backend/internal/middleware"
	"evolvermon/backend/internal/model"
)

// WBLine 回写行（三库：携带 materialId，端点进程无 wms 表无法回查行）。
type WBLine struct {
	LineID     int64   `json:"lineId"`
	Qty        float64 `json:"qty"`
	MaterialID int64   `json:"materialId"`
}

// WBReq 回写请求。
type WBReq struct {
	TenantID  int64    `json:"tenantId"`
	FactoryID int64    `json:"factoryId"`
	UserID    int64    `json:"userId"`
	Source    string   `json:"source"` // sales|purchase|picking|return|production
	SourceDocID int64  `json:"sourceDocId"`
	OrderID   int64    `json:"orderId"`
	DocNo     string   `json:"docNo"`
	Lines     []WBLine `json:"lines"`
}

// RemoteWriteBack 远程回写钩子（未设=Local 同事务 SQL；设=HTTP 到 core-erp/mes 的回写端点）。
var RemoteWriteBack func(base string, token string, req WBReq) error

func httpWB(base, token string, req WBReq) error {
	if RemoteWriteBack == nil {
		return nil
	}
	return RemoteWriteBack(base, token, req)
}

// HTTPWriteBack 默认 HTTP 实现（wire 装配 RemoteWriteBack 用）。
func HTTPWriteBack(base, token string, req WBReq) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	q, err := http.NewRequest(http.MethodPost, base+"/internal/core/write-back", bytes.NewReader(body))
	if err != nil {
		return err
	}
	q.Header.Set("Content-Type", "application/json")
	q.Header.Set("X-Internal-Token", token)
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(q)
	if err != nil {
		return fmt.Errorf("write-back %s: %w", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("write-back %s: http %d", base, resp.StatusCode)
	}
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Code != 0 {
		return fmt.Errorf("write-back %s: %s", base, out.Message)
	}
	return nil
}

// wbReq 构造回写请求。
func wbReq(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []PostLine) WBReq {
	wbl := make([]WBLine, 0, len(lines))
	for _, l := range lines {
		wbl = append(wbl, WBLine{LineID: l.LineID, Qty: l.Qty, MaterialID: l.MaterialID})
	}
	return WBReq{TenantID: ac.TenantID, FactoryID: ac.FactoryID, UserID: ac.UserID,
		Source: order.SourceType, SourceDocID: order.SourceDocID, OrderID: order.ID, DocNo: order.DocNo, Lines: wbl}
}

// ErpWriteBack erp 段（Remote 模式）：核销预占+SO/PO 状态。
func ErpWriteBack(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []PostLine, runLocal func()) error {
	if os.Getenv("EM_ERP_URL") == "" {
		runLocal()
		return nil
	}
	wbl := make([]WBLine, 0, len(lines))
	for _, l := range lines {
		wbl = append(wbl, WBLine{LineID: l.LineID, Qty: l.Qty, MaterialID: l.MaterialID})
	}
	return httpWB(os.Getenv("EM_ERP_URL"), os.Getenv("EM_INTERNAL_TOKEN"), WBReq{
		TenantID: ac.TenantID, FactoryID: ac.FactoryID, UserID: ac.UserID,
		Source: order.SourceType, SourceDocID: order.SourceDocID, OrderID: order.ID, DocNo: order.DocNo, Lines: wbl,
	})
}

// MesWriteBack mes 段（Remote 模式）：WO picked/stored + AutoQc + 成本。
func MesWriteBack(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []PostLine, runLocal func()) error {
	if os.Getenv("EM_MES_URL") == "" {
		runLocal()
		return nil
	}
	wbl := make([]WBLine, 0, len(lines))
	for _, l := range lines {
		wbl = append(wbl, WBLine{LineID: l.LineID, Qty: l.Qty, MaterialID: l.MaterialID})
	}
	return httpWB(os.Getenv("EM_MES_URL"), os.Getenv("EM_INTERNAL_TOKEN"), WBReq{
		TenantID: ac.TenantID, FactoryID: ac.FactoryID, UserID: ac.UserID,
		Source: order.SourceType, SourceDocID: order.SourceDocID, OrderID: order.ID, DocNo: order.DocNo, Lines: wbl,
	})
}

var _ = bytes.MinRead
var _ = json.Marshal
var _ = fmt.Sprintf
var _ = http.MethodPost
var _ = time.Now
var _ = gorm.Expr

// WriteBackLocal 回写端点实现（core 进程：与 WriteBack 原各 case 的跨域 SQL 等价）。
// 幂等：行级增量更新天然可重放（qty += / GREATEST 保护）。
func WriteBackLocal(db *gorm.DB, req WBReq) error {
	ac := &middleware.AuthCtx{TenantID: req.TenantID, FactoryID: req.FactoryID, UserID: req.UserID}
	lines := make([]PostLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, PostLine{LineID: l.LineID, Qty: l.Qty, MaterialID: l.MaterialID})
	}
	// 端点进程（erp/mes 库）无 wms 单据：order 由请求构造（SN 等 wms 自域段已被 WB_NO_SN 跳过；
	// purchase 的 AutoQc 在 mes 库端点上正常执行——order.WarehouseID/Lines 仅 IQC 判定用，三库下经 has-iqc 通道）
	order := model.StockOrder{ID: req.OrderID, DocNo: req.DocNo, SourceType: req.Source, SourceDocID: req.SourceDocID, Lines: wbOrderLines(req.Lines)}
	// 端点侧强制 Local 执行（清空远程 env 防嵌套转发；WB_NO_SN=1 跳过 wms 自域 SN 段——由 core-wms 调用方执行）
	os.Setenv("WB_NO_SN", "1")
	defer os.Unsetenv("WB_NO_SN")
	e1, e2 := os.Getenv("EM_ERP_URL"), os.Getenv("EM_MES_URL")
	os.Setenv("EM_ERP_URL", "")
	os.Setenv("EM_MES_URL", "")
	defer func() { os.Setenv("EM_ERP_URL", e1); os.Setenv("EM_MES_URL", e2) }()
	return WriteBack(db, ac, order, lines)
}

func wbOrderLines(ls []WBLine) []model.StockOrderLine {
	out := make([]model.StockOrderLine, 0, len(ls))
	for _, l := range ls {
		out = append(out, model.StockOrderLine{ID: l.LineID, MaterialID: l.MaterialID, Qty: l.Qty})
	}
	return out
}
