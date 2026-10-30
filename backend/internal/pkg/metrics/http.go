// Package metrics HTTP 侧：Gin 采集中间件 + /metrics 导出端点（Phase 0.3）。
package metrics

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	reqTotalName = "em_http_requests_total"
	reqDurName   = "em_http_request_duration_seconds"
)

// Middleware 采集每请求 QPS/时延/状态码（路由用注册模板而非真实路径，防标签爆炸）。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := timeNow()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		labels := map[string]string{
			"method": c.Request.Method,
			"route":  route,
		}
		Default().Inc(reqTotalName, withStatus(labels, c.Writer.Status()))
		Default().Observe(reqDurName, labels, timeNow().Sub(start).Seconds())
	}
}

// Handler /metrics 端点（仅内网/ops-admin 抓取，网关不对外暴露）。
func Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.String(http.StatusOK, "%s", Default().Render())
	}
}

func withStatus(labels map[string]string, code int) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out["status"] = strconv.Itoa(code / 100) + "xx" // 2xx/4xx/5xx 聚合
	return out
}
