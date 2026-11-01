// cmd/core-mes：erp 域服务（Phase 3 批2）：工单/报工/质检/追溯/看板/打印。
// 中间态：与 core-mes/wms 共享 core 库（拆库在 saga 批次完成后）。
package main

import (
	"log"
	"os"
	"time"

	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/router"
	"github.com/evolvermon/evolvermon/internal/wire"
)

func main() {
	logx.Init("core-mes")
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
		if _, err := data.MigrateVersioned(db, "core"); err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
	}
	wire.Identity()
	in, err := infra.Open(infra.Options{Broker: cfg.Broker, BrokerURL: cfg.BrokerURL, Cache: cfg.Cache, CacheURL: cfg.CacheURL})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}
	_ = in
	client.InitDefaultMaster(db)
	client.InitWMS(db)

	jm := jwtx.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTTLHour, cfg.RefreshTTLDay)
	r := router.New(db, jm, "mes")
	addr := getenvDefault("EM_HTTP_ADDR", ":8087")
	log.Printf("core-mes listening on %s", addr)
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
