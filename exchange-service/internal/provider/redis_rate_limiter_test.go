package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRedisFixedWindowRateLimiter_AllowsWithinLimit(t *testing.T) {
	store := newFakeCounterStore()
	limiter := NewRedisFixedWindowRateLimiter(store, "test:rl", 2, time.Minute)

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("second wait failed: %v", err)
	}
	if store.expireCalls == 0 {
		t.Fatal("expected limiter to set key expiration")
	}
}

func TestRedisFixedWindowRateLimiter_PropagatesRedisError(t *testing.T) {
	limiter := NewRedisFixedWindowRateLimiter(failingCounterStore{}, "test:rl", 2, time.Minute)

	if err := limiter.Wait(context.Background()); err == nil {
		t.Fatal("expected Redis error")
	}
}

func TestAlphaVantageRateLimit_FallsBackWhenRedisLimiterFails(t *testing.T) {
	c := NewAlphaVantageClient("k").WithRateLimiter(failingLimiter{})
	c.minInterval = 10 * time.Millisecond

	start := time.Now()
	c.rateLimit()
	c.rateLimit()

	if elapsed := time.Since(start); elapsed < 8*time.Millisecond {
		t.Fatalf("expected local fallback limiter delay, got %v", elapsed)
	}
}

type fakeCounterStore struct {
	values      map[string]int64
	expireCalls int
}

func newFakeCounterStore() *fakeCounterStore {
	return &fakeCounterStore{values: make(map[string]int64)}
}

func (f *fakeCounterStore) Incr(_ context.Context, key string) (int64, error) {
	f.values[key]++
	return f.values[key], nil
}

func (f *fakeCounterStore) Expire(_ context.Context, _ string, _ time.Duration) error {
	f.expireCalls++
	return nil
}

type failingCounterStore struct{}

func (failingCounterStore) Incr(context.Context, string) (int64, error) {
	return 0, errors.New("redis unavailable")
}

func (failingCounterStore) Expire(context.Context, string, time.Duration) error {
	return errors.New("redis unavailable")
}

type failingLimiter struct{}

func (failingLimiter) Wait(context.Context) error {
	return errors.New("redis unavailable")
}
