// Package internalapi core 服务内部 API（Phase 2 分库）：integration 拉取事件（webhook 扇出）、
// identity 校验工厂停用（查 core 单据表）。/internal/* 仅服务间调用。
package internalapi

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type API struct{ DB *gorm.DB }

// Register 挂载 /internal/core/*。
func (a *API) Register(r *gin.Engine) {
	g := r.Group("/internal/core", internalToken())
	g.GET("/events", a.Events)
	g.GET("/factory-check", a.FactoryCheck)
}

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

// Events integration 拉取事件（outbox 增量：id>after，limit≤500）。扇出方按 messageId 幂等。
func (a *API) Events(c *gin.Context) {
	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.OutboxMessage
	a.DB.Select("id, message_id, tenant_id, event, payload").
		Where("id > ? AND sent_at IS NOT NULL", after).Order("id").Limit(limit).Find(&rows)
	if rows == nil {
		rows = []model.OutboxMessage{}
	}
	response.OK2(c, gin.H{"list": rows})
}

// FactoryCheck 工厂停用校验（identity 侧远程调用）：返回未结单据数，>0 禁止停用。
func (a *API) FactoryCheck(c *gin.Context) {
	factoryID, _ := strconv.ParseInt(c.Query("factoryId"), 10, 64)
	if factoryID == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, "factoryId required"))
		return
	}
	var wo, so int64
	a.DB.Model(&model.WorkOrder{}).Where("factory_id=? AND status IN ('draft','released','running','paused','finished')", factoryID).Count(&wo)
	a.DB.Model(&model.StockOrder{}).Where("factory_id=? AND status IN ('draft','doing')", factoryID).Count(&so)
	response.OK2(c, gin.H{"openDocs": wo + so})
}
