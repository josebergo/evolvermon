package infra

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ---- Redis 适配器：缓存 + 分布式锁（EM_CACHE=redis 时启用）----

type redisCache struct {
	cmd redis.Cmdable
}

// newRedisCache 按 URL 建立 Redis 连接（redis://user:pass@host:port/db），并 Ping 验证。
func newRedisCache(url string) (*redisCache, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	if err := c.Ping(context.Background()).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &redisCache{cmd: c}, nil
}

func (c *redisCache) Name() string { return "redis" }

func (c *redisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := c.cmd.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

func (c *redisCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 0 // redis: 0 = 永不过期
	}
	return c.cmd.Set(ctx, key, val, ttl).Err()
}

func (c *redisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.cmd.Del(ctx, keys...).Err()
}

func (c *redisCache) Close() error {
	if cl, ok := c.cmd.(interface{ Close() error }); ok {
		return cl.Close()
	}
	return nil
}

type redisLocker struct {
	cmd redis.Cmdable
}

// newRedisLocker 复用缓存连接的分布式锁。
func newRedisLocker(cmd redis.Cmdable) Locker { return &redisLocker{cmd: cmd} }

func (l *redisLocker) Name() string { return "redis" }

// 只删除自己持有的锁（token 比对），避免误释放他人锁。
var redisUnlock = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end`)

func (l *redisLocker) Acquire(ctx context.Context, name string, ttl time.Duration) (func(), bool, error) {
	key := "em:lock:" + name
	token := uuid.NewString()
	ok, err := l.cmd.SetNX(ctx, key, token, ttl).Result()
	if err != nil || !ok {
		return nil, false, err
	}
	release := func() {
		_ = redisUnlock.Run(context.Background(), l.cmd, []string{key}, token).Err()
	}
	return release, true, nil
}
