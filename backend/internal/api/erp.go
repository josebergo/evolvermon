package api

// ERP 交易域：销售订单状态机/发货、请购转采购、采购到货（FR-ERP-050~068）。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type ErpAPI struct{ DB *gorm.DB }

// ---- 销售订单 ----

func (e *ErpAPI) AuditSalesOrder(c *gin.Context) {
	ac := middleware.GetAuth(c)
	so, err := e.loadSO(c, ac)
	if err != nil {
		return
	}
	if so.Status != model.StDraft {
		response.Fail(c, response.NewBiz(response.ErrBadState, "仅草稿可审核"))
		return
	}
	// 校验客户与物料有效（FR-ERP-052）
	var cust model.Customer
	if err := e.DB.Scopes(data.ScopeFor(ac, false)).First(&cust, so.CustomerID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "客户不存在或已停用"))
		return
	}
	for _, l := range so.Lines {
		if l.Qty <= 0 {
			response.Fail(c, response.NewBiz(response.ErrBiz, "行数量必须大于 0"))
			return
		}
		var m model.Material
		if err := e.DB.Scopes(data.ScopeFor(ac, false)).First(&m, l.MaterialID).Error; err != nil || m.Status != 1 {
			response.Fail(c, response.NewBiz(response.ErrBiz, "物料无效："+i64s(l.MaterialID)))
			return
		}
	}
	// 审核即软预占（FR：可用量口径 = 现有 − 预占，PRD OQ-1 假设已参数化为默认软预占）
	now := time.Now()
	err = e.DB.Transaction(func(tx *gorm.DB) error {
		for _, l := range so.Lines {
			r := model.StockReservation{
				TenantID: ac.TenantID, FactoryID: ac.FactoryID, MaterialID: l.MaterialID,
				Qty: l.Qty, BizType: "sales", SourceDocType: "sales_order", SourceDocID: so.ID, SourceLineID: l.ID,
			}
			if err := tx.Create(&r).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.SalesOrder{}).Where("id=? AND version=?", so.ID, so.Version).
			Updates(map[string]any{"status": model.StAudited, "audited_by": ac.UserID, "audited_at": now, "version": so.Version + 1}).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(e.DB, ac, c.ClientIP(), "audit", "sales_order", so.DocNo, so.DocNo, string(so.Status), string(model.StAudited))
	events.Publish(e.DB, ac.TenantID, "salesorder.audited", gin.H{"docNo": so.DocNo, "amount": so.Amount})
	response.OK2(c, gin.H{"ok": true})
}

func (e *ErpAPI) VoidSalesOrder(c *gin.Context) {
	ac := middleware.GetAuth(c)
	so, err := e.loadSO(c, ac)
	if err != nil {
		return
	}
	if so.Status == model.StDone || so.Status == model.StVoid {
		response.Fail(c, response.NewBiz(response.ErrBadState, "当前状态不可作废"))
		return
	}
	err = e.DB.Transaction(func(tx *gorm.DB) error {
		// 释放未核销预占
		if err := tx.Model(&model.StockReservation{}).
			Where("source_doc_type='sales_order' AND source_doc_id=? AND status='held'", so.ID).
			Updates(map[string]any{"status": model.RvReleased, "released_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Model(&model.SalesOrder{}).Where("id=? AND version=?", so.ID, so.Version).
			Updates(map[string]any{"status": model.StVoid, "version": so.Version + 1}).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(e.DB, ac, c.ClientIP(), "void", "sales_order", so.DocNo, so.DocNo, "", "")
	response.OK2(c, gin.H{"ok": true})
}

// CreateShipment FR-ERP-054：参照订单生成发货出库草稿。
func (e *ErpAPI) CreateShipment(c *gin.Context) {
	ac := middleware.GetAuth(c)
	so, err := e.loadSO(c, ac)
	if err != nil {
		return
	}
	if so.Status != model.StAudited && so.Status != model.StPartial {
		response.Fail(c, response.NewBiz(response.ErrBadState, "订单未审核或已完成"))
		return
	}
	// 成品仓
	var wh model.Warehouse
	if err := e.DB.Scopes(data.ScopeFor(ac, true)).Where("type='finished' AND status=1").First(&wh).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "未配置成品仓"))
		return
	}
	docNo, err := data.NextDocNo(e.DB, ac.TenantID, "SHP")
	if err != nil {
		response.Fail(c, err)
		return
	}
	order := model.StockOrder{
		DocType: model.DocSHP, WarehouseID: wh.ID, SourceType: "sales",
		SourceDocType: "sales_order", SourceDocID: so.ID, SourceDocNo: so.DocNo,
	}
	order.TenantID = ac.TenantID
	order.FactoryID = &ac.FactoryID
	order.DocNo = docNo
	err = e.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		for i, l := range so.Lines {
			remain := l.Qty - l.ShippedQty
			if remain <= 0 {
				continue
			}
			line := model.StockOrderLine{
				StockOrderID: order.ID, LineNo: i + 1, MaterialID: l.MaterialID,
				Qty: remain,
			}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(e.DB, ac, c.ClientIP(), "create-shipment", "stock_order", order.DocNo, "销售发货 " + so.DocNo, "", "")
	response.OK2(c, order)
}

// ---- 请购 ----

// ConvertPRToPO FR-ERP-063：请购审核后转采购订单（按供应商拆分由前端多次调用）。
func (e *ErpAPI) ConvertPRToPO(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		IDs        []int64 `json:"ids" binding:"required"`
		SupplierID int64   `json:"supplierId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var prs []model.PurchaseRequest
	if err := e.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").Find(&prs, req.IDs).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "请购单不存在"))
		return
	}
	var supp model.Supplier
	if err := e.DB.Scopes(data.ScopeFor(ac, false)).First(&supp, req.SupplierID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "供应商不存在"))
		return
	}
	docNo, _ := data.NextDocNo(e.DB, ac.TenantID, "PO")
	po := model.PurchaseOrder{
		SupplierID: req.SupplierID, OrderDate: ptrTime(time.Now()), Status: model.StDraft,
	}
	po.TenantID = ac.TenantID
	po.FactoryID = &ac.FactoryID
	po.DocNo = docNo
	var lines []model.PurchaseOrderLine
	err := e.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&po).Error; err != nil {
			return err
		}
		no := 0
		for _, pr := range prs {
			for _, l := range pr.Lines {
				remain := l.Qty - l.PurchasedQty
				if remain <= 0 {
					continue
				}
				no++
				var mat model.Material
				tx.Scopes(data.ScopeFor(ac, false)).First(&mat, l.MaterialID)
				lines = append(lines, model.PurchaseOrderLine{
					PurchaseOrderID: po.ID, LineNo: no, MaterialID: l.MaterialID, Qty: remain, Price: mat.Price,
				})
				if err := tx.Model(&model.PurchaseRequestLine{}).Where("id=?", l.ID).
					Update("purchased_qty", gorm.Expr("purchased_qty+?", remain)).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&model.PurchaseRequest{}).Where("id=?", pr.ID).
				Update("status", model.StConverted).Error; err != nil {
				return err
			}
		}
		for i := range lines {
			if err := tx.Create(&lines[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"poDocNo": po.DocNo, "poId": po.ID})
}

// AuditPO PO 审核（与销售类似，简版）。
func (e *ErpAPI) AuditPO(c *gin.Context) {
	ac := middleware.GetAuth(c)
	po, ok := e.loadPO(c, ac)
	if !ok {
		return
	}
	if po.Status != model.StDraft {
		response.Fail(c, response.NewBiz(response.ErrBadState, "仅草稿可审核"))
		return
	}
	e.DB.Model(&model.PurchaseOrder{}).Where("id=? AND version=?", po.ID, po.Version).
		Updates(map[string]any{"status": model.StArriving, "audited_by": ac.UserID, "audited_at": time.Now(), "version": po.Version + 1})
	data.Audit(e.DB, ac, c.ClientIP(), "audit", "purchase_order", po.DocNo, po.DocNo, "", "")
	response.OK2(c, gin.H{"ok": true})
}

// ArrivePO FR-ERP-064：到货通知 → 生成采购入库草稿（RCV）。
func (e *ErpAPI) ArrivePO(c *gin.Context) {
	ac := middleware.GetAuth(c)
	po, ok := e.loadPO(c, ac)
	if !ok {
		return
	}
	if po.Status != model.StArriving && po.Status != model.StReceiving {
		response.Fail(c, response.NewBiz(response.ErrBadState, "订单未审核或已完结"))
		return
	}
	var wh model.Warehouse
	if err := e.DB.Scopes(data.ScopeFor(ac, true)).Where("type='raw' AND status=1").First(&wh).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "未配置原料仓"))
		return
	}
	docNo, _ := data.NextDocNo(e.DB, ac.TenantID, "RCV")
	order := model.StockOrder{
		DocType: model.DocRCV, WarehouseID: wh.ID, SourceType: "purchase",
		SourceDocType: "purchase_order", SourceDocID: po.ID, SourceDocNo: po.DocNo,
	}
	order.TenantID = ac.TenantID
	order.FactoryID = &ac.FactoryID
	order.DocNo = docNo
	err := e.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		for i, l := range po.Lines {
			remain := l.Qty - l.ReceivedQty
			if remain <= 0 {
				continue
			}
			var mat model.Material
			tx.Scopes(data.ScopeFor(ac, false)).First(&mat, l.MaterialID)
			if err := tx.Create(&model.StockOrderLine{
				StockOrderID: order.ID, LineNo: i + 1, MaterialID: l.MaterialID, Qty: remain, Price: l.Price,
			}).Error; err != nil {
				return err
			}
			_ = mat
		}
		return tx.Model(&model.PurchaseOrder{}).Where("id=?", po.ID).Update("status", model.StReceiving).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, order)
}

// ---- helpers ----

func (e *ErpAPI) loadSO(c *gin.Context, ac *middleware.AuthCtx) (*model.SalesOrder, error) {
	var so model.SalesOrder
	if err := e.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").First(&so, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "订单不存在"))
		return nil, err
	}
	return &so, nil
}

func (e *ErpAPI) loadPO(c *gin.Context, ac *middleware.AuthCtx) (*model.PurchaseOrder, bool) {
	var po model.PurchaseOrder
	if err := e.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").First(&po, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "订单不存在"))
		return nil, false
	}
	return &po, true
}

func ptrTime(t time.Time) *time.Time { return &t }
