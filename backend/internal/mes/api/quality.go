package api

// M4 质量与成本域：自动质检(IQC/FQC)、返工/报废处置、工单成本归集、财务凭证导出。

import (
	"github.com/evolvermon/evolvermon/internal/mes/svc"
	"github.com/evolvermon/evolvermon/internal/pkg/conv"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type QualityAPI struct{ DB *gorm.DB }


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
	if _, merr := client.MatRef(c.Request.Context(), ac.TenantID, rec.MaterialID); merr != nil {
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
			PlanStartAt: conv.PtrTime(time.Now()), PlanEndAt: conv.PtrTime(time.Now().Add(48 * time.Hour)),
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
		whDef, derr := client.WhByType(c.Request.Context(), ac.TenantID, ac.FactoryID, "defective")
		if derr != nil {
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
	total, err := svc.CostWorkOrder(q.DB, ac, wo.ID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"workOrder": wo.DocNo, "cost": total, "unitCost": total / maxF(wo.Qty, 1)})
}


func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
