package api

// 主数据扩展：BOM 树/环校验/启用切换、物料反查（FR-ERP-020~027）、工作台/看板/追溯。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type MasterAPI struct{ DB *gorm.DB }

// BomTree FR-ERP-022 多级树形展开。
func (m *MasterAPI) BomTree(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var bom model.Bom
	if err := m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Lines").First(&bom, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "BOM 不存在"))
		return
	}
	var mats []model.Material
	m.DB.Scopes(data.ScopeFor(ac, false)).Find(&mats)
	matName := map[int64]model.Material{}
	for _, mt := range mats {
		matName[mt.ID] = mt
	}
	type node struct {
		MaterialID int64   `json:"materialId"`
		Code       string `json:"code"`
		Name       string `json:"name"`
		Qty        float64 `json:"qty"`
		ScrapRate  float64 `json:"scrapRate"`
		Unit       string  `json:"unit"`
		Children   []any   `json:"children"`
	}
	var build func(bomID int64, factor float64, depth int, path map[int64]bool) []any
	build = func(bomID int64, factor float64, depth int, path map[int64]bool) []any {
		if depth > 10 {
			return nil
		}
		var b model.Bom
		m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Lines").First(&b, bomID)
		var out []any
		for _, l := range b.Lines {
			mat := matName[l.MaterialID]
			n := node{
				MaterialID: l.MaterialID, Code: mat.Code, Name: mat.Name,
				Qty: factor * l.Qty, ScrapRate: l.ScrapRate, Unit: mat.Unit,
			}
			// 子层：子项若为自制件且有启用 BOM，继续展开
			if mat.SourceType == model.SrcMake && !path[l.MaterialID] {
				var child model.Bom
				if err := m.DB.Scopes(data.ScopeFor(ac, false)).
					Where("material_id=? AND status='active'", l.MaterialID).First(&child).Error; err == nil {
					p2 := map[int64]bool{}
					for k, v := range path {
						p2[k] = v
					}
					p2[l.MaterialID] = true
					n.Children = build(child.ID, factor*l.Qty*(1+l.ScrapRate/100), depth+1, p2)
				}
			}
			out = append(out, n)
		}
		return out
	}
	root := matName[bom.MaterialID]
	tree := node{
		MaterialID: bom.MaterialID, Code: root.Code, Name: root.Name, Qty: 1, Unit: root.Unit,
		Children: build(bom.ID, 1, 0, map[int64]bool{bom.MaterialID: true}),
	}
	response.OK2(c, tree)
}

// BomValidate FR-ERP-023 环引用校验（保存前由前端调用或 hook 调用）。
func (m *MasterAPI) BomValidate(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		BomID     int64 `json:"bomId"`
		ParentID  int64 `json:"parentId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	// 自根向下收集所有子项，检测是否出现 parent
	visits := 0
	var check func(id int64, seen map[int64]bool) bool
	check = func(id int64, seen map[int64]bool) bool {
		visits++
		if visits > 500 {
			return false
		}
		var b model.Bom
		if err := m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Lines").
			Where("id <> ? AND material_id=? AND status IN ('draft','active')", req.BomID, id).First(&b).Error; err != nil {
			return true
		}
		for _, l := range b.Lines {
			if l.MaterialID == req.ParentID {
				return false
			}
			if seen[l.MaterialID] {
				continue
			}
			seen[l.MaterialID] = true
			if !check(l.MaterialID, seen) {
				return false
			}
		}
		return true
	}
	for _, l := range mustBomLines(m.DB, ac, req.BomID) {
		if l.MaterialID == req.ParentID {
			response.Fail(c, response.NewBiz(response.ErrBomCycle, "检测到直接环引用：父项出现在子项中"))
			return
		}
		if ok := check(l.MaterialID, map[int64]bool{l.MaterialID: true}); !ok {
			response.Fail(c, response.NewBiz(response.ErrBomCycle, "检测到多级环引用"))
			return
		}
	}
	response.OK2(c, gin.H{"ok": true})
}

func mustBomLines(db *gorm.DB, ac *middleware.AuthCtx, bomID int64) []model.BomLine {
	if bomID == 0 {
		return nil
	}
	var b model.Bom
	db.Scopes(data.ScopeFor(ac, false)).Preload("Lines").First(&b, bomID)
	return b.Lines
}

// BomEnable FR-ERP-021 唯一启用版本：切换即停用旧版。
func (m *MasterAPI) BomEnable(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var bom model.Bom
	if err := m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Lines").First(&bom, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "BOM 不存在"))
		return
	}
	if len(bom.Lines) == 0 {
		response.Fail(c, response.NewBiz(response.ErrBiz, "空 BOM 不可启用"))
		return
	}
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Bom{}).
			Where("material_id=? AND status='active' AND id<>?", bom.MaterialID, bom.ID).
			Update("status", model.BomOff).Error; err != nil {
			return err
		}
		return tx.Model(&model.Bom{}).Where("id=?", bom.ID).Update("status", model.BomActive).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(m.DB, ac, c.ClientIP(), "enable", "bom", i64s(bom.ID), bom.VersionNo, "", "active")
	response.OK2(c, gin.H{"ok": true})
}

// WhereUsed FR-ERP-003 物料反查（哪里用到）。
func (m *MasterAPI) WhereUsed(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var rows []struct {
		BomID      int64  `json:"bomId"`
		VersionNo  string `json:"versionNo"`
		Status     string `json:"status"`
		ParentID   int64  `json:"parentId"`
		ParentCode string `json:"parentCode"`
		ParentName string `json:"parentName"`
		Qty        float64 `json:"qty"`
	}
	m.DB.Raw(`SELECT b.id bom_id, b.version_no, b.status, b.material_id parent_id,
		mt.code parent_code, mt.name parent_name, l.qty
		FROM bom_lines l
		JOIN boms b ON b.id = l.bom_id AND b.tenant_id = ? AND b.deleted_at IS NULL
		JOIN materials mt ON mt.id = b.material_id
		WHERE l.material_id = ?`, ac.TenantID, c.Param("id")).Scan(&rows)
	response.OK2(c, rows)
}

// TraceBatch FR-MES-044 批次/工单追溯链。
func (m *MasterAPI) TraceWorkOrder(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var wo model.WorkOrder
	if err := m.DB.Scopes(data.ScopeFor(ac, true)).First(&wo, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "工单不存在"))
		return
	}
	// 报工链
	var reports []model.ProductionReport
	m.DB.Where("work_order_id=?", wo.ID).Order("reported_at").Find(&reports)
	// 领料链（出库单）→ 批次 → 采购来源
	var picks []model.StockOrder
	m.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").
		Where("source_type='picking' AND source_doc_id=?", wo.ID).Find(&picks)
	var movements []model.StockMovement
	m.DB.Where("source_doc_type='stock_order' AND source_doc_id IN ?", pickIDs(picks)).Find(&movements)
	response.OK2(c, gin.H{
		"workOrder": wo,
		"reports":   reports,
		"pickings":  picks,
		"batchFlow": movements,
	})
}

func pickIDs(ps []model.StockOrder) []int64 {
	ids := []int64{}
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	return ids
}
