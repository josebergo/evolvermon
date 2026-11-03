// cmd/master-data：主数据服务（Phase 2，Micro 形态）：物料/BOM/工艺/工作中心/客商 CRUD
// + /internal/master/* 批量读 API（core/scheduler 经 internal/client 调用）。
package main

import (
	"log"
	"os"
	"time"

	"evolvermon/backend/internal/wire"
	"evolvermon/backend/internal/client"
	"evolvermon/backend/internal/config"
	"evolvermon/backend/internal/data"
	"evolvermon/backend/internal/infra"
	mapi "evolvermon/backend/internal/master/internalapi"
	"evolvermon/backend/internal/pkg/jwtx"
	"evolvermon/backend/internal/pkg/logx"
	"evolvermon/backend/seed"
	"evolvermon/backend/internal/router"
)

func main() {
	logx.Init("master-data")
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
	if os.Getenv("EM_MIGRATE") != "0" {
		if _, err := data.MigrateVersioned(db, "master"); err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
		if cfg.SeedDemo {
			if err := seed.RunMasterDemo(db); err != nil { // master 库种子
				log.Printf("seed master demo failed (non-fatal): %v", err)
			}
		}
	}
	in, err := infra.Open(infra.Options{Broker: cfg.Broker, BrokerURL: cfg.BrokerURL, Cache: cfg.Cache, CacheURL: cfg.CacheURL})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}
	_ = in

	client.InitDefaultMaster(db) // 主数据读通道默认装配（Lite=Local 直连）

	jm := jwtx.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTTLHour, cfg.RefreshTTLDay)
	r := router.New(db, jm, "master")
	(&mapi.API{DB: db}).Register(r) // /internal/master/*

	addr := getenvDefault("EM_HTTP_ADDR", ":8085")
	log.Printf("master-data listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
