// cmd/integration：integration 服务（Phase 1.2，Micro 形态）。
// 承载 m6 域（webhook 出口/设备遥测/智能建议）+ webhook 投递 worker。
// 事件入口为共享库投递箱（integration schema）与 evolvermon.events 总线；本进程
// 与核心交易完全解耦——被杀掉不影响单据流转（verify-micro.sh 故障隔离断言）。
package main

import (
	"log"
	"os"
	"time"

	"github.com/evolvermon/evolvermon/internal/wire"
	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/integration/fanout"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/router"
)

func main() {
	logx.Init("integration")
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
		if _, err := data.MigrateVersioned(db, "integration"); err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
	} else {
		log.Printf("migrate skipped (EM_MIGRATE=0)")
	}

	client.InitDefaultMaster(db) // 主数据读通道默认装配（Lite=Local 直连）

	jm := jwtx.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTTLHour, cfg.RefreshTTLDay)
	in, err := infra.Open(infra.Options{Broker: cfg.Broker, BrokerURL: cfg.BrokerURL, Cache: cfg.Cache, CacheURL: cfg.CacheURL})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}
	events.UseBroker(in.Broker)

	events.StartWorker(db) // webhook 投递（integration schema 投递箱，跨进程共享库轮询）
	if coreURL := getenvDefault("EM_CORE_URL", ""); coreURL != "" {
		fanout.Start(db, coreURL) // Phase 2 分库：拉取 core outbox → 自家库扇出 webhook
	}
	r := router.New(db, jm, "integration")
	addr := getenvDefault("EM_HTTP_ADDR", ":8083")
	log.Printf("integration listening on %s", addr)
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
