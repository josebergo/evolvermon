package main

import "testing"

// Phase 1.4：路由决策矩阵——平台/集成/ops/core 四类目标 + 非 API 404。
func TestPick(t *testing.T) {
	g := &gateway{identity: &upstream{name: "identity"}, core: &upstream{name: "core"},
		integration: &upstream{name: "integration"}, ops: &upstream{name: "ops"}, master: &upstream{name: "master"},
		coreERP: &upstream{name: "erpx"}, coreMES: &upstream{name: "mesx"}, coreWMS: &upstream{name: "wmsx"}}
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/auth/login", "identity"},
		{"/api/v1/auth/refresh", "identity"},
		{"/api/v1/users", "identity"},
		{"/api/v1/users/1/roles", "identity"},
		{"/api/v1/tenants", "identity"},
		{"/api/v1/factories/3", "identity"},
		{"/api/v1/warehouses", "master"},
		{"/api/v1/audit-logs", "identity"},
		{"/api/v1/attachments/9/download", "identity"},
		{"/api/v1/webhooks", "integration"},
		{"/api/v1/webhook-deliveries", "integration"},
		{"/api/v1/devices/CNC-01/telemetry", "integration"},
		{"/api/v1/advisor/insights", "integration"},
		{"/api/v1/ops/services", "ops"},
		{"/api/v1/ops/services/core/stop", "ops"},
		{"/api/v1/materials", "master"},
		{"/api/v1/materials/2/where-used", "master"},
		{"/api/v1/boms/1/tree", "master"},
		{"/api/v1/work-centers", "master"},
		{"/api/v1/customers/7", "master"},
		{"/api/v1/work-orders/5", "mesx"},
		{"/api/v1/stock-orders/1/post-lines", "wmsx"},
		{"/api/v1/mrp/run", "erpx"},
		{"/api/v1/dashboard/summary", "mesx"},
		{"/healthz", ""},
		{"/assets/app.js", ""},
	}
	for _, c := range cases {
		var got string
		if up := g.pick(c.path); up != nil {
			got = up.name
		}
		if got != c.want {
			t.Errorf("pick(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
