package api

// WMS：库存过账（行级锁防超卖，core-flows.md §3）、可用量、流水、盘点（FR-WMS 全域）。

import (
	messvc "github.com/evolvermon/evolvermon/internal/mes/svc"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/wms/posting"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type WmsAPI struct{ DB *gorm.DB }

// ---- 查询 ----

// Stocks FR-WMS-002 台账（按维度聚合过滤）。
func (w *WmsAPI) Stocks(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := w.DB.Scopes(data.ScopeFor(ac, false)).Model(&model.Stock{}).Where("factory_id=?", ac.FactoryID)
	for col, param := range map[string]string{
		"warehouse_id": "warehouse_id", "location_id": "location_id", "material_id": "material_id",
	} {
		if v := c.Query(param); v != "" {
			q = q.Where(col+" = ?", v)
		}
	}
	if v := c.Query("batch_no"); v != "" {
		q = q.Where("batch_no = ?", v)
	}
	if c.Query("positive") == "1" {
		q = q.Where("qty_on_hand > 0")
	}
	var list []model.Stock
	var total int64
	q.Count(&total)
	q.Order("updated_at DESC").Limit(500).Find(&list)
	response.Page(c, list, total)
}

// Available FR-WMS-004 可用量统一口径：现有 − 预占（held）。
func (w *WmsAPI) Available(c *gin.Context) {
	ac := middleware.GetAuth(c)
	ids := c.Query("material_ids")
	cond := ""
	if ids != "" {
		cond = " AND material_id IN (" + ids + ")"
	}
	type stockRow struct {
		MaterialID int64
		OnHand     float64
	}
	var stockRows []stockRow
	w.DB.Model(&model.Stock{}).
		Select("material_id, SUM(qty_on_hand) on_hand").
		Where("tenant_id=? AND factory_id=?"+cond, ac.TenantID, ac.FactoryID).
		Group("material_id").Scan(&stockRows)
	var heldRows []stockRow
	w.DB.Model(&model.StockReservation{}).
		Select("material_id, SUM(qty) on_hand").
		Where("tenant_id=? AND factory_id=? AND status='held'"+cond, ac.TenantID, ac.FactoryID).
		Group("material_id").Scan(&heldRows)
	heldMap := map[int64]float64{}
	for _, r := range heldRows {
		heldMap[r.MaterialID] = r.OnHand
	}
	type outRow struct {
		MaterialID int64   `json:"materialId"`
		OnHand     float64 `json:"onHand"`
		Reserved   float64 `json:"reserved"`
		Available  float64 `json:"available"`
	}
	var rows []outRow
	for _, r := range stockRows {
		res := heldMap[r.MaterialID]
		rows = append(rows, outRow{r.MaterialID, r.OnHand, res, r.OnHand - res})
	}
	response.OK2(c, rows)
}

// Movements FR-WMS-003 流水穿透。
func (w *WmsAPI) Movements(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := w.DB.Scopes(data.ScopeFor(ac, false)).Model(&model.StockMovement{}).Where("factory_id=?", ac.FactoryID)
	if v := c.Query("material_id"); v != "" {
		q = q.Where("material_id=?", v)
	}
	if v := c.Query("source_doc_id"); v != "" {
		q = q.Where("source_doc_id=?", v)
	}
	if v := c.Query("batch_no"); v != "" {
		q = q.Where("batch_no=?", v)
	}
	var list []model.StockMovement
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(500).Find(&list)
	response.Page(c, list, total)
}

// Reservations 预占查询/释放。
func (w *WmsAPI) Reservations(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := w.DB.Scopes(data.ScopeFor(ac, false)).Model(&model.StockReservation{}).Where("factory_id=?", ac.FactoryID)
	if v := c.Query("status"); v != "" {
		q = q.Where("status=?", v)
	}
	var list []model.StockReservation
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(500).Find(&list)
	response.Page(c, list, total)
}

func (w *WmsAPI) ReleaseReservation(c *gin.Context) {
	ac := middleware.GetAuth(c)
	if err := w.DB.Model(&model.StockReservation{}).
		Where("id=? AND tenant_id=? AND status='held'", c.Param("id"), ac.TenantID).
		Updates(map[string]any{"status": model.RvReleased, "released_at": time.Now()}).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// ---- 过账核心 ----

// svcLines 过账行转质检服务入参（wms→mes 边界 DTO，Phase 3 API 化）。
func svcLines(lines []postLineReq) []messvc.PostLine {
	out := make([]messvc.PostLine, len(lines))
	for i, l := range lines {
		out[i] = messvc.PostLine{LineID: l.LineID, Qty: l.Qty}
	}
	return out
}

type postLineReq = posting.PostLine


// PostLines 分次执行过账（FR-WMS-022/032/033）：行级锁防超卖。
func (w *WmsAPI) PostLines(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		Lines []postLineReq `json:"lines" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var order model.StockOrder
	if err := w.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").First(&order, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "单据不存在"))
		return
	}
	if order.Status == model.SkDone || order.Status == model.SkVoid {
		response.Fail(c, response.NewBiz(response.ErrBadState, "单据已完成或已作废"))
		return
	}
	for _, pl := range req.Lines {
		if pl.Qty <= 0 {
			response.Fail(c, response.NewBiz(response.ErrParam, "过账数量必须大于 0"))
			return
		}
	}
	err := posting.PostLinesTx(w.DB, ac, order, req.Lines)
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(w.DB, ac, c.ClientIP(), "post", "stock_order", order.DocNo, order.DocNo, string(order.Status), "")
	events.Publish(w.DB, ac.TenantID, "stock.posted", gin.H{"docNo": order.DocNo, "docType": order.DocType, "sourceType": order.SourceType})
	response.OK2(c, gin.H{"ok": true})
}

// applyStock 对（仓库+货位+物料+批次）维度做行锁过账；dirQty 正入负出。
func (w *WmsAPI) applyStock(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, line model.StockOrderLine, batch string, loc *int64, dirQty float64, direction string) error {
	return posting.ApplyStockAt(tx, ac, order, line, batch, loc, dirQty, direction)
}









func (w *WmsAPI) StartStocktaking(c *gin.Context) {
	ac := middleware.GetAuth(c)
	w.updateStkStatus(c, ac, []string{"draft"}, "counting")
}

func (w *WmsAPI) CountLine(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		CountedQty float64 `json:"countedQty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var st model.Stocktaking
	if err := w.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").First(&st, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "盘点单不存在"))
		return
	}
	if st.Status != "counting" && st.Status != "draft" {
		response.Fail(c, response.NewBiz(response.ErrBadState, "盘点单不在可录入状态"))
		return
	}
	lineID := c.Param("lineId")
	if err := w.DB.Model(&model.StocktakingLine{}).Where("id=? AND stocktaking_id=?", lineID, st.ID).
		Update("counted_qty", req.CountedQty).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// FinishStockting 差异确认 → 生成盘盈/盘亏调整草稿单（FR-WMS-053）。
func (w *WmsAPI) FinishStocktaking(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var st model.Stocktaking
	if err := w.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").First(&st, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "盘点单不存在"))
		return
	}
	if st.Status != "counting" {
		response.Fail(c, response.NewBiz(response.ErrBadState, "盘点未开始或已完成"))
		return
	}
	var gains, losses []model.StocktakingLine
	for _, l := range st.Lines {
		if l.CountedQty == nil {
			continue
		}
		if *l.CountedQty > l.BookQty+1e-9 {
			gains = append(gains, l)
		} else if *l.CountedQty < l.BookQty-1e-9 {
			losses = append(losses, l)
		}
	}
	err := w.DB.Transaction(func(tx *gorm.DB) error {
		mk := func(sourceType string, ls []model.StocktakingLine, docType model.StockDocType, dt string) error {
			if len(ls) == 0 {
				return nil
			}
			docNo, err := data.NextDocNo(tx, ac.TenantID, dt)
			if err != nil {
				return err
			}
			order := model.StockOrder{
				DocType: docType, WarehouseID: st.WarehouseID, SourceType: sourceType,
				SourceDocType: "stocktaking", SourceDocID: st.ID, SourceDocNo: st.DocNo,
			}
			order.TenantID = ac.TenantID
			order.FactoryID = &ac.FactoryID
			order.DocNo = docNo
			if err := tx.Create(&order).Error; err != nil {
				return err
			}
			for i, l := range ls {
				qty := posting.Abs(*l.CountedQty - l.BookQty)
				if err := tx.Create(&model.StockOrderLine{
					StockOrderID: order.ID, LineNo: i + 1, MaterialID: l.MaterialID,
					BatchNo: l.BatchNo, Qty: qty, FromLocationID: l.LocationID, ToLocationID: l.LocationID,
				}).Error; err != nil {
					return err
				}
				tx.Model(&model.StocktakingLine{}).Where("id=?", l.ID).Update("adjusted", true)
			}
			return nil
		}
		if err := mk("gain", gains, model.DocRCV, "RCV"); err != nil {
			return err
		}
		if err := mk("loss", losses, model.DocSHP, "SHP"); err != nil {
			return err
		}
		return tx.Model(&model.Stocktaking{}).Where("id=?", st.ID).Update("status", "confirmed").Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(w.DB, ac, c.ClientIP(), "finish-counting", "stocktaking", st.DocNo, st.DocNo, "", "")
	response.OK2(c, gin.H{"gainOrders": len(gains), "lossOrders": len(losses)})
}

func (w *WmsAPI) updateStkStatus(c *gin.Context, ac *middleware.AuthCtx, from []string, to string) {
	var st model.Stocktaking
	if err := w.DB.Scopes(data.ScopeFor(ac, true)).First(&st, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "盘点单不存在"))
		return
	}
	for _, f := range from {
		if st.Status == f {
			w.DB.Model(&st).Update("status", to)
			response.OK2(c, gin.H{"ok": true})
			return
		}
	}
	response.Fail(c, response.NewBiz(response.ErrBadState, "状态不允许该操作"))
}

// ---- 质检单列表（mes:quality 只读视图挂 WMS 文件） ----

func (w *WmsAPI) QcRecords(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := w.DB.Scopes(data.ScopeFor(ac, true)).Model(&model.QcRecord{})
	if v := c.Query("result"); v != "" {
		q = q.Where("result=?", v)
	}
	if v := c.Query("work_order_id"); v != "" {
		q = q.Where("work_order_id=?", v)
	}
	var list []model.QcRecord
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(500).Find(&list)
	response.Page(c, list, total)
}

// ---- 盘点单列表/详情 ----

func (w *WmsAPI) ListStocktakings(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := w.DB.Scopes(data.ScopeFor(ac, true)).Model(&model.Stocktaking{})
	if v := c.Query("status"); v != "" {
		q = q.Where("status=?", v)
	}
	var list []model.Stocktaking
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(200).Find(&list)
	response.Page(c, list, total)
}

func (w *WmsAPI) GetStocktaking(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var st model.Stocktaking
	if err := w.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").First(&st, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "盘点单不存在"))
		return
	}
	response.OK2(c, st)
}

// ---- 盘点（抽取时误删，自 HEAD 恢复）----

func (w *WmsAPI) CreateStocktaking(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var st model.Stocktaking
	if err := c.ShouldBindJSON(&st); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	whID := st.WarehouseID
	docNo, _ := data.NextDocNo(w.DB, ac.TenantID, "STK")
	st = model.Stocktaking{WarehouseID: whID, Scope: st.Scope, Method: st.Method, Remark: st.Remark}
	st.TenantID = ac.TenantID
	st.FactoryID = &ac.FactoryID
	st.DocNo = docNo
	err := w.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&st).Error; err != nil {
			return err
		}
		// 快照当前库存行（FR-WMS-051）
		var stocks []model.Stock
		tx.Where("tenant_id=? AND factory_id=? AND warehouse_id=?", ac.TenantID, ac.FactoryID, whID).Find(&stocks)
		for i, s := range stocks {
			if err := tx.Create(&model.StocktakingLine{
				StocktakingID: st.ID, LocationID: s.LocationID, MaterialID: s.MaterialID,
				BatchNo: s.BatchNo, BookQty: s.QtyOnHand,
			}).Error; err != nil {
				return err
			}
			_ = i
		}
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, st)
}

