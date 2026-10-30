// Package svc mes 域服务层：跨 api 包复用的业务逻辑（Phase 0.1 自 api 包迁出）。
package svc

import (
	"log"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
)

// PostLine 过账行（wms→mes 跨域 DTO：仅质检所需字段）。
type PostLine struct {
	LineID int64
	Qty    float64
}

// AutoQc 过账后自动生成待检记录（purchase→IQC / production→FQC），FR-MES-042 来源自动化。
// Phase 0.1 起独立于 wms api 包（拆库后由 core-wms 经内部 API 调用，见微服务方案 A 表）。
func AutoQc(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []PostLine, checkType model.QcCheckType) {
	lineByID := map[int64]model.StockOrderLine{}
	for _, l := range order.Lines {
		lineByID[l.ID] = l
	}
	for _, pl := range lines {
		l, ok := lineByID[pl.LineID]
		if !ok || pl.Qty <= 1e-9 {
			continue
		}
		docNo, err := data.NextDocNo(tx, ac.TenantID, "QC")
		if err != nil {
			log.Printf("[autoQc] nextdocno: %v", err)
			continue
		}
		rec := model.QcRecord{
			SourceType: map[string]string{"purchase": "arrive", "production": "finish"}[order.SourceType],
			SourceDocType: "stock_order", SourceDocID: order.ID, SourceDocNo: order.DocNo,
			MaterialID: l.MaterialID, BatchNo: l.BatchNo, Qty: pl.Qty,
			Result: model.QcPending,
		}
		if order.SourceType == "production" {
			wo := model.WorkOrder{}
			if tx.First(&wo, order.SourceDocID).Error == nil {
				rec.WorkOrderID = &wo.ID
			}
		}
		rec.TenantID = ac.TenantID
		rec.FactoryID = &ac.FactoryID
		rec.DocNo = docNo
		if err := tx.Create(&rec).Error; err != nil {
			log.Printf("[autoQc] create failed: %v", err)
		}
	}
	log.Printf("[autoQc] done: order=%s type=%s posted_lines=%d", order.DocNo, checkType, len(lines))
}
