// Package config 读取环境变量与默认值（NFR-033：敏感配置只走环境变量）。
package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr      string // 监听地址
	Env           string // dev | prod
	DatabaseDSN   string
	JWTSecret     string
	JWTIssuer     string
	AccessTTLHour int
	RefreshTTLDay int
	SeedDemo      bool // 启动时写入演示数据
	AllowCors     string
	// 基础设施选型（internal/infra）：默认全 memory，零外部依赖。
	Broker    string // memory | rabbitmq | kafka
	BrokerURL string // amqp://... 或 kafka://h1:9092,h2:9092
	Cache     string // memory | redis
	CacheURL  string // redis://...
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Load() *Config {
	return &Config{
		HTTPAddr:      getenv("EM_HTTP_ADDR", ":8080"),
		Env:           getenv("EM_ENV", "dev"),
		DatabaseDSN:   getenv("EM_DATABASE_DSN", "host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon sslmode=disable TimeZone=Asia/Shanghai"),
		JWTSecret:     getenv("EM_JWT_SECRET", "change-me-in-production"),
		JWTIssuer:     "evolvermon",
		AccessTTLHour: getenvInt("EM_ACCESS_TTL_HOUR", 2),
		RefreshTTLDay: getenvInt("EM_REFRESH_TTL_DAY", 7),
		SeedDemo:      getenv("EM_SEED_DEMO", "true") == "true",
		AllowCors:     getenv("EM_ALLOW_CORS", "*"),
		Broker:        getenv("EM_BROKER", "memory"),
		BrokerURL:     getenv("EM_BROKER_URL", ""),
		Cache:         getenv("EM_CACHE", "memory"),
		CacheURL:      getenv("EM_CACHE_URL", ""),
	}
}

// MrpAutoMinutes 定时 MRP 间隔（分钟），0=关闭（FR-ERP-077）。
func MrpAutoMinutes() int { return getenvInt("EM_MRP_AUTO_MINUTES", 0) }
