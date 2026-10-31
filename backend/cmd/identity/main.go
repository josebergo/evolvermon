// cmd/identity：identity 服务（Phase 1.1，Micro 形态）。
// 承载 platform 域：认证/租户/用户/角色/工厂/仓区/审计/参数/单号规则/附件。
// Micro 拓扑中负责迁移与种子（EM_MIGRATE 默认开启；其余服务 EM_MIGRATE=0）。
package main

import (
	"log"
	"os"
	"time"

	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	piapi "github.com/evolvermon/evolvermon/internal/platform/internalapi"
	"github.com/evolvermon/evolvermon/internal/router"
	"github.com/evolvermon/evolvermon/seed"
)

func main() {
	logx.Init("identity")
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
		version, err := data.MigrateVersioned(db, "identity")
		if err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
		log.Printf("schema at version %d", version)
	}
	if cfg.SeedDemo {
		if err := seed.RunPlatform(db); err != nil {
			log.Printf("seed failed (non-fatal): %v", err)
		}
	}

	jm := jwtx.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTTLHour, cfg.RefreshTTLDay)
	in, err := infra.Open(infra.Options{Broker: cfg.Broker, BrokerURL: cfg.BrokerURL, Cache: cfg.Cache, CacheURL: cfg.CacheURL})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}
	_ = in // identity 自身不消费总线；保持端口层装配校验（fail-fast）

	r := router.New(db, jm, "platform")
	(&piapi.API{DB: db}).Register(r) // /internal/identity/*（附录 C：认证上下文/单号/审计）
	addr := getenvDefault("EM_HTTP_ADDR", ":8081")
	log.Printf("identity listening on %s", addr)
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
