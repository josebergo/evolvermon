package api

// 序列号追溯（M2 v1）+ 批次追溯统一入口。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type TraceAPI struct{ DB *gorm.DB }

// Serial 按序列号追溯：序列号 → 工单（报工记录）→ 批次流水。
func (t *TraceAPI) Serial(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var sn model.SerialNumber
	if err := t.DB.Where("tenant_id=? AND serial=?", ac.TenantID, c.Param("sn")).First(&sn).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "序列号不存在"))
		return
	}
	var wo model.WorkOrder
	t.DB.Scopes(data.ScopeFor(ac, true)).First(&wo, sn.WorkOrderID)
	var reports []model.ProductionReport
	t.DB.Where("work_order_id=?", sn.WorkOrderID).Order("reported_at").Find(&reports)
	var movements []model.StockMovement
	t.DB.Where("tenant_id=? AND material_id=?", ac.TenantID, sn.MaterialID).
		Order("id DESC").Limit(20).Find(&movements)
	response.OK2(c, gin.H{"serial": sn, "workOrder": wo, "reports": reports, "movements": movements})
}

// Batch 按批次追溯（FR-MES-044）。
func (t *TraceAPI) Batch(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var movements []model.StockMovement
	q := t.DB.Scopes(data.ScopeFor(ac, false)).Model(&model.StockMovement{}).
		Where("factory_id=? AND batch_no=?", ac.FactoryID, c.Param("no"))
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(100).Find(&movements)
	var serials []model.SerialNumber
	t.DB.Where("tenant_id=? AND batch_no=?", ac.TenantID, c.Param("no")).Limit(500).Find(&serials)
	response.OK2(c, gin.H{"movements": movements, "serials": serials})
}
