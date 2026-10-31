// cmd/gateway：Micro 形态统一入口（Phase 1.4）。
// 自研轻量反向代理（零依赖，httputil.ReverseProxy），按路径前缀分流：
//   platform 域（auth/tenants/users/roles/menus/factories/warehouses/locations/
//   audit-logs/params/doc-rules/attachments）→ identity
//   integration 域（webhooks/webhook-deliveries/devices/advisor）→ integration
//   /api/v1/ops/* → ops-admin；其余 /api/v1/* → core
// K8s 形态由 Ingress 取代（Phase 4）；Lite 单进程形态不需要本服务。
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/pkg/metrics"
)

// identityPrefixes identity 域路径前缀（/api/v1 之后段）。
var identityPrefixes = []string{
	"auth", "tenants", "users", "roles", "perms", "menus", "factories", "warehouses",
	"locations", "audit-logs", "params", "doc-rules", "attachments",
}

var integrationPrefixes = []string{"webhooks", "webhook-deliveries", "devices", "advisor"}

// masterPrefixes 主数据域（Phase 2）：materials 等路由至 master-data 服务。
var masterPrefixes = []string{"materials", "boms", "routings", "work-centers", "customers", "suppliers"}

type upstream struct {
	name  string
	proxy *httputil.ReverseProxy
}

type gateway struct {
	identity, core, integration, ops, master *upstream
}

func main() {
	logx.Init("gateway")

	mk := func(name, raw string) *upstream {
		u, err := url.Parse(raw)
		if err != nil {
			log.Fatalf("bad upstream %s=%q: %v", name, raw, err)
		}
		up := &upstream{name: name, proxy: httputil.NewSingleHostReverseProxy(u)}
		up.proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			// fail-fast 语义：上游不可达明确 502，绝不静默换目标
			logx.L().Error("upstream_error", "upstream", name, "path", r.URL.Path, "err", err.Error())
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"code":502,"message":"upstream ` + name + ` unavailable"}`))
		}
		return up
	}

	g := &gateway{
		identity:    mk("identity", getenvDefault("EM_IDENTITY_URL", "http://127.0.0.1:8081")),
		core:        mk("core", getenvDefault("EM_CORE_URL", "http://127.0.0.1:8082")),
		integration: mk("integration", getenvDefault("EM_INTEGRATION_URL", "http://127.0.0.1:8083")),
		ops:         mk("ops-admin", getenvDefault("EM_OPS_URL", "http://127.0.0.1:8180")),
		master:      mk("master-data", getenvDefault("EM_MASTER_URL", "http://127.0.0.1:8085")),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", g.handle)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(metrics.Default().Render()))
	})

	addr := getenvDefault("EM_HTTP_ADDR", ":8080")
	log.Printf("gateway listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func (g *gateway) handle(w http.ResponseWriter, r *http.Request) {
	labels := map[string]string{"route": routeClass(r.URL.Path)}
	start := time.Now()
	metrics.Default().Inc("em_gateway_requests_total", labels)
	if up := g.pick(r.URL.Path); up != nil {
		up.proxy.ServeHTTP(w, r)
	} else {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":404,"message":"no route"}`))
	}
	metrics.Default().Observe("em_gateway_request_duration_seconds", labels, time.Since(start).Seconds())
}

// pick 路由决策（gateway_test.go 覆盖）；非 /api/v1 路径无路由。
func (g *gateway) pick(path string) *upstream {
	if !strings.HasPrefix(path, "/api/v1/") {
		return nil
	}
	rest := strings.TrimPrefix(path, "/api/v1/")
	switch {
	case rest == "ops" || strings.HasPrefix(rest, "ops/"):
		return g.ops
	case matchPrefix(rest, integrationPrefixes):
		return g.integration
	case matchPrefix(rest, masterPrefixes):
		return g.master
	case matchPrefix(rest, identityPrefixes):
		return g.identity
	default:
		return g.core
	}
}

func matchPrefix(rest string, prefixes []string) bool {
	for _, p := range prefixes {
		if rest == p || strings.HasPrefix(rest, p+"/") || strings.HasPrefix(rest, p+"?") {
			return true
		}
	}
	return false
}

// routeClass 指标标签（防爆炸：取 /api/v1 后首段）。
func routeClass(path string) string {
	if !strings.HasPrefix(path, "/api/v1/") {
		return "other"
	}
	rest := strings.TrimPrefix(path, "/api/v1/")
	if i := strings.IndexAny(rest, "/?"); i > 0 {
		return rest[:i]
	}
	return rest
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
