package api

// WMS：库存过账（行级锁防超卖，core-flows.md §3）、可用量、流水、盘点（FR-WMS 全域）。

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
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

type postLineReq struct {
	LineID     int64   `json:"lineId" binding:"required"`
	Qty        float64 `json:"qty" binding:"required,gt=0"`
	BatchNo    string  `json:"batchNo"`
	LocationID *int64  `json:"locationId"`
}

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
	err := w.DB.Transaction(func(tx *gorm.DB) error {
		for _, pl := range req.Lines {
			var line model.StockOrderLine
			if err := tx.Where("stock_order_id=?", order.ID).First(&line, pl.LineID).Error; err != nil {
				return response.NewBiz(response.ErrParam, fmt.Sprintf("行 %d 不存在", pl.LineID))
			}
			if pl.Qty > line.Qty-line.DoneQty+1e-9 {
				return response.NewBiz(response.ErrStockOut, fmt.Sprintf("行 %d 超出未执行数量", line.LineNo))
			}
			batch := pl.BatchNo
			if batch == "" {
				batch = line.BatchNo
			}
			var mat model.Material
			if err := tx.Where("tenant_id=?", ac.TenantID).First(&mat, line.MaterialID).Error; err != nil {
				return response.NewBiz(response.ErrBiz, "物料不存在")
			}
			var toLoc *int64 = pl.LocationID
			if toLoc == nil {
				toLoc = line.ToLocationID
			}
			hasIqc := tx.Model(&model.QcScheme{}).
				Where("tenant_id=? AND material_id=? AND check_type='iqc'", ac.TenantID, line.MaterialID).
				First(&model.QcScheme{}).Error == nil

			switch order.DocType {
			case model.DocRCV:
				// 入库方向：批次物料必须明确批次（生成新批次或对上来源批次）
				if mat.BatchManaged && batch == "" {
					return response.NewBiz(response.ErrParam, mat.Name+" 启用批次管理，入库必须提供批次号")
				}
				bucket := "on_hand"
				bizNote := ""
				// FR-WMS-025：配置 IQC 检验方案的采购到货先入「待检」，检验合格转可用
				if hasIqc && order.SourceType == "purchase" {
					bucket = "pending"
					bizNote = "（待检）"
				}
				if err := w.applyStockAt2(tx, ac, order, line, batch, toLoc, +pl.Qty, "IN", bucket, bizNote); err != nil {
					return err
				}
			case model.DocSHP:
				// FR-WMS-036(M1)：批次物料未指定批次时按 FIFO 自动拣批，逐批扣减
				remaining := pl.Qty
				type batchRow struct {
					Batch string
					Qty   float64
				}
				var batches []batchRow
				if batch != "" {
					batches = []batchRow{{batch, pl.Qty}}
				} else {
					tx.Model(&model.Stock{}).
						Select("batch_no AS batch, qty_on_hand AS qty").
						Where("tenant_id=? AND factory_id=? AND warehouse_id=? AND material_id=? AND qty_on_hand > 0",
							ac.TenantID, ac.FactoryID, order.WarehouseID, line.MaterialID).
						Order("updated_at ASC").Scan(&batches)
					if len(batches) == 0 {
						batches = []batchRow{{"", pl.Qty}}
					}
				}
				for _, br := range batches {
					if remaining <= 1e-9 {
						break
					}
					take := remaining
					if batch == "" && br.Qty < take {
						take = br.Qty
					}
					if take <= 1e-9 {
						continue
					}
					if err := w.applyStockAt(tx, ac, order, line, br.Batch, toLoc, -take, "OUT"); err != nil {
						return err
					}
					remaining -= take
				}
				if remaining > 1e-9 {
					if !data.BoolParam(tx, ac.TenantID, "allow_negative_stock") {
						return response.NewBiz(response.ErrStockOut,
							fmt.Sprintf("行 %d 可用库存不足（缺 %s），禁止负库存（可在系统参数开启）", line.LineNo, f64(remaining)))
					}
					// 参数开启：缺口以无批次维度落负账（总册 OQ-3）
					if err := w.applyStockAt(tx, ac, order, line, "", toLoc, -remaining, "OUT"); err != nil {
						return err
					}
					remaining = 0
				}
			case model.DocMV:
				// 移库 = 原货位出 + 目标货位入（简化：同仓库内）
				from := line.FromLocationID
				if err := w.applyStockAt(tx, ac, order, line, batch, from, -pl.Qty, "OUT"); err != nil {
					return err
				}
				if err := w.applyStockAt(tx, ac, order, line, batch, toLoc, +pl.Qty, "IN"); err != nil {
					return err
				}
			}
			// 行回写
			if err := tx.Model(&model.StockOrderLine{}).Where("id=?", line.ID).
				Update("done_qty", gorm.Expr("done_qty+?", pl.Qty)).Error; err != nil {
				return err
			}
		}
		// 状态推进
		var remain int64
		tx.Raw(`SELECT COUNT(*) FROM stock_order_lines WHERE stock_order_id=? AND qty - done_qty > 1e-9`, order.ID).Scan(&remain)
		status := model.SkDoing
		if remain == 0 {
			status = model.SkDone
		}
		now := time.Now()
		if err := tx.Model(&model.StockOrder{}).Where("id=?", order.ID).
			Updates(map[string]any{"status": status, "posted_by": ac.UserID, "posted_at": &now}).Error; err != nil {
			return err
		}
		// 业务回写（销售已发量 / 采购已入库量 / 工单状态与领料量）
		return w.writeBack(tx, ac, order, req.Lines)
	})
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
	return w.applyStockAt(tx, ac, order, line, batch, loc, dirQty, direction)
}

// TransferBucket QC 联动桶间转移（FR-WMS-025/MES-043）：pending→on_hand / pending→defective。
func (w *WmsAPI) TransferBucket(tx *gorm.DB, ac *middleware.AuthCtx, warehouseID int64, line model.StockOrderLine, batch string, qty float64, from, to, bizType, srcNo string) error {
	var stock model.Stock
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id=? AND factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=?",
			ac.TenantID, ac.FactoryID, warehouseID, line.MaterialID, batch).
		First(&stock).Error
	if err != nil {
		return err
	}
	col := func(b string) string {
		return map[string]string{"on_hand": "qty_on_hand", "pending": "qty_pending", "defective": "qty_defective"}[b]
	}
	ups := map[string]any{
		col(from): gorm.Expr(col(from)+" - ?", qty),
		col(to):   gorm.Expr(col(to) + " + ?", qty),
	}
	if err := tx.Model(&model.Stock{}).Where("id=?", stock.ID).Updates(ups).Error; err != nil {
		return err
	}
	// 双流水：来源桶 OUT + 目标桶 IN（保证每个桶的流水重算自洽）
	mk := func(dir model.MoveDirection, bucket string) model.StockMovement {
		return model.StockMovement{
			TenantID: ac.TenantID, FactoryID: ac.FactoryID, WarehouseID: warehouseID,
			MaterialID: line.MaterialID, BatchNo: batch,
			Direction: dir, Qty: qty, QtyType: bucket, BizType: bizType,
			SourceDocType: "qc_record", SourceDocNo: srcNo,
			BeforeQty: 0, AfterQty: qty, OpUserID: ac.UserID,
		}
	}
	out := mk(model.MoveOut, from)
	if err := tx.Create(&out).Error; err != nil {
		return err
	}
	in := mk(model.MoveIn, to)
	return tx.Create(&in).Error
}

func (w *WmsAPI) applyStockAt2(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, line model.StockOrderLine, batch string, loc *int64, dirQty float64, direction, bucket, bizNote string) error {
	col := map[string]string{"on_hand": "qty_on_hand", "pending": "qty_pending", "defective": "qty_defective"}[bucket]
	var stock model.Stock
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=? AND (location_id IS NOT DISTINCT FROM ?)",
			ac.FactoryID, order.WarehouseID, line.MaterialID, batch, loc).
		First(&stock).Error
	if err == gorm.ErrRecordNotFound {
		stock = model.Stock{
			TenantID: ac.TenantID, FactoryID: ac.FactoryID, WarehouseID: order.WarehouseID,
			LocationID: loc, MaterialID: line.MaterialID, BatchNo: batch,
		}
		if err := tx.Create(&stock).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	cur := map[string]float64{"on_hand": stock.QtyOnHand, "pending": stock.QtyPending, "defective": stock.QtyDefective}[bucket]
	if dirQty < 0 && cur+dirQty < -1e-9 && !data.BoolParam(tx, ac.TenantID, "allow_negative_stock") {
		return response.NewBiz(response.ErrStockOut, "可用库存不足（账面 "+f64(cur)+"），禁止负库存（可在系统参数开启）")
	}
	before := cur
	after := before + dirQty
	if err := tx.Model(&model.Stock{}).Where("id=?", stock.ID).
		Update(col, gorm.Expr(col+" + ?", dirQty)).Error; err != nil {
		return err
	}
	mov := model.StockMovement{
		TenantID: ac.TenantID, FactoryID: ac.FactoryID, WarehouseID: order.WarehouseID,
		LocationID: loc, MaterialID: line.MaterialID, BatchNo: batch,
		Direction: model.MoveDirection(direction), Qty: abs(dirQty), QtyType: bucket,
		BizType: bizTypeOf(order) + bizNote,
		SourceDocType: "stock_order", SourceDocID: order.ID,
		SourceDocNo: order.DocNo, BeforeQty: before, AfterQty: after, OpUserID: ac.UserID,
	}
	return tx.Create(&mov).Error
}

func (w *WmsAPI) applyStockAt(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, line model.StockOrderLine, batch string, loc *int64, dirQty float64, direction string) error {
	// 唯一维度行锁定或创建（数据库设计 §6：表达式唯一索引）
	// FR-WMS-033：货位冻结禁止过账
	if loc != nil {
		var l model.Location
		if err := tx.First(&l, *loc).Error; err == nil && l.Frozen {
			return response.NewBiz(response.ErrBiz, "货位 "+l.Code+" 已冻结，禁止过账")
		}
	}
	var stock model.Stock
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=? AND (location_id IS NOT DISTINCT FROM ?)",
			ac.FactoryID, order.WarehouseID, line.MaterialID, batch, loc).
		First(&stock).Error
	if err == gorm.ErrRecordNotFound {
		stock = model.Stock{
			TenantID: ac.TenantID, FactoryID: ac.FactoryID, WarehouseID: order.WarehouseID,
			LocationID: loc, MaterialID: line.MaterialID, BatchNo: batch,
		}
		if err := tx.Create(&stock).Error; err != nil {
			return err // 并发下唯一索引冲突 → 整体重试
		}
	} else if err != nil {
		return err
	}
	if dirQty < 0 && stock.QtyOnHand+dirQty < -1e-9 && !data.BoolParam(tx, ac.TenantID, "allow_negative_stock") {
		return response.NewBiz(response.ErrStockOut, "可用库存不足（账面 "+f64(stock.QtyOnHand)+"），禁止负库存（可在系统参数开启）")
	}
	before := stock.QtyOnHand
	after := before + dirQty
	if err := tx.Model(&model.Stock{}).Where("id=?", stock.ID).
		Update("qty_on_hand", after).Error; err != nil {
		return err
	}
	mov := model.StockMovement{
		TenantID: ac.TenantID, FactoryID: ac.FactoryID, WarehouseID: order.WarehouseID,
		LocationID: loc, MaterialID: line.MaterialID, BatchNo: batch,
		Direction: model.MoveDirection(direction), Qty: abs(dirQty), QtyType: "on_hand",
		BizType: bizTypeOf(order), SourceDocType: "stock_order", SourceDocID: order.ID,
		SourceDocNo: order.DocNo, BeforeQty: before, AfterQty: after, OpUserID: ac.UserID,
	}
	return tx.Create(&mov).Error
}

// writeBack 单据回写：销售已发量+订单状态、采购已入库量、工单领料量/入库状态。
func (w *WmsAPI) writeBack(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []postLineReq) error {
	switch order.SourceType {
	case "sales":
		// 按行回写已发量
		for _, pl := range lines {
			var sol model.SalesOrderLine
			if err := tx.Where("sales_order_id=?", order.SourceDocID).First(&sol, findSOLine(tx, order, pl)).Error; err == nil {
				tx.Model(&sol).Update("shipped_qty", gorm.Expr("shipped_qty+?", pl.Qty))
			}
		}
		// 核销预占 + 状态推进
		tx.Model(&model.StockReservation{}).
			Where("source_doc_type='sales_order' AND source_doc_id=? AND status='held'", order.SourceDocID).
			Updates(map[string]any{"status": model.RvConsumed})
		var so model.SalesOrder
		if err := tx.First(&so, order.SourceDocID).Error; err == nil {
			var remain int64
			tx.Raw("SELECT COUNT(*) FROM sales_order_lines WHERE sales_order_id=? AND qty - shipped_qty > 1e-9", so.ID).Scan(&remain)
			st := model.StPartial
			if remain == 0 {
				st = model.StDone
			}
			tx.Model(&so).Updates(map[string]any{"status": st})
		}
		// 序列号核销：先进先出标记出库（M2 v1）
		for _, l := range order.Lines {
			var mat model.Material
			if err := tx.Scopes(data.ScopeFor(ac, false)).First(&mat, l.MaterialID).Error; err != nil || !mat.SerialManaged {
				continue
			}
			tx.Model(&model.SerialNumber{}).
				Where("tenant_id=? AND material_id=? AND status='in_stock'", ac.TenantID, l.MaterialID).
				Order("id").Limit(int(l.DoneQty)).
				Updates(map[string]any{"status": "out", "stock_order_id": order.ID, "updated_at": time.Now()})
		}
	case "purchase":
		for _, pl := range lines {
			var pol model.PurchaseOrderLine
			if err := tx.Where("purchase_order_id=?", order.SourceDocID).First(&pol, findPOLine(tx, order, pl)).Error; err == nil {
				tx.Model(&pol).Update("received_qty", gorm.Expr("received_qty+?", pl.Qty))
			}
		}
		var po model.PurchaseOrder
		if err := tx.First(&po, order.SourceDocID).Error; err == nil {
			var remain int64
			tx.Raw("SELECT COUNT(*) FROM purchase_order_lines WHERE purchase_order_id=? AND qty - received_qty > 1e-9", po.ID).Scan(&remain)
			st := model.StReceiving
			if remain == 0 {
				st = model.StDone
			}
			tx.Model(&po).Updates(map[string]any{"status": st})
		}
		// M4：到货自动生成 IQC 待检单（FR-MES-042 来源自动化）
		autoQc(tx, ac, order, lines, model.QcIQC)
	case "picking":
		for _, pl := range lines {
			tx.Model(&model.WorkOrder{}).Where("id=?", order.SourceDocID).
				Update("picked_qty", gorm.Expr("picked_qty+?", pl.Qty))
		}
	case "return":
		// A5 退料：冲减工单已领量（FR-MES-005）
		for _, pl := range lines {
			tx.Model(&model.WorkOrder{}).Where("id=?", order.SourceDocID).
				Update("picked_qty", gorm.Expr("GREATEST(picked_qty-?, 0)", pl.Qty))
		}
	case "production":
		// 完工入库：工单 → 已入库；FQC 待检自动生成；实际成本自动归集（M4）
		tx.Model(&model.WorkOrder{}).Where("id=? AND status='finished'", order.SourceDocID).
			Update("status", model.WoStored)
		autoQc(tx, ac, order, lines, model.QcFQC)
		_, _ = CostWorkOrder(tx, ac, order.SourceDocID)
		postedQty := map[int64]float64{}
		for _, pl := range lines {
			postedQty[pl.LineID] = postedQty[pl.LineID] + pl.Qty
		}
		for _, l := range order.Lines {
			var mat model.Material
			if err := tx.Scopes(data.ScopeFor(ac, false)).First(&mat, l.MaterialID).Error; err != nil || !mat.SerialManaged {
				continue
			}
			n := int(postedQty[l.ID])
			if n < 0 {
				n = 0
			}
			if n > 2000 {
				n = 2000
			}
			for i := 1; i <= n; i++ {
				sn := model.SerialNumber{
					TenantID: ac.TenantID, FactoryID: ac.FactoryID, MaterialID: l.MaterialID,
					Serial: "SN-" + order.DocNo + "-" + fmt.Sprintf("%04d", i),
					BatchNo: l.BatchNo, WorkOrderID: order.SourceDocID, WorkOrderNo: order.SourceDocNo,
					StockOrderID: order.ID, Status: "in_stock",
				}
				if err := tx.Create(&sn).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func findSOLine(tx *gorm.DB, order model.StockOrder, pl postLineReq) int64 {
	var l model.StockOrderLine
	tx.First(&l, pl.LineID)
	var sol model.SalesOrderLine
	tx.Where("sales_order_id=? AND material_id=?", order.SourceDocID, l.MaterialID).Order("line_no").First(&sol)
	return sol.ID
}

func findPOLine(tx *gorm.DB, order model.StockOrder, pl postLineReq) int64 {
	var l model.StockOrderLine
	tx.First(&l, pl.LineID)
	var pol model.PurchaseOrderLine
	tx.Where("purchase_order_id=? AND material_id=?", order.SourceDocID, l.MaterialID).Order("line_no").First(&pol)
	return pol.ID
}

func bizTypeOf(o model.StockOrder) string {
	switch o.SourceType {
	case "purchase":
		return "采购入库"
	case "production":
		return "生产入库"
	case "sales":
		return "销售出库"
	case "picking":
		return "领料出库"
	case "return":
		return "退料入库"
	case "move":
		return "移库"
	case "gain":
		return "盘盈"
	case "loss":
		return "盘亏"
	case "scrap":
		return "报废出库"
	}
	return "手工"
}

func abs(f float64) float64 { if f < 0 { return -f }; return f }
func f64(f float64) string  { return fmt.Sprintf("%g", f) }

// ---- 盘点 ----

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
				qty := abs(*l.CountedQty - l.BookQty)
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
