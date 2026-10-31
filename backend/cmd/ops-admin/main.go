// cmd/ops-admin：统一管理平台（Phase 1.5，Micro 形态；Lite 下亦可独立小进程运行）。
// 无状态：registry 来自文件、状态来自探测——本进程宕机不影响任何业务流量。
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/ops"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/pkg/metrics"
)

func main() {
	logx.Init("ops-admin")
	cfg := config.Load()

	db, err := data.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("connect database failed: %v", err)
	}
	for i := 1; i <= 10; i++ {
		if err := db.Exec("SELECT 1").Error; err == nil {
			break
		}
		log.Printf("waiting for database (%d/10)...", i)
		time.Sleep(3 * time.Second)
	}
	if err := ops.EnsureDB(db); err != nil { // ops 表幂等自建（baseline 之外的独立演进）
		log.Fatalf("ops ensuredb failed: %v", err)
	}

	defs, err := ops.LoadRegistry("")
	if err != nil {
		log.Fatalf("%v", err) // fail-fast：registry 不可读直接退出
	}
	monitor := ops.NewMonitor(defs, db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Run(ctx)

	jm := jwtx.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTTLHour, cfg.RefreshTTLDay)

	r := gin.New()
	r.Use(gin.Recovery(), middleware.Trace(), metrics.Middleware(), middleware.Logger(), middleware.CORS(cfg.AllowCors))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if err := db.Exec("SELECT 1").Error; err != nil {
			c.JSON(503, gin.H{"status": "db-down"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	r.GET("/metrics", metrics.Handler())

	v1 := r.Group("/api/v1")
	v1.Use(middleware.Auth(jm, db)) // JWT 鉴权 + 平台管理员角色校验（ops:service）
	api := &ops.API{Monitor: monitor, DB: db}
	api.Register(v1)

	addr := getenvDefault("EM_HTTP_ADDR", ":8090")
	log.Printf("ops-admin listening on %s (%d services registered)", addr, len(defs))
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
	_ = gorm.ErrRecordNotFound
	_ = os.Getenv
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
