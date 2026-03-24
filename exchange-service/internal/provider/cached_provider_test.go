package provider_test

import (
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
