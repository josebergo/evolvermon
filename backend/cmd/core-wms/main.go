// cmd/core-wms：erp 域服务（Phase 3 批2）：库存/出入库/盘点/预占/调拨/序列号。
// 中间态：与 core-mes/wms 共享 core 库（拆库在 saga 批次完成后）。
package main

import (
	"log"
	"os"
	"time"

	ciapi "github.com/evolvermon/evolvermon/internal/core/internalapi"
	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/outbox"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/router"
	"github.com/evolvermon/evolvermon/seed"
	"github.com/evolvermon/evolvermon/internal/wire"
)

func main() {
	logx.Init("core-wms")
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
		if cfg.SeedDemo {
			if err := seed.RunCoreSplit(db); err != nil { // core 库种子（IQC 方案/期初库存/字典）
				log.Printf("seed core split failed (non-fatal): %v", err)
			}
		}
	}
	wire.Identity()
	in, err := infra.Open(infra.Options{Broker: cfg.Broker, BrokerURL: cfg.BrokerURL, Cache: cfg.Cache, CacheURL: cfg.CacheURL})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}
	events.UseBroker(in.Broker)
	outbox.StartRelay(db, in.Broker) // split 形态：core 域事件 relay 由 core-wms 承载（共享库单点）
	client.InitDefaultMaster(db)
	client.InitWMS(db)
	client.InitCoreMes(db) // core 域间读通道（Local 直连）
	client.InitCoreErp(db)
	client.InitWMSWriter(db) // wms 写通道（Local 直连）

	jm := jwtx.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTTLHour, cfg.RefreshTTLDay)
	r := router.New(db, jm, "wms")
	(&ciapi.API{DB: db}).Register(r) // /internal/core/*（三域共享库期由 core-wms 承载）
	addr := getenvDefault("EM_HTTP_ADDR", ":8088")
	log.Printf("core-wms listening on %s", addr)
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
