// Package metrics 轻量指标采集与 Prometheus 文本格式导出（微服务方案 Phase 0.3）。
//
// 设计原则：不引入 prometheus/client_golang 依赖，自实现计数器 + 直方图 +
// 文本导出（text exposition format v0.0.4），供 ops-admin 抓取与内置看板使用。
// 指标命名前缀 em_（evolvermon）；将来需要更丰富的指标体系时可平滑外接官方客户端。
package metrics

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// durationBuckets HTTP 时延直方图桶（秒）。
	durationBuckets = []float64{0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}

	startAt = time.Now()

	// timeNow 当前时间函数（http.go 使用；测试可替换打桩时延）。
	timeNow = time.Now
)

// Registry 指标注册表（进程内单例即可，锁粒度按指标行）。
type Registry struct {
	mu        sync.Mutex
	counters  map[string]*Counter // key = name + 序列化 labels
	histogram *Histogram          // 全局 HTTP 时延（labels 已编码进 bucket key）
	uptime    time.Time
}

// Counter 带标签的计数器。
type Counter struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Histogram 固定桶直方图，按 label 组合分行。
type Histogram struct {
	rows map[string]*histRow // key = name + labels
}

type histRow struct {
	Name    string
	Labels  map[string]string
	Buckets []float64
	counts  []uint64
	sum     float64
	count   uint64
}

var defaultRegistry = &Registry{
	counters:  map[string]*Counter{},
	histogram: &Histogram{rows: map[string]*histRow{}},
	uptime:    startAt,
}

// Default 进程默认注册表。
func Default() *Registry { return defaultRegistry }

// Inc 计数器自增（labels 需为稳定键值对，如 method/route/status）。
func (r *Registry) Inc(name string, labels map[string]string) {
	key := name + labelKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.counters[key]
	if !ok {
		c = &Counter{Name: name, Labels: labels}
		r.counters[key] = c
	}
	c.Value++
}

// Observe 直方图观测（秒）。
func (r *Registry) Observe(name string, labels map[string]string, v float64) {
	key := name + labelKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.histogram.rows[key]
	if !ok {
		row = &histRow{Name: name, Labels: labels, Buckets: durationBuckets, counts: make([]uint64, len(durationBuckets))}
		r.histogram.rows[key] = row
	}
	for i, b := range row.Buckets {
		if v <= b {
			row.counts[i]++
		}
	}
	row.sum += v
	row.count++
}

// Render 导出 Prometheus 文本格式（含 Go 运行时指标）。
func (r *Registry) Render() string {
	r.mu.Lock()
	var b strings.Builder

	// Go 运行时
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(&b, "# TYPE em_go_goroutines gauge\nem_go_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(&b, "# TYPE em_go_mem_heap_bytes gauge\nem_go_mem_heap_bytes %d\n", ms.HeapAlloc)
	fmt.Fprintf(&b, "# TYPE em_go_mem_sys_bytes gauge\nem_go_mem_sys_bytes %d\n", ms.Sys)
	fmt.Fprintf(&b, "# TYPE em_go_gc_total counter\nem_go_gc_total %d\n", ms.NumGC)
	fmt.Fprintf(&b, "# TYPE em_uptime_seconds gauge\nem_uptime_seconds %.0f\n", time.Since(r.uptime).Seconds())

	// 计数器（按 key 排序保证输出稳定）
	keys := make([]string, 0, len(r.counters))
	for k := range r.counters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := r.counters[k]
		fmt.Fprintf(&b, "# TYPE %s counter\n%s%s %v\n", c.Name, c.Name, labelRender(c.Labels), c.Value)
	}

	// 直方图
	hkeys := make([]string, 0, len(r.histogram.rows))
	for k := range r.histogram.rows {
		hkeys = append(hkeys, k)
	}
	sort.Strings(hkeys)
	for _, k := range hkeys {
		row := r.histogram.rows[k]
		fmt.Fprintf(&b, "# TYPE %s histogram\n", row.Name)
		for i, bound := range row.Buckets {
			fmt.Fprintf(&b, "%s_bucket%s %d\n", row.Name, withLE(row.Labels, fmt.Sprintf("%g", bound)), row.counts[i])
		}
		fmt.Fprintf(&b, "%s_bucket%s %d\n", row.Name, withLE(row.Labels, "+Inf"), row.count)
		fmt.Fprintf(&b, "%s_sum%s %.6f\n", row.Name, labelRender(row.Labels), row.sum)
		fmt.Fprintf(&b, "%s_count%s %d\n", row.Name, labelRender(row.Labels), row.count)
	}
	out := b.String()
	r.mu.Unlock()
	return out
}

// Quantile 估算分位耗时（秒）；无样本时返回 NaN。供 ops-admin/测试直接复用。
func (r *Registry) Quantile(name string, labels map[string]string, q float64) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.histogram.rows[name+labelKey(labels)]
	if !ok || row.count == 0 {
		return math.NaN()
	}
	target := uint64(math.Ceil(float64(row.count) * q))
	var cumulative uint64
	for i, bound := range row.Buckets {
		// counts[i] 是「≤bucket[i]」的累积口径（Observe 已按累积写入）
		cumulative = row.counts[i]
		if cumulative >= target {
			return bound
		}
	}
	return durationBuckets[len(durationBuckets)-1]
}

// labelKey 内部去重键（无引号，仅用于 map key）。
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return "{" + strings.Join(pairs, ",") + "}"
}

// labelRender Prometheus 文本输出格式（带引号）。
func labelRender(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, k+"=\""+v+"\"")
	}
	sort.Strings(pairs)
	return "{" + strings.Join(pairs, ",") + "}"
}

// withLE 在标签渲染中追加 le（直方图 bucket 行）。
func withLE(labels map[string]string, le string) string {
	if len(labels) == 0 {
		return "{le=\"" + le + "\"}"
	}
	pairs := make([]string, 0, len(labels)+1)
	for k, v := range labels {
		pairs = append(pairs, k+"=\""+v+"\"")
	}
	pairs = append(pairs, "le=\""+le+"\"")
	sort.Strings(pairs)
	return "{" + strings.Join(pairs, ",") + "}"
}
