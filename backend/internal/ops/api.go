// ops-admin REST API（Phase 1.5）：/api/v1/ops/*（经网关路由；仅平台管理员）。
package ops

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type API struct {
	Monitor *Monitor
	DB      *gorm.DB
}

// Register 挂载 /api/v1/ops 路由（鉴权由调用方 v1 组中间件承担 + Require(ops)）。
func (a *API) Register(v1 *gin.RouterGroup) {
	g := v1.Group("", middleware.Require("ops:service"))
	g.GET("/ops/services", a.list)
	g.POST("/ops/services/:name/:action", a.control) // start|stop|restart
	g.GET("/ops/services/:name/metrics", a.metrics)
	g.GET("/ops/alerts", a.alerts)
	g.GET("/ops/audit", a.auditLogs)
}

func (a *API) list(c *gin.Context) {
	response.OK2(c, gin.H{"list": a.Monitor.Snapshot()})
}

func (a *API) control(c *gin.Context) {
	name, action := c.Param("name"), c.Param("action")
	if action != "start" && action != "stop" && action != "restart" {
		response.Fail(c, response.NewBiz(response.ErrParam, "action 须为 start|stop|restart"))
		return
	}
	snap := a.Monitor.Snapshot()
	var def *ServiceDef
	for i := range snap {
		if snap[i].Name == name {
			def = &snap[i].ServiceDef
			break
		}
	}
	if def == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "unknown service: " + name})
		return
	}
	ac := middleware.GetAuth(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	err := execDriver(ctx, NewDriver(def.Driver), action, *def)
	result, detail := "ok", ""
	if err != nil {
		result, detail = "failed", err.Error()
	}
	audit(a.DB, ac.UserID, ac.Username, name, action, result, detail)
	if err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, detail))
		return
	}
	// 立即触发一轮健康检查（不等下个周期）
	go a.Monitor.pollAll(ctx)
	response.OK2(c, gin.H{"ok": true, "service": name, "action": action})
}

func (a *API) metrics(c *gin.Context) {
	name := c.Param("name")
	n, _ := strconv.Atoi(c.DefaultQuery("n", "120")) // 默认最近 30 分钟
	response.OK2(c, gin.H{"list": a.Monitor.History(name, n)})
}

func (a *API) alerts(c *gin.Context) {
	var rows []model.OpsAlert
	q := a.DB.Order("opened_at DESC").Limit(100)
	if c.Query("open") == "1" {
		q = q.Where("resolved_at IS NULL")
	}
	q.Find(&rows)
	response.OK2(c, gin.H{"list": rows})
}

func (a *API) auditLogs(c *gin.Context) {
	var rows []model.OpsAuditLog
	a.DB.Order("id DESC").Limit(100).Find(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// execDriver Driver 动作分发。
func execDriver(ctx context.Context, d Driver, action string, def ServiceDef) error {
	switch action {
	case "start":
		return d.Start(ctx, def)
	case "stop":
		return d.Stop(ctx, def)
	case "restart":
		return d.Restart(ctx, def)
	}
	return nil
}
