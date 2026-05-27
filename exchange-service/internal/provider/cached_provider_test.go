package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/provider"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/service"
)

// --- mock providers ---

type alwaysOKProvider struct {
	rate float64
}

func (m *alwaysOKProvider) GetRate(from, to string) (float64, error) {
	if from == to {
		return 1.0, nil
	}
	return m.rate, nil
}
func (m *alwaysOKProvider) GetAllRates() []service.ExchangeRate {
	return []service.ExchangeRate{{From: "EUR", To: "RSD", Rate: m.rate}}
}

type alwaysErrProvider struct{}

func (m *alwaysErrProvider) GetRate(_, _ string) (float64, error) {
	return 0, errors.New("primary unavailable")
}
func (m *alwaysErrProvider) GetAllRates() []service.ExchangeRate { return nil }

// --- tests ---

func TestCachedProvider_GetRate_ReturnsPrimaryRate(t *testing.T) {
	primary := &alwaysOKProvider{rate: 117.5}
	fallback := provider.NewStaticRateProvider()
	cp := provider.NewCachedProvider(primary, fallback, 24*time.Hour)

	rate, err := cp.GetRate("EUR", "RSD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 117.5 {
		t.Errorf("expected 117.5, got %f", rate)
	}
}

func TestCachedProvider_GetRate_FallsBackWhenPrimaryFails(t *testing.T) {
	cp := provider.NewCachedProvider(&alwaysErrProvider{}, provider.NewStaticRateProvider(), 24*time.Hour)

	rate, err := cp.GetRate("EUR", "RSD")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if rate <= 0 {
		t.Errorf("expected positive rate from fallback, got %f", rate)
	}
}

func TestCachedProvider_GetAllRates_ReturnsNonEmpty(t *testing.T) {
	cp := provider.NewCachedProvider(provider.NewStaticRateProvider(), provider.NewStaticRateProvider(), 24*time.Hour)

	rates := cp.GetAllRates()
	if len(rates) == 0 {
		t.Error("expected non-empty rate list from cache")
	}
}

func TestCachedProvider_SameCurrency_ReturnsOne(t *testing.T) {
	cp := provider.NewCachedProvider(provider.NewStaticRateProvider(), provider.NewStaticRateProvider(), 24*time.Hour)

	rate, err := cp.GetRate("EUR", "EUR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 1.0 {
		t.Errorf("expected 1.0 for same currency, got %f", rate)
	}
}

func TestCachedProvider_RefreshesWhenExpired(t *testing.T) {
	callCount := 0
	primary := &countingProvider{rate: 200.0, count: &callCount}
	cp := provider.NewCachedProvider(primary, provider.NewStaticRateProvider(), 0) // ttl=0 → always stale

	cp.GetRate("EUR", "RSD") // first call — refresh
	cp.GetRate("EUR", "RSD") // second call — refresh again (expired immediately)

	if callCount < 2 {
		t.Errorf("expected provider refreshed at least twice with ttl=0, got %d calls", callCount)
	}
}

func TestCachedProvider_DoesNotRefreshBeforeExpiry(t *testing.T) {
	callCount := 0
	primary := &countingProvider{rate: 117.5, count: &callCount}
	cp := provider.NewCachedProvider(primary, provider.NewStaticRateProvider(), 24*time.Hour)

	cp.GetRate("EUR", "RSD") // triggers first refresh
	cp.GetRate("EUR", "RSD") // should use cache — no refresh
	cp.GetRate("EUR", "USD") // should use cache — no refresh

	if callCount > 1 {
		t.Errorf("expected provider refreshed only once within TTL, got %d calls", callCount)
	}
}

func TestCachedProvider_GetRate_UsesRedisHit(t *testing.T) {
	callCount := 0
	primary := &countingProvider{rate: 117.5, count: &callCount}
	cache := newFakeRateCache()
	_ = cache.HSet(context.Background(), "fx:rates", "EUR:RSD", "118.25")

	cp := provider.NewCachedProvider(primary, provider.NewStaticRateProvider(), 24*time.Hour).WithRedis(cache)

	rate, err := cp.GetRate("EUR", "RSD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 118.25 {
		t.Fatalf("expected Redis rate 118.25, got %f", rate)
	}
	if callCount != 0 {
		t.Fatalf("expected primary provider not to be called on Redis hit, got %d", callCount)
	}
}

func TestCachedProvider_RedisMissFallsBackAndPopulatesRedis(t *testing.T) {
	cache := newFakeRateCache()
	cp := provider.NewCachedProvider(&alwaysOKProvider{rate: 119.5}, provider.NewStaticRateProvider(), time.Hour).WithRedis(cache)

	rate, err := cp.GetRate("EUR", "RSD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 119.5 {
		t.Fatalf("expected provider rate 119.5, got %f", rate)
	}

	raw, err := cache.HGet(context.Background(), "fx:rates", "EUR:RSD")
	if err != nil {
		t.Fatalf("expected Redis cache to be populated: %v", err)
	}
	if raw != "119.5" {
		t.Fatalf("expected cached raw rate 119.5, got %q", raw)
	}
}

func TestCachedProvider_RedisDownFallsBackToMemory(t *testing.T) {
	cp := provider.NewCachedProvider(&alwaysOKProvider{rate: 120.5}, provider.NewStaticRateProvider(), time.Hour).WithRedis(failingRateCache{})

	rate, err := cp.GetRate("EUR", "RSD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 120.5 {
		t.Fatalf("expected provider fallback rate 120.5, got %f", rate)
	}
}

type countingProvider struct {
	rate  float64
	count *int
}

func (m *countingProvider) GetRate(from, to string) (float64, error) {
	(*m.count)++
	if from == to {
		return 1.0, nil
	}
	return m.rate, nil
}
func (m *countingProvider) GetAllRates() []service.ExchangeRate {
	(*m.count)++
	return []service.ExchangeRate{{From: "EUR", To: "RSD", Rate: m.rate}}
}

type fakeRateCache struct {
	values map[string]map[string]string
}

func newFakeRateCache() *fakeRateCache {
	return &fakeRateCache{values: make(map[string]map[string]string)}
}

func (f *fakeRateCache) HGet(_ context.Context, key, field string) (string, error) {
	if fields, ok := f.values[key]; ok {
		if value, ok := fields[field]; ok {
			return value, nil
		}
	}
	return "", errors.New("cache miss")
}

func (f *fakeRateCache) HGetAll(_ context.Context, key string) (map[string]string, error) {
	result := make(map[string]string)
	for field, value := range f.values[key] {
		result[field] = value
	}
	return result, nil
}

func (f *fakeRateCache) HSet(_ context.Context, key, field, value string) error {
	if f.values[key] == nil {
		f.values[key] = make(map[string]string)
	}
	f.values[key][field] = value
	return nil
}

func (f *fakeRateCache) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

type failingRateCache struct{}

func (failingRateCache) HGet(context.Context, string, string) (string, error) {
	return "", errors.New("redis unavailable")
}

func (failingRateCache) HGetAll(context.Context, string) (map[string]string, error) {
	return nil, errors.New("redis unavailable")
}

func (failingRateCache) HSet(context.Context, string, string, string) error {
	return errors.New("redis unavailable")
}

func (failingRateCache) Expire(context.Context, string, time.Duration) error {
	return errors.New("redis unavailable")
}
