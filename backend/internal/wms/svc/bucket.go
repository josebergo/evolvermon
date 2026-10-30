// wms 域服务：三桶库存转移（自 api 包下沉，供 mes 质检处置联动调用；Phase 3 API 化）。
package svc

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
)

// TransferBucket QC 联动桶间转移（FR-WMS-025/MES-043）：pending→on_hand / pending→defective。
func TransferBucket(tx *gorm.DB, ac *middleware.AuthCtx, warehouseID int64, line model.StockOrderLine, batch string, qty float64, from, to, bizType, srcNo string) error {
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
