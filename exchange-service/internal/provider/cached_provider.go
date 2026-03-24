package provider

import (
	"sync"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/service"
)

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

// GetRate returns the exchange rate for the given currency pair.
// For identical currencies it returns 1.0 immediately. Otherwise it
// ensures the cache is fresh and looks up the pair.
func (c *CachedProvider) GetRate(from, to string) (float64, error) {
	if from == to {
		return 1.0, nil
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
}
