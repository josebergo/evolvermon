// cmd/master-data：主数据服务（Phase 2，Micro 形态）：物料/BOM/工艺/工作中心/客商 CRUD
// + /internal/master/* 批量读 API（core/scheduler 经 internal/client 调用）。
package main

import (
	"log"
	"os"
	"time"

	"github.com/evolvermon/evolvermon/internal/wire"
	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/infra"
	mapi "github.com/evolvermon/evolvermon/internal/master/internalapi"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/router"
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
		if _, err := data.MigrateVersioned(db); err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
	}
	in, err := infra.Open(infra.Options{Broker: cfg.Broker, BrokerURL: cfg.BrokerURL, Cache: cfg.Cache, CacheURL: cfg.CacheURL})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}
	_ = in

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
