package provider

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/service"
)

const defaultFXRedisKey = "fx:rates"

type rateCacheStore interface {
	HGet(ctx context.Context, key, field string) (string, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HSet(ctx context.Context, key, field, value string) error
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// CachedProvider wraps a primary and fallback RateProviderInterface.
// It refreshes the rate cache lazily: on each call it checks whether the TTL
// has elapsed, and only fetches from the underlying provider when stale.
type CachedProvider struct {
	primary  service.RateProviderInterface
	fallback service.RateProviderInterface

	mu          sync.RWMutex
	cache       map[string]map[string]float64
	allRates    []service.ExchangeRate
	lastUpdated time.Time
	ttl         time.Duration

	redisCache rateCacheStore
	redisKey   string
}

// NewCachedProvider creates a CachedProvider with the given primary provider,
// fallback provider, and TTL. When TTL is 0 the cache is always considered stale.
func NewCachedProvider(primary, fallback service.RateProviderInterface, ttl time.Duration) *CachedProvider {
	return &CachedProvider{
		primary:  primary,
		fallback: fallback,
		ttl:      ttl,
	}
}

// WithRedis stores and reads rates through a shared Redis hash. The provider
// keeps the existing in-memory cache as a fallback when Redis is unavailable.
func (c *CachedProvider) WithRedis(cache rateCacheStore) *CachedProvider {
	if cache == nil {
		return c
	}
	c.redisCache = cache
	c.redisKey = defaultFXRedisKey
	return c
}

// GetRate returns the exchange rate for the given currency pair.
// For identical currencies it returns 1.0 immediately. Otherwise it
// ensures the cache is fresh and looks up the pair.
func (c *CachedProvider) GetRate(from, to string) (float64, error) {
	if from == to {
		return 1.0, nil
	}
	if rate, ok := c.getRedisRate(from, to); ok {
		return rate, nil
	}
	c.ensureFresh()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if row, ok := c.cache[from]; ok {
		if rate, ok := row[to]; ok {
			return rate, nil
		}
	}
	// Try fallback directly for the specific pair
	return c.fallback.GetRate(from, to)
}

// GetAllRates returns all available exchange rates, refreshing the cache if stale.
func (c *CachedProvider) GetAllRates() []service.ExchangeRate {
	if rates, ok := c.getRedisRates(); ok {
		return rates
	}
	c.ensureFresh()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.allRates
}

// ensureFresh refreshes the cache if it is stale (last update + TTL < now).
func (c *CachedProvider) ensureFresh() {
	c.mu.RLock()
	fresh := !c.lastUpdated.IsZero() && time.Since(c.lastUpdated) < c.ttl
	c.mu.RUnlock()
	if fresh {
		return
	}
	c.refresh()
}

// refresh fetches all rates from the primary provider. On error it falls back
// to the fallback provider. The resulting rates are stored in the cache.
func (c *CachedProvider) refresh() {
	var rates []service.ExchangeRate

	primary := c.primary.GetAllRates()
	if len(primary) > 0 {
		rates = primary
	} else {
		rates = c.fallback.GetAllRates()
	}

	newCache := make(map[string]map[string]float64)
	for _, r := range rates {
		if newCache[r.From] == nil {
			newCache[r.From] = make(map[string]float64)
		}
		newCache[r.From][r.To] = r.Rate
	}

	c.mu.Lock()
	c.cache = newCache
	c.allRates = rates
	c.lastUpdated = time.Now()
	c.mu.Unlock()

	c.storeRedisRates(rates)
}

func (c *CachedProvider) getRedisRate(from, to string) (float64, bool) {
	if c.redisCache == nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	raw, err := c.redisCache.HGet(ctx, c.redisKey, redisRateField(from, to))
	if err != nil || raw == "" {
		return 0, false
	}
	rate, err := strconv.ParseFloat(raw, 64)
	if err != nil || rate <= 0 {
		return 0, false
	}
	return rate, true
}

func (c *CachedProvider) getRedisRates() ([]service.ExchangeRate, bool) {
	if c.redisCache == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	rawRates, err := c.redisCache.HGetAll(ctx, c.redisKey)
	if err != nil || len(rawRates) == 0 {
		return nil, false
	}

	fields := make([]string, 0, len(rawRates))
	for field := range rawRates {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	rates := make([]service.ExchangeRate, 0, len(fields))
	for _, field := range fields {
		from, to, ok := splitRedisRateField(field)
		if !ok {
			continue
		}
		rate, err := strconv.ParseFloat(rawRates[field], 64)
		if err != nil || rate <= 0 {
			continue
		}
		rates = append(rates, service.ExchangeRate{
			From:     from,
			To:       to,
			Rate:     rate,
			BuyRate:  rate * (1 - spread),
			SellRate: rate * (1 + spread),
		})
	}

	return rates, len(rates) > 0
}

func (c *CachedProvider) storeRedisRates(rates []service.ExchangeRate) {
	if c.redisCache == nil || len(rates) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	for _, rate := range rates {
		if rate.Rate <= 0 {
			continue
		}
		if err := c.redisCache.HSet(ctx, c.redisKey, redisRateField(rate.From, rate.To), strconv.FormatFloat(rate.Rate, 'f', -1, 64)); err != nil {
			return
		}
	}
	if c.ttl > 0 {
		_ = c.redisCache.Expire(ctx, c.redisKey, c.ttl)
	}
}

func redisRateField(from, to string) string {
	return fmt.Sprintf("%s:%s", from, to)
}

func splitRedisRateField(field string) (string, string, bool) {
	for i := 0; i < len(field); i++ {
		if field[i] == ':' {
			if i == 0 || i == len(field)-1 {
				return "", "", false
			}
			return field[:i], field[i+1:], true
		}
	}
	return "", "", false
}
