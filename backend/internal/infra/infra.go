// Package infra 基础设施端口层（ports & adapters）：消息总线 / 缓存 / 分布式锁。
//
// 端口（接口）由业务侧依赖，适配器按部署形态插拔：
//   - memory（默认）：进程内实现，零外部依赖，单机/开发/测试用；
//   - rabbitmq / kafka / redis：通过 EM_BROKER / EM_CACHE 环境变量启用（factory.go）。
//
// 默认部署（docker compose 单机）不需要任何外部中间件；需要水平扩展时
// 换 Broker/Cache 配置即可，业务代码不感知具体实现。
package infra

import (
	"context"
	"encoding/json"
	"time"
)

// 消息主题：领域事件统一走单一 topic，事件名放 Event 字段（便于消费端按需过滤）。
const (
	TopicEvents = "evolvermon.events" // 领域事件（events.Publish 双写：DB 投递箱 + 总线）
)

// Message 统一消息信封：所有 Broker 适配器的线上格式（JSON）。
type Message struct {
	ID        string          `json:"id"`    // 消息唯一 ID（uuid）
	Topic     string          `json:"topic"` // 主题
	Key       string          `json:"key"`   // 路由/分区键（如 tenant-1；为空由适配器决定）
	TenantID  int64           `json:"tenantId"`
	Event     string          `json:"event"`   // 事件名（如 stock.posted）
	Payload   json.RawMessage `json:"payload"` // JSON 负载
	Timestamp time.Time       `json:"ts"`
}

// Handler 消费回调：返回 nil 才确认（ack/commit），错误按适配器重试策略处理。
type Handler func(ctx context.Context, m Message) error

// Broker 消息总线端口（发布/订阅）。
// 语义：at-least-once（rabbitmq/kafka）；memory 为尽力投递（消费者慢则丢弃并记日志），
// 仅用于单机。Subscribe 非阻塞：后台起消费协程，ctx 取消即停。
type Broker interface {
	Name() string
	Publish(ctx context.Context, m Message) error
	Subscribe(ctx context.Context, topic, group string, h Handler) error
	Close() error
}

// Cache 缓存端口：字节值 + TTL（ttl<=0 表示不过期）。键建议加租户前缀。
type Cache interface {
	Name() string
	Get(ctx context.Context, key string) (val []byte, ok bool, err error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// Locker 分布式锁端口：Acquire 非阻塞，拿到锁返回 release；TTL 到期自动释放
// （持有方崩溃不会死锁）。用于多副本部署下保护定时任务等临界区。
type Locker interface {
	Name() string
	Acquire(ctx context.Context, name string, ttl time.Duration) (release func(), ok bool, err error)
}
