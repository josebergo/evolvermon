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

	"evolvermon/backend/internal/wire"
	"evolvermon/backend/internal/client"
	"evolvermon/backend/internal/config"
	"evolvermon/backend/internal/data"
	"evolvermon/backend/internal/middleware"
	"evolvermon/backend/internal/ops"
	"evolvermon/backend/internal/pkg/jwtx"
	"evolvermon/backend/internal/pkg/logx"
	"evolvermon/backend/internal/pkg/metrics"
)

func main() {
	logx.Init("ops-admin")
	wire.Identity() // Phase 2：identity 远程通道（单号/审计/认证上下文；未设 EM_IDENTITY_URL 时 no-op）
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

	client.InitDefaultMaster(db) // 主数据读通道默认装配（Lite=Local 直连）

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
