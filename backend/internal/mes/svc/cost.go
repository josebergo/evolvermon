// mes 域服务：工单成本归集（M4；自 api 包下沉，供 wms 完工入库联动调用）。
package svc

import (
	"time"
	"encoding/json"
	"fmt"
	"os"
	"net/http"
	"context"
	"github.com/evolvermon/evolvermon/internal/client"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
)

// CostWorkOrder 计算并回写工单实际成本（可在事务内调用；完工入库过账时自动触发）。
func CostWorkOrder(tx *gorm.DB, ac *middleware.AuthCtx, woID int64) (float64, error) {
	var wo model.WorkOrder
	if err := tx.Scopes(data.ScopeFor(ac, true)).First(&wo, woID).Error; err != nil {
		return 0, err
	}
	var matCost float64
	var picks []model.StockOrder
	if wmsURL := os.Getenv("EM_WMS_URL"); wmsURL != "" {
		// 三库：领料单经 wms 端点（pick-agg 已有 picked 数；成本需行级 done_qty×价——暂用 pick-agg qty 近似
		q, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/core/wms/pick-agg?tenantId=%d&workOrderId=%d", wmsURL, ac.TenantID, woID), nil)
		q.Header.Set("X-Internal-Token", os.Getenv("EM_INTERNAL_TOKEN"))
		if resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(q); err == nil {
			var out struct {
				Data struct {
					Picked []struct {
						MaterialID int64   `json:"materialId"`
						Qty        float64 `json:"qty"`
					} `json:"picked"`
				} `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			for _, pk := range out.Data.Picked {
				mat, _ := client.MatRef(context.Background(), ac.TenantID, pk.MaterialID)
				matCost += pk.Qty * mat.Price
			}
			goto laborPart
		}
	}
	tx.Scopes(data.ScopeFor(ac, true)).Preload("Lines").
		Where("source_type='picking' AND source_doc_id=?", wo.ID).Find(&picks)
	for _, p := range picks {
		for _, l := range p.Lines {
			mat, _ := client.MatRef(context.Background(), ac.TenantID, l.MaterialID)
			matCost += l.DoneQty * mat.Price
		}
	}
	laborPart:
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
	wcs, _ := client.M().WorkCenters(context.Background(), ac.TenantID)
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
