package api

// M4 质量与成本域：自动质检(IQC/FQC)、返工/报废处置、工单成本归集、财务凭证导出。

import (
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type QualityAPI struct{ DB *gorm.DB }

// autoQc 过账后自动生成待检记录（purchase→IQC / production→FQC），FR-MES-042 来源自动化。
func autoQc(tx *gorm.DB, ac *middleware.AuthCtx, order model.StockOrder, lines []postLineReq, checkType model.QcCheckType) {
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

// Dispose 不合格处置：返工→生成返工工单；报废→生成移库单（转入不良品仓）。
func (q *QualityAPI) Dispose(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var rec model.QcRecord
	if err := q.DB.Scopes(data.ScopeFor(ac, true)).First(&rec, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "质检单不存在"))
		return
	}
	if rec.Result != model.QcFail || rec.Disposal == "" {
		response.Fail(c, response.NewBiz(response.ErrBadState, "仅「不合格且已选处置方式」的质检单可执行处置"))
		return
	}
	var mat model.Material
	if err := q.DB.Scopes(data.ScopeFor(ac, false)).First(&mat, rec.MaterialID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "物料不存在"))
		return
	}

	// 处置联动库存桶：返工 → 待检转可用（供返工单领用）；报废 → 待检转不良品
	mesAPI := &MesAPI{DB: q.DB}
	switch rec.Disposal {
	case "rework":
		mesAPI.releasePending(ac, rec, "on_hand", "返工处置转可用")
	case "scrap":
		mesAPI.releasePending(ac, rec, "defective", "报废转不良品")
	}

	switch rec.Disposal {
	case "rework":
		docNo, _ := data.NextDocNo(q.DB, ac.TenantID, "WO")
		wo := model.WorkOrder{
			MaterialID: rec.MaterialID, Qty: rec.Qty, SourceType: "rework",
			SourceDocNo: fmt.Sprintf("QC:%s", rec.DocNo), Priority: 2,
			PlanStartAt: ptrTime(time.Now()), PlanEndAt: ptrTime(time.Now().Add(48 * time.Hour)),
			Remark: "返工工单（来源质检 " + rec.DocNo + "）",
		}
		wo.TenantID = ac.TenantID
		wo.FactoryID = &ac.FactoryID
		wo.DocNo = docNo
		if err := q.DB.Create(&wo).Error; err != nil {
			response.Fail(c, err)
			return
		}
		data.Audit(q.DB, ac, c.ClientIP(), "dispose-rework", "qc_record", rec.DocNo, rec.DocNo, "", wo.DocNo)
		response.OK2(c, gin.H{"reworkWorkOrder": wo.DocNo, "woId": wo.ID})
	case "scrap":
		// 报废：可用仓 → 不良品仓 移库草稿（仓管确认过账）
		var whDef model.Warehouse
		if err := q.DB.Scopes(data.ScopeFor(ac, true)).Where("type='defective'").First(&whDef).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrBiz, "未配置不良品仓"))
			return
		}
		docNo, _ := data.NextDocNo(q.DB, ac.TenantID, "MV")
		mv := model.StockOrder{
			DocType: model.DocMV, WarehouseID: whDef.ID, SourceType: "scrap",
			SourceDocType: "qc_record", SourceDocID: rec.ID, SourceDocNo: rec.DocNo,
			Remark: "报废转移（来源质检 " + rec.DocNo + "）",
		}
		mv.TenantID = ac.TenantID
		mv.FactoryID = &ac.FactoryID
		mv.DocNo = docNo
		if err := q.DB.Create(&mv).Error; err != nil {
			response.Fail(c, err)
			return
		}
		q.DB.Create(&model.StockOrderLine{
			StockOrderID: mv.ID, LineNo: 1, MaterialID: rec.MaterialID,
			BatchNo: rec.BatchNo, Qty: rec.Qty,
		})
		data.Audit(q.DB, ac, c.ClientIP(), "dispose-scrap", "qc_record", rec.DocNo, rec.DocNo, "", mv.DocNo)
		response.OK2(c, gin.H{"scrapMoveOrder": mv.DocNo, "mvId": mv.ID})
	default:
		response.Fail(c, response.NewBiz(response.ErrParam, "不支持的处置方式"))
	}
}

// Costing 工单实际成本归集：料（领料流水×物料价）+ 工（报工工时×工作中心费率）+ 费（人工×50%）。
func (q *QualityAPI) Costing(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var wo model.WorkOrder
	if err := q.DB.Scopes(data.ScopeFor(ac, true)).First(&wo, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "工单不存在"))
		return
	}
	total, err := CostWorkOrder(q.DB, ac, wo.ID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"workOrder": wo.DocNo, "cost": total, "unitCost": total / maxF(wo.Qty, 1)})
}

// CostWorkOrder 计算并回写工单实际成本（可在事务内调用；完工入库过账时自动触发）。
func CostWorkOrder(tx *gorm.DB, ac *middleware.AuthCtx, woID int64) (float64, error) {
	var wo model.WorkOrder
	if err := tx.Scopes(data.ScopeFor(ac, true)).First(&wo, woID).Error; err != nil {
		return 0, err
	}
	var matCost float64
	var picks []model.StockOrder
	tx.Scopes(data.ScopeFor(ac, true)).Preload("Lines").
		Where("source_type='picking' AND source_doc_id=?", wo.ID).Find(&picks)
	for _, p := range picks {
		for _, l := range p.Lines {
			var mat model.Material
			tx.Scopes(data.ScopeFor(ac, false)).First(&mat, l.MaterialID)
			matCost += l.DoneQty * mat.Price
		}
	}
	var laborCost float64
	var reports []model.ProductionReport
	tx.Where("work_order_id=?", wo.ID).Find(&reports)
	opWC := map[int64]int64{}
	var ops []model.WoOperation
	tx.Where("work_order_id=?", wo.ID).Find(&ops)
	for _, op := range ops {
		opWC[op.ID] = op.WorkCenterID
	}
	wcRate := map[int64]float64{}
	var wcs []model.WorkCenter
	tx.Scopes(data.ScopeFor(ac, true)).Find(&wcs)
	for _, w := range wcs {
		wcRate[w.ID] = w.CostRate
	}
	for _, r := range reports {
		laborCost += r.Hours * wcRate[opWC[r.WoOperationID]]
	}
	overhead := laborCost * 0.5
	total := matCost + laborCost + overhead
	if err := tx.Model(&model.WorkOrder{}).Where("id=?", wo.ID).Updates(map[string]any{
		"material_cost": matCost, "labor_cost": laborCost, "overhead_cost": overhead,
	}).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
