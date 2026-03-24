package provider

import (
	"errors"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/service"
)

// StaticRateProvider serves hardcoded exchange rates as a development/fallback provider.
type StaticRateProvider struct {
	rates map[string]map[string]float64
}

func NewStaticRateProvider() *StaticRateProvider {
	return &StaticRateProvider{
		rates: map[string]map[string]float64{
			"EUR": {"RSD": 117.15, "USD": 1.08, "CHF": 0.96, "GBP": 0.85, "JPY": 161.50, "CAD": 1.47, "AUD": 1.63},
			"USD": {"RSD": 108.48, "EUR": 0.926, "CHF": 0.888, "GBP": 0.786, "JPY": 149.53, "CAD": 1.362, "AUD": 1.508},
			"CHF": {"RSD": 122.03, "EUR": 1.042, "USD": 1.126, "GBP": 0.887, "JPY": 168.17, "CAD": 1.530, "AUD": 1.694},
			"GBP": {"RSD": 137.82, "EUR": 1.176, "USD": 1.272, "CHF": 1.128, "JPY": 189.59, "CAD": 1.724, "AUD": 1.909},
			"JPY": {"RSD": 0.728, "EUR": 0.00619, "USD": 0.00669, "CHF": 0.00595, "GBP": 0.00527, "CAD": 0.00909, "AUD": 0.01007},
			"CAD": {"RSD": 79.71, "EUR": 0.680, "USD": 0.734, "CHF": 0.654, "GBP": 0.580, "JPY": 110.01, "AUD": 1.107},
			"AUD": {"RSD": 71.87, "EUR": 0.613, "USD": 0.663, "CHF": 0.590, "GBP": 0.524, "JPY": 99.31, "CAD": 0.903},
			"RSD": {"EUR": 0.00854, "USD": 0.00922, "CHF": 0.00819, "GBP": 0.00726, "JPY": 1.373, "CAD": 0.01254, "AUD": 0.01391},
		},
	}
}

func (p *StaticRateProvider) GetRate(from, to string) (float64, error) {
	if from == to {
		return 1.0, nil
	}
	if row, ok := p.rates[from]; ok {
		if rate, ok := row[to]; ok {
			return rate, nil
		}
	}
	return 0, errors.New("unknown currency pair")
}

func (p *StaticRateProvider) GetAllRates() []service.ExchangeRate {
	var result []service.ExchangeRate
	for from, targets := range p.rates {
		for to, rate := range targets {
			result = append(result, service.ExchangeRate{From: from, To: to, Rate: rate})
		}
	}
	return result
}
