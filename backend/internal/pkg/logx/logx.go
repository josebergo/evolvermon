// Package logx 结构化 JSON 日志（微服务方案 Phase 0.4：slog + service 维度）。
//
// 所有服务（Lite 单进程与 Micro 各 cmd/<svc>）统一经 Init(service) 初始化，
// 日志行自带 service 字段，供 ops-admin/后续 Loki 聚合按服务过滤。
package logx

import (
	"log/slog"
	"os"
)

var logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

// Init 初始化进程日志器（service 名进每行日志）。幂等，重复调用覆盖。
func Init(service string) {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).
		With("service", service)
	slog.SetDefault(logger)
}

// L 返回当前日志器。
func L() *slog.Logger { return logger }
