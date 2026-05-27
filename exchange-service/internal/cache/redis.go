package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultRedisTimeout = 500 * time.Millisecond

// RedisClient is a thin wrapper around go-redis with short timeouts so Redis
// outages degrade cache users instead of blocking request paths for seconds.
type RedisClient struct {
	client *redis.Client
}

func NewRedisClient(addr, password string, db int) *RedisClient {
	if addr == "" {
		return nil
	}
	return &RedisClient{
		client: redis.NewClient(&redis.Options{
			Addr:         addr,
			Password:     password,
			DB:           db,
			DialTimeout:  defaultRedisTimeout,
			ReadTimeout:  defaultRedisTimeout,
			WriteTimeout: defaultRedisTimeout,
		}),
	}
}

func (c *RedisClient) Ping(ctx context.Context) error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Ping(ctx).Err()
}

func (c *RedisClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *RedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	return c.client.HGet(ctx, key, field).Result()
}

func (c *RedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.client.HGetAll(ctx, key).Result()
}

func (c *RedisClient) HSet(ctx context.Context, key, field, value string) error {
	return c.client.HSet(ctx, key, field, value).Err()
}

func (c *RedisClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.client.Expire(ctx, key, ttl).Err()
}

func (c *RedisClient) Incr(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}
