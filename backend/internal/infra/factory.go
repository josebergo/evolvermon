package infra

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Options 基础设施选型（来自环境变量，见 internal/config）：
//
//	EM_BROKER=memory|rabbitmq|kafka   消息总线（默认 memory）
//	EM_BROKER_URL=amqp://... | kafka://h1:9092,h2:9092
//	EM_CACHE=memory|redis             缓存与锁（默认 memory）
//	EM_CACHE_URL=redis://user:pass@host:6379/0
type Options struct {
	Broker    string
	BrokerURL string
	Cache     string
	CacheURL  string
}

// Bundle 装配结果：锁跟随缓存提供方（memory 锁 / redis 锁）。
type Bundle struct {
	Broker Broker
	Cache  Cache
	Locker Locker
}

// Open 按选项构建适配器；远端适配器启动即拨号验证（fail-fast），
// 配置拼错、地址不可达直接报错，绝不静默降级到 memory。
func Open(o Options) (*Bundle, error) {
	broker, err := openBroker(o)
	if err != nil {
		return nil, err
	}
	cache, locker, err := openCache(o)
	if err != nil {
		_ = broker.Close()
		return nil, err
	}
	return &Bundle{Broker: broker, Cache: cache, Locker: locker}, nil
}

func openBroker(o Options) (Broker, error) {
	switch strings.ToLower(strings.TrimSpace(o.Broker)) {
	case "", "memory":
		return NewMemoryBroker(), nil
	case "rabbitmq":
		return newRabbitBroker(o.BrokerURL)
	case "kafka":
		return newKafkaBroker(o.BrokerURL)
	default:
		return nil, fmt.Errorf("infra: unknown EM_BROKER %q (memory|rabbitmq|kafka)", o.Broker)
	}
}

func openCache(o Options) (Cache, Locker, error) {
	switch strings.ToLower(strings.TrimSpace(o.Cache)) {
	case "", "memory":
		return NewMemoryCache(), NewMemoryLocker(), nil
	case "redis":
		rc, err := newRedisCache(o.CacheURL)
		if err != nil {
			return nil, nil, fmt.Errorf("infra: redis cache: %w", err)
		}
		return rc, newRedisLocker(rc.cmd), nil
	default:
		return nil, nil, fmt.Errorf("infra: unknown EM_CACHE %q (memory|redis)", o.Cache)
	}
}

// Close 释放全部适配器（幂等性由各适配器保证）。
func (b *Bundle) Close() error {
	var errs []error
	if b.Broker != nil {
		if err := b.Broker.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c, ok := b.Cache.(io.Closer); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// marshalMessage 统一信封序列化（各远端适配器线上格式一致）。
func marshalMessage(m Message) ([]byte, error) { return json.Marshal(m) }

func unmarshalMessage(body []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(body, &m); err != nil {
		return Message{}, err
	}
	return m, nil
}

// 确保端口被适配器实现完整覆盖（编译期检查）。
var (
	_ Broker = (*memBroker)(nil)
	_ Broker = (*rabbitBroker)(nil)
	_ Broker = (*kafkaBroker)(nil)
	_ Cache  = (*memCache)(nil)
	_ Cache  = (*redisCache)(nil)
	_ Locker = (*memLocker)(nil)
	_ Locker = (*redisLocker)(nil)
)
