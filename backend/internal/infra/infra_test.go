package infra

import (
	"context"
	"os"
	"testing"
	"time"
)

// ---- 内存总线 ----

func TestMemoryBrokerPubSub(t *testing.T) {
	b := NewMemoryBroker()
	defer b.Close()
	ctx := context.Background()
	got := make(chan Message, 4)
	if err := b.Subscribe(ctx, TopicEvents, "test", func(_ context.Context, m Message) error {
		got <- m
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Publish(ctx, Message{ID: "m1", Topic: TopicEvents, Event: "stock.posted", Key: "tenant-1", TenantID: 1, Payload: []byte(`{"docNo":"SO1"}`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m.Event != "stock.posted" || m.TenantID != 1 || string(m.Payload) != `{"docNo":"SO1"}` {
			t.Fatalf("unexpected message: %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no message received")
	}
}

// ---- 内存缓存 ----

func TestMemoryCache(t *testing.T) {
	c := NewMemoryCache()
	ctx := context.Background()
	if err := c.Set(ctx, "k", []byte("v1"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := c.Get(ctx, "k"); !ok || string(v) != "v1" {
		t.Fatalf("get: ok=%v v=%q", ok, v)
	}
	if err := c.Set(ctx, "k", []byte("v2"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("expired key should miss")
	}
	_ = c.Set(ctx, "del1", []byte("x"), 0)
	_ = c.Set(ctx, "del2", []byte("x"), 0)
	if err := c.Del(ctx, "del1", "del2"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get(ctx, "del1"); ok {
		t.Fatal("deleted key should miss")
	}
}

// ---- 内存锁 ----

func TestMemoryLocker(t *testing.T) {
	l := NewMemoryLocker()
	ctx := context.Background()
	release, ok, err := l.Acquire(ctx, "mrp", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	if _, ok2, _ := l.Acquire(ctx, "mrp", time.Minute); ok2 {
		t.Fatal("second acquire should fail while held")
	}
	release()
	if _, ok3, _ := l.Acquire(ctx, "mrp", time.Minute); !ok3 {
		t.Fatal("acquire after release should succeed")
	}
	// TTL 过期自动释放（持有方崩溃不死锁）
	if _, ok4, _ := l.Acquire(ctx, "ttl-lock", 30*time.Millisecond); !ok4 {
		t.Fatal("ttl acquire should succeed")
	}
	time.Sleep(60 * time.Millisecond)
	if _, ok5, _ := l.Acquire(ctx, "ttl-lock", time.Minute); !ok5 {
		t.Fatal("acquire after ttl expiry should succeed")
	}
}

// ---- 工厂：默认 memory、非法配置 fail-fast ----

func TestOpenDefaults(t *testing.T) {
	b, err := Open(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Broker.Name() != "memory" || b.Cache.Name() != "memory" || b.Locker.Name() != "memory" {
		t.Fatalf("defaults should be memory: %s/%s/%s", b.Broker.Name(), b.Cache.Name(), b.Locker.Name())
	}
}

func TestOpenRejectsBadOptions(t *testing.T) {
	cases := []Options{
		{Broker: "pulsar"},
		{Cache: "memcached"},
		{Broker: "rabbitmq"},                    // 缺 URL
		{Broker: "kafka", BrokerURL: "  "},      // 缺地址
		{Cache: "redis", CacheURL: "not-a-url"}, // URL 非法
	}
	for i, o := range cases {
		if _, err := Open(o); err == nil {
			t.Fatalf("case %d (%+v) should fail", i, o)
		}
	}
}

// ---- 信封序列化 ----

func TestMessageEnvelope(t *testing.T) {
	m := Message{ID: "id1", Topic: TopicEvents, Key: "tenant-1", TenantID: 7, Event: "qc.audited", Payload: []byte(`{}`), Timestamp: time.Now()}
	body, err := marshalMessage(m)
	if err != nil {
		t.Fatal(err)
	}
	back, err := unmarshalMessage(body)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != m.ID || back.Event != m.Event || back.TenantID != m.TenantID || back.Key != m.Key {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
}

// ---- 远端适配器（需要真实服务，默认跳过）----
// 例：EM_TEST_RABBITMQ_URL=amqp://guest:guest@127.0.0.1:5672/ go test ./internal/infra/

func TestRabbitMQRoundtrip(t *testing.T) {
	url := os.Getenv("EM_TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("EM_TEST_RABBITMQ_URL not set")
	}
	b, err := newRabbitBroker(url)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := make(chan Message, 1)
	if err := b.Subscribe(ctx, TopicEvents, "itest", func(_ context.Context, m Message) error {
		got <- m
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // 等队列绑定生效
	if err := b.Publish(ctx, Message{ID: "r1", Topic: TopicEvents, Event: "itest.ping", TenantID: 1, Payload: []byte(`{}`), Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m.Event != "itest.ping" {
			t.Fatalf("unexpected: %+v", m)
		}
	case <-ctx.Done():
		t.Fatal("no message received")
	}
}

func TestKafkaRoundtrip(t *testing.T) {
	url := os.Getenv("EM_TEST_KAFKA_URL")
	if url == "" {
		t.Skip("EM_TEST_KAFKA_URL not set")
	}
	b, err := newKafkaBroker(url)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got := make(chan Message, 1)
	if err := b.Subscribe(ctx, "em.itest", "itest-group", func(_ context.Context, m Message) error {
		got <- m
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second) // 等消费组加入
	if err := b.Publish(ctx, Message{ID: "k1", Topic: "em.itest", Event: "itest.ping", TenantID: 1, Payload: []byte(`{}`), Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m.Event != "itest.ping" {
			t.Fatalf("unexpected: %+v", m)
		}
	case <-ctx.Done():
		t.Fatal("no message received")
	}
}

func TestRedisCacheAndLock(t *testing.T) {
	url := os.Getenv("EM_TEST_REDIS_URL")
	if url == "" {
		t.Skip("EM_TEST_REDIS_URL not set")
	}
	rc, err := newRedisCache(url)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	ctx := context.Background()
	if err := rc.Set(ctx, "em:test:k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := rc.Get(ctx, "em:test:k"); !ok || string(v) != "v" {
		t.Fatalf("get: ok=%v v=%q", ok, v)
	}
	_ = rc.Del(ctx, "em:test:k")
	if _, ok, _ := rc.Get(ctx, "em:test:k"); ok {
		t.Fatal("deleted key should miss")
	}
	l := newRedisLocker(rc.cmd)
	release, ok, err := l.Acquire(ctx, "em:test:lock", time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}
	if _, ok2, _ := l.Acquire(ctx, "em:test:lock", time.Minute); ok2 {
		t.Fatal("second acquire should fail")
	}
	release()
	if _, ok3, _ := l.Acquire(ctx, "em:test:lock", time.Minute); !ok3 {
		t.Fatal("acquire after release should succeed")
	}
}
