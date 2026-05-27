package provider

import (
	"context"
	"fmt"
	"time"
)

type rateCounterStore interface {
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

type RateLimiter interface {
	Wait(ctx context.Context) error
}

type RedisFixedWindowRateLimiter struct {
	store     rateCounterStore
	keyPrefix string
	limit     int64
	window    time.Duration
}

func NewRedisFixedWindowRateLimiter(store rateCounterStore, keyPrefix string, limit int64, window time.Duration) *RedisFixedWindowRateLimiter {
	if keyPrefix == "" {
		keyPrefix = "rl:av"
	}
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RedisFixedWindowRateLimiter{
		store:     store,
		keyPrefix: keyPrefix,
		limit:     limit,
		window:    window,
	}
}

func (l *RedisFixedWindowRateLimiter) Wait(ctx context.Context) error {
	if l == nil || l.store == nil {
		return nil
	}

	for {
		now := time.Now().UTC()
		windowStart := now.Truncate(l.window)
		key := fmt.Sprintf("%s:%d", l.keyPrefix, windowStart.Unix())

		count, err := l.store.Incr(ctx, key)
		if err != nil {
			return err
		}
		if count == 1 {
			_ = l.store.Expire(ctx, key, l.window+time.Second)
		}
		if count <= l.limit {
			return nil
		}

		wait := windowStart.Add(l.window).Sub(now)
		if wait <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
