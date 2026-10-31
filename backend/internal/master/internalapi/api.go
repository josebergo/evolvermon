// Package internalapi master-data 服务内部 API（Phase 2.3/2.4）：供 core/scheduler
// 经网内调用读取主数据，/internal/* 不对外路由；X-Internal-Token 服务间认证。
// BOM 展开为 MRP 试点（执行方案 2.4：scheduler 的 MRP 改走 master-data API）。
package internalapi

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

// BomEdge BOM 边（MRP 净需求展开输入：parent→child + 单耗 + 损耗率）。
type BomEdge struct {
	ParentID int64   `json:"parentId"`
	ChildID  int64   `json:"childId"`
	Qty      float64 `json:"qty"`
	Scrap    float64 `json:"scrap"`
}

// MaterialLite 物料精简视图（跨域读通道 DTO）。
type MaterialLite struct {
	ID           int64   `json:"id"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Category     string  `json:"category"`
	Price        float64 `json:"price"`
	SafetyStock  float64 `json:"safetyStock"`
	SerialManaged bool   `json:"serialManaged"`
}

type API struct{ DB *gorm.DB }

// Register 挂载 /internal/master/*（独立于业务路由，仅服务间调用）。
func (a *API) Register(r *gin.Engine) {
	g := r.Group("/internal/master", internalToken())
	g.GET("/bom-edges", a.BomEdges)
	g.GET("/materials", a.Materials)
}

// internalToken 服务间令牌（EM_INTERNAL_TOKEN，各服务配置一致；fail-fast：未配置拒绝）。
func internalToken() gin.HandlerFunc {
	token := os.Getenv("EM_INTERNAL_TOKEN")
	return func(c *gin.Context) {
		if token == "" || c.GetHeader("X-Internal-Token") != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "bad internal token"})
			return
		}
		c.Next()
	}
}

// BomEdges 全量启用 BOM 边（MRP 每轮一次批量拉取）。
func (a *API) BomEdges(c *gin.Context) {
	tenantID := queryInt64(c, "tenantId")
	var edges []BomEdge
	a.DB.Raw(`SELECT b.material_id parent_id, l.material_id child_id, l.qty, l.scrap_rate
		FROM master.boms b JOIN master.bom_lines l ON l.bom_id=b.id
		WHERE b.tenant_id=? AND b.status='active' AND b.deleted_at IS NULL`, tenantID).Scan(&edges)
	response.OK2(c, gin.H{"list": edges})
}

// Materials 按 ids 批量取物料（逗号分隔；空=全量精简视图）。
func (a *API) Materials(c *gin.Context) {
	tenantID := queryInt64(c, "tenantId")
	q := a.DB.Table("master.materials").
		Select("id, code, name, category, price, safety_stock, serial_managed").
		Where("tenant_id=? AND deleted_at IS NULL", tenantID)
	if ids := c.Query("ids"); ids != "" {
		q = q.Where("id IN (" + ids + ")") // ids 为内部服务传参（int64 逗号串），不做用户输入
	}
	var rows []MaterialLite
	q.Scan(&rows)
	response.OK2(c, gin.H{"list": rows})
}

func queryInt64(c *gin.Context, k string) int64 {
	var v int64
	for _, ch := range c.Query(k) {
		if ch < '0' || ch > '9' {
			return 0
		}
		v = v*10 + int64(ch-'0')
	}
	return v
}
