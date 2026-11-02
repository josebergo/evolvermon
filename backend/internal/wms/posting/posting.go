// Package posting wms 过账核心（Phase 3 saga：自 api handler 抽取的包级函数，
// 供 /internal/wms/* 端点与 Local 通道直调；三库拆分后为 core-wms 唯一写入口）。
package posting

import (
	"os"
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/data"
	messvc "github.com/evolvermon/evolvermon/internal/mes/svc"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/conv"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

// PostLine 过账行（幂等键由调用方按单据维度保证）。
type PostLine struct {
	LineID     int64   `json:"lineId"`
	Qty        float64 `json:"qty"`
	BatchNo    string  `json:"batchNo"`
	LocationID *int64  `json:"locationId"`
}

func ApplyStockAt2(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, line model.StockOrderLine, batch string, loc *int64, dirQty float64, direction, bucket, bizNote string) error {
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
		return response.NewBiz(response.ErrStockOut, "可用库存不足（账面 "+conv.F64(cur)+"），禁止负库存（可在系统参数开启）")
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
		Direction: model.MoveDirection(direction), Qty: Abs(dirQty), QtyType: bucket,
		BizType: bizTypeOf(order) + bizNote,
		SourceDocType: "stock_order", SourceDocID: order.ID,
		SourceDocNo: order.DocNo, BeforeQty: before, AfterQty: after, OpUserID: ac.UserID,
	}
	return tx.Create(&mov).Error
}


func ApplyStockAt(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, line model.StockOrderLine, batch string, loc *int64, dirQty float64, direction string) error {
	// 唯一维度行锁定或创建（数据库设计 §6：表达式唯一索引）
	// FR-WMS-033：货位冻结禁止过账
	if loc != nil {
	if l2, lerr := client.LocationRef(context.Background(), *loc); lerr == nil && l2.Frozen {
			return response.NewBiz(response.ErrBiz, "货位 "+l2.Code+" 已冻结，禁止过账")
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
		return response.NewBiz(response.ErrStockOut, "可用库存不足（账面 "+conv.F64(stock.QtyOnHand)+"），禁止负库存（可在系统参数开启）")
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
		Direction: model.MoveDirection(direction), Qty: Abs(dirQty), QtyType: "on_hand",
		BizType: bizTypeOf(order), SourceDocType: "stock_order", SourceDocID: order.ID,
		SourceDocNo: order.DocNo, BeforeQty: before, AfterQty: after, OpUserID: ac.UserID,
	}
	return tx.Create(&mov).Error
}


// writeBack 单据回写：销售已发量+订单状态、采购已入库量、工单领料量/入库状态。
func WriteBack(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []PostLine) error {
	// Phase 3 ③B-③：Remote 模式（拆库）跨域段走端点；wms 自域段（SN 核销）始终本地
	remoteErp := os.Getenv("EM_ERP_URL") != ""
	remoteMes := os.Getenv("EM_MES_URL") != ""
	_ = remoteErp
	_ = remoteMes
	switch order.SourceType {
	case "sales":
		if remoteErp {
			if err := httpWB(os.Getenv("EM_ERP_URL"), os.Getenv("EM_INTERNAL_TOKEN"), wbReq(tx, ac, order, lines)); err != nil {
				return err
			}
			for _, l := range order.Lines {
				mat, merr := client.MatRef(context.Background(), ac.TenantID, l.MaterialID)
				if merr != nil || !mat.SerialManaged {
					continue
				}
				tx.Model(&model.SerialNumber{}).
					Where("tenant_id=? AND material_id=? AND status='in_stock'", ac.TenantID, l.MaterialID).
					Order("id").Limit(int(l.DoneQty)).
					Updates(map[string]any{"status": "out", "stock_order_id": order.ID, "updated_at": time.Now()})
			}
			return nil
		}
		// 按行回写已发量
		for _, pl := range lines {
			var sol model.SalesOrderLine
			if err := tx.Where("sales_order_id=?", order.SourceDocID).First(&sol, FindSOLine(tx, order, pl)).Error; err == nil {
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
			tx.Raw("SELECT COUNT(*) FROM erp.sales_order_lines WHERE sales_order_id=? AND qty - shipped_qty > 1e-9", so.ID).Scan(&remain)
			st := model.StPartial
			if remain == 0 {
				st = model.StDone
			}
			tx.Model(&so).Updates(map[string]any{"status": st})
		}
		// 序列号核销：先进先出标记出库（M2 v1）
		if os.Getenv("WB_NO_SN") != "1" {
		for _, l := range order.Lines {
			mat, merr := client.MatRef(context.Background(), ac.TenantID, l.MaterialID)
			if merr != nil || !mat.SerialManaged {
				continue
			}
			tx.Model(&model.SerialNumber{}).
				Where("tenant_id=? AND material_id=? AND status='in_stock'", ac.TenantID, l.MaterialID).
				Order("id").Limit(int(l.DoneQty)).
				Updates(map[string]any{"status": "out", "stock_order_id": order.ID, "updated_at": time.Now()})
		}
		}

	case "purchase":
		if remoteErp || remoteMes {
			u := os.Getenv("EM_ERP_URL")
			if u == "" { u = os.Getenv("EM_MES_URL") }
			return httpWB(u, os.Getenv("EM_INTERNAL_TOKEN"), wbReq(tx, ac, order, lines))
		}
		for _, pl := range lines {
			var pol model.PurchaseOrderLine
			if err := tx.Where("purchase_order_id=?", order.SourceDocID).First(&pol, FindPOLine(tx, order, pl)).Error; err == nil {
				tx.Model(&pol).Update("received_qty", gorm.Expr("received_qty+?", pl.Qty))
			}
		}
		var po model.PurchaseOrder
		if err := tx.First(&po, order.SourceDocID).Error; err == nil {
			var remain int64
			tx.Raw("SELECT COUNT(*) FROM erp.purchase_order_lines WHERE purchase_order_id=? AND qty - received_qty > 1e-9", po.ID).Scan(&remain)
			st := model.StReceiving
			if remain == 0 {
				st = model.StDone
			}
			tx.Model(&po).Updates(map[string]any{"status": st})
		}
		// M4：到货自动生成 IQC 待检单（FR-MES-042 来源自动化）
		messvc.AutoQc(tx, ac, order, svcLinesIn(lines), model.QcIQC)
	case "picking":
		if remoteMes {
			return httpWB(os.Getenv("EM_MES_URL"), os.Getenv("EM_INTERNAL_TOKEN"), wbReq(tx, ac, order, lines))
		}
		for _, pl := range lines {
			tx.Model(&model.WorkOrder{}).Where("id=?", order.SourceDocID).
				Update("picked_qty", gorm.Expr("picked_qty+?", pl.Qty))
		}
	case "return":
		if remoteMes {
			return httpWB(os.Getenv("EM_MES_URL"), os.Getenv("EM_INTERNAL_TOKEN"), wbReq(tx, ac, order, lines))
		}
		// A5 退料：冲减工单已领量（FR-MES-005）
		for _, pl := range lines {
			tx.Model(&model.WorkOrder{}).Where("id=?", order.SourceDocID).
				Update("picked_qty", gorm.Expr("GREATEST(picked_qty-?, 0)", pl.Qty))
		}
	case "production":
		if remoteMes {
			// mes 段（WO stored/AutoQc/成本）远程；wms 自域 SN 生成本地执行
			if err := httpWB(os.Getenv("EM_MES_URL"), os.Getenv("EM_INTERNAL_TOKEN"), wbReq(tx, ac, order, lines)); err != nil {
				return err
			}
			genSN(tx, ac, order, lines)
			return nil
		}
		// 完工入库：工单 → 已入库；FQC 待检自动生成；实际成本自动归集（M4）
		tx.Model(&model.WorkOrder{}).Where("id=? AND status='finished'", order.SourceDocID).
			Update("status", model.WoStored)
		messvc.AutoQc(tx, ac, order, svcLinesIn(lines), model.QcFQC)
		_, _ = messvc.CostWorkOrder(tx, ac, order.SourceDocID)
		postedQty := map[int64]float64{}
		for _, pl := range lines {
			postedQty[pl.LineID] = postedQty[pl.LineID] + pl.Qty
		}
		if os.Getenv("WB_NO_SN") == "1" {
			return nil // SN 由 core-wms 远程分支生成
		}
		for _, l := range order.Lines {
			mat, merr := client.MatRef(context.Background(), ac.TenantID, l.MaterialID)
			if merr != nil || !mat.SerialManaged {
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


func FindSOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {
	var l model.StockOrderLine
	tx.First(&l, pl.LineID)
	var sol model.SalesOrderLine
	tx.Where("sales_order_id=? AND material_id=?", order.SourceDocID, l.MaterialID).Order("line_no").First(&sol)
	return sol.ID
}


func FindPOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {
	var l model.StockOrderLine
	tx.First(&l, pl.LineID)
	var pol model.PurchaseOrderLine
	tx.Where("purchase_order_id=? AND material_id=?", order.SourceDocID, l.MaterialID).Order("line_no").First(&pol)
	return pol.ID
}
func Abs(f float64) float64 { if f < 0 { return -f }; return f }

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

// svcLinesIn 过账行转质检入参（messvc.PostLine）。
func svcLinesIn(lines []PostLine) []messvc.PostLine {
	out := make([]messvc.PostLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, messvc.PostLine{LineID: l.LineID, Qty: l.Qty})
	}
	return out
}

func findSOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {
	var l model.StockOrderLine
	tx.First(&l, pl.LineID)
	var sol model.SalesOrderLine
	tx.Where("sales_order_id=? AND material_id=?", order.SourceDocID, l.MaterialID).Order("line_no").First(&sol)
	return sol.ID
}

func findPOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {
	var l model.StockOrderLine
	tx.First(&l, pl.LineID)
	var pol model.PurchaseOrderLine
	tx.Where("purchase_order_id=? AND material_id=?", order.SourceDocID, l.MaterialID).Order("line_no").First(&pol)
	return pol.ID
}

// PostLinesTx 过账主事务（自 api.PostLines 抽取；/internal/wms 与 Local 通道共用）。
func PostLinesTx(db *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []PostLine) error {
	return db.Transaction(func(tx *gorm.DB) error {
for _, pl := range lines {
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
		mat, merr := client.MatRef(context.Background(), ac.TenantID, line.MaterialID)
		if merr != nil {
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
			if err := ApplyStockAt2(tx, ac, order, line, batch, toLoc, +pl.Qty, "IN", bucket, bizNote); err != nil {
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
				if err := ApplyStockAt(tx, ac, order, line, br.Batch, toLoc, -take, "OUT"); err != nil {
					return err
				}
				remaining -= take
			}
			if remaining > 1e-9 {
				if !data.BoolParam(tx, ac.TenantID, "allow_negative_stock") {
					return response.NewBiz(response.ErrStockOut,
						fmt.Sprintf("行 %d 可用库存不足（缺 %s），禁止负库存（可在系统参数开启）", line.LineNo, conv.F64(remaining)))
				}
				// 参数开启：缺口以无批次维度落负账（总册 OQ-3）
				if err := ApplyStockAt(tx, ac, order, line, "", toLoc, -remaining, "OUT"); err != nil {
					return err
				}
				remaining = 0
			}
		case model.DocMV:
			// 移库 = 原货位出 + 目标货位入（简化：同仓库内）
			from := line.FromLocationID
			if err := ApplyStockAt(tx, ac, order, line, batch, from, -pl.Qty, "OUT"); err != nil {
				return err
			}
			if err := ApplyStockAt(tx, ac, order, line, batch, toLoc, +pl.Qty, "IN"); err != nil {
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
	tx.Raw(`SELECT COUNT(*) FROM wms.stock_order_lines WHERE stock_order_id=? AND qty - done_qty > 1e-9`, order.ID).Scan(&remain)
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
	return WriteBack(tx, ac, order, lines)
	})
}

// genSN 完工入库序列号生成（wms 自域；Remote 分支调用，Local 段内联保留）。
func genSN(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []PostLine) error {
	postedQty := map[int64]float64{}
	for _, pl := range lines {
		postedQty[pl.LineID] = postedQty[pl.LineID] + pl.Qty
	}
	for _, l := range order.Lines {
		mat, merr := client.MatRef(context.Background(), ac.TenantID, l.MaterialID)
		if merr != nil || !mat.SerialManaged {
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
	return nil
}
