package infra

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---- 内存消息总线（默认）：订阅者慢（缓冲满）即丢弃，仅供单机/开发 ----

type memSub struct {
	ch chan Message
	h  Handler
}

type memBroker struct {
	mu     sync.Mutex
	subs   []*memSub
	wg     sync.WaitGroup
	stop   chan struct{}
	closed bool
}

// NewMemoryBroker 进程内发布/订阅。
func NewMemoryBroker() Broker { return &memBroker{stop: make(chan struct{})} }

func (b *memBroker) Name() string { return "memory" }

func (b *memBroker) Publish(_ context.Context, m Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		select {
		case s.ch <- m:
		default: // 尽力投递：缓冲满丢弃，避免拖慢发布方
			log.Printf("[infra:memory] topic %s subscriber overflow, message %s dropped", m.Topic, m.ID)
		}
	}
	return nil
}

func (b *memBroker) Subscribe(ctx context.Context, _, _ string, h Handler) error {
	s := &memSub{ch: make(chan Message, 256), h: h}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case m := <-s.ch:
				if err := h(ctx, m); err != nil {
					log.Printf("[infra:memory] handler: %v", err)
				}
			case <-ctx.Done():
				return
			case <-b.stop:
				return
			}
		}
	}()
	return nil
}

func (b *memBroker) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	close(b.stop)
	b.mu.Unlock()
	b.wg.Wait()
	return nil
}

// ---- 内存缓存（默认）：读时惰性过期 + 写时偶发清扫 ----

type memEntry struct {
	val []byte
	exp time.Time // 零值 = 永不过期
}

type memCache struct {
	mu     sync.RWMutex
	m      map[string]memEntry
	writes int
}

// NewMemoryCache 进程内缓存。
func NewMemoryCache() Cache { return &memCache{m: map[string]memEntry{}} }

func (c *memCache) Name() string { return "memory" }

func (c *memCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok || (e.exp != (time.Time{}) && time.Now().After(e.exp)) {
		return nil, false, nil
	}
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, true, nil
}

func (c *memCache) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	cp := make([]byte, len(val))
	copy(cp, val)
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	c.mu.Lock()
	c.m[key] = memEntry{val: cp, exp: exp}
	c.writes++
	if c.writes%1024 == 0 { // 偶发清扫，防止冷键长期滞留
		now := time.Now()
		for k, e := range c.m {
			if !e.exp.Equal(time.Time{}) && now.After(e.exp) {
				delete(c.m, k)
			}
		}
	}
	c.mu.Unlock()
	return nil
}

func (c *memCache) Del(_ context.Context, keys ...string) error {
	c.mu.Lock()
	for _, k := range keys {
		delete(c.m, k)
	}
	c.mu.Unlock()
	return nil
}

// ---- 内存锁（默认）：随缓存提供方选择，单进程内互斥 ----

type memLock struct {
	deadline time.Time
	token    string
}

type memLocker struct {
	mu   sync.Mutex
	held map[string]memLock
}

// NewMemoryLocker 进程内互斥锁。
func NewMemoryLocker() Locker { return &memLocker{held: map[string]memLock{}} }

func (l *memLocker) Name() string { return "memory" }

func (l *memLocker) Acquire(_ context.Context, name string, ttl time.Duration) (func(), bool, error) {
	token := uuid.NewString()
	l.mu.Lock()
	now := time.Now()
	if cur, ok := l.held[name]; ok && now.Before(cur.deadline) {
		l.mu.Unlock()
		return nil, false, nil // 未过期：被持有
	}
	l.held[name] = memLock{deadline: now.Add(ttl), token: token}
	l.mu.Unlock()
	release := func() {
		l.mu.Lock()
		if cur, ok := l.held[name]; ok && cur.token == token { // 只释放自己持有的
			delete(l.held, name)
		}
		l.mu.Unlock()
	}
	return release, true, nil
}
