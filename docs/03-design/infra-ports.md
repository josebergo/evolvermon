# 基础设施端口层设计（Infra Ports & Adapters）

> 对应实现：`backend/internal/infra/`。目标：默认部署零外部中间件（单机 docker compose / 开发机直跑），
> 需要水平扩展或事件流接入时，仅改环境变量即可切换到 RabbitMQ / Kafka / Redis，业务代码不感知。

## 1. 动机与原则

- **默认零依赖**：开源免费自托管（NFR-020 形态①）不能强制用户装 MQ；默认全部 `memory` 适配器（进程内实现）。
- **端口-适配器（六边形架构）**：业务代码只依赖 `infra.Broker / infra.Cache / infra.Locker` 接口，适配器按环境变量装配。
- **fail-fast，不静默降级**：显式配置了 `EM_BROKER=rabbitmq` 而连接不可达 → 启动即失败；绝不悄悄退回 memory 造成"看起来正常"。
- **语义对齐**：所有 Broker 适配器统一消息信封 `infra.Message`（JSON），跨中间件迁移时消费端格式不变。

## 2. 端口定义

```go
// infra.Message 统一信封：ID/Topic/Key/TenantID/Event/Payload/Timestamp
type Broker interface {
    Name() string
    Publish(ctx, m Message) error
    Subscribe(ctx, topic, group string, h Handler) error // 非阻塞，后台消费
    Close() error
}

type Cache interface { // 字节值 + TTL（<=0 永不过期）
    Name() string
    Get(ctx, key) (val []byte, ok bool, err error)
    Set(ctx, key, val, ttl) error
    Del(ctx, keys ...string) error
}

type Locker interface { // 分布式锁：TTL 到期自动释放（持有方崩溃不死锁）
    Name() string
    Acquire(ctx, name, ttl) (release func(), ok bool, err error)
}
```

锁（Locker）跟随缓存（Cache）提供方：`memory / memory` 或 `redis / redis`，不单独配置。

## 3. 适配器矩阵

| 端口 | memory（默认） | rabbitmq | kafka | redis |
|---|---|---|---|---|
| Broker | ✅ 进程内 pub/sub | ✅ topic 交换机 `evolvermon`，durable 队列 `em:{group}.{topic}`，手动 ack，断线自动重连 | ✅ 消费组，acks=all，成功才提交位点 | — |
| Cache | ✅ map+惰性过期 | — | — | ✅ GET/SET/DEL |
| Locker | ✅ 进程内互斥 | — | — | ✅ SETNX+token，Lua 比对释放 |
| 投递语义 | 尽力（慢消费者丢弃并记日志） | at-least-once，内联重试 3 次后丢弃 | at-least-once，内联重试 3 次后丢弃 | — |

消息失败策略：毒消息（handler 持续失败）重试 3 次后**记日志丢弃**，防止无限重投阻塞队列（后续版本可加死信队列）。

## 4. 环境变量

| 变量 | 取值 | 默认 | 说明 |
|---|---|---|---|
| `EM_BROKER` | `memory` \| `rabbitmq` \| `kafka` | `memory` | 消息总线 |
| `EM_BROKER_URL` | `amqp://user:pass@host:5672/vhost`；`kafka://h1:9092,h2:9092`（前缀可省） | — | broker 非 memory 时必填 |
| `EM_CACHE` | `memory` \| `redis` | `memory` | 缓存 + 分布式锁 |
| `EM_CACHE_URL` | `redis://user:pass@host:6379/0` | — | cache 为 redis 时必填 |

启动日志会打印装配结果：`infra ready: broker=rabbitmq cache=redis(locker=redis)`。

## 5. 现有接线

- **领域事件双写**（`internal/events`）：`Publish` 原有 DB 投递箱（webhook 出口，事务安全）不变，
  叠加推送 `infra.TopicEvents`（`evolvermon.events`）主题，Key=`tenant-{id}`（同租户事件同分区/同队列保序）。
  消费示例：实时看板推送、外部数据管道、缓存失效广播。
- **定时 MRP 互斥**（`cmd/server/main.go`）：`EM_MRP_AUTO_MINUTES>0` 时每轮先 `Locker.Acquire("mrp-auto")`，
  多副本部署下同一时刻只有一个实例执行；单机 memory 锁恒成功，行为不变。

订阅示例（业务代码）：

```go
bundle.Broker.Subscribe(ctx, infra.TopicEvents, "my-consumer",
    func(ctx context.Context, m infra.Message) error {
        log.Printf("tenant=%d event=%s payload=%s", m.TenantID, m.Event, m.Payload)
        return nil
    })
```

通配订阅：RabbitMQ 绑定键支持 `workorder.*`；Kafka 为整 topic 订阅（按消费组负载均衡）。

## 6. 本地联调（docker compose 可选 profile）

```bash
docker compose --profile full up -d          # 带上 redis/rabbitmq/kafka
export EM_BROKER=rabbitmq EM_BROKER_URL=amqp://guest:guest@127.0.0.1:5672/
export EM_CACHE=redis    EM_CACHE_URL=redis://127.0.0.1:6379/0
./evolvermon
```

适配器集成测试同样需要真实服务，未设置时自动跳过：

```bash
EM_TEST_RABBITMQ_URL=amqp://guest:guest@127.0.0.1:5672/ go test ./internal/infra/
EM_TEST_KAFKA_URL=kafka://127.0.0.1:9092        go test ./internal/infra/
EM_TEST_REDIS_URL=redis://127.0.0.1:6379/0      go test ./internal/infra/
```

## 7. 扩展新适配器（如 Pulsar / RocketMQ）

1. 新建 `internal/infra/pulsar.go`，实现 `Broker` 三个方法 + `Name()`；
2. `factory.go` 的 `openBroker` 加一个 case（URL 校验 + 拨号验证）；
3. 补 `infra_test.go` 往返测试（无服务时 skip）。

约定：消息体必须是 `marshalMessage(Message)` 的 JSON，保证信封跨适配器一致。
