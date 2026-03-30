package provider

import (
	"testing"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
)

func TestBuildPortfolioReadModel_UsesListingSnapshotMetadata(t *testing.T) {
	older := time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 3, 26, 9, 30, 0, 0, time.UTC)

	listings := map[string]models.Listing{
		"MSFT": {
			Ticker:      "MSFT",
			Name:        "Microsoft Corp.",
			LastRefresh: older,
			Price:       410,
			Exchange: models.ExchangeSummary{
				Acronym:  "NASDAQ",
				Currency: "USD",
			},
		},
		"AAPL": {
			Ticker:      "AAPL",
			Name:        "Apple Inc.",
			LastRefresh: newer,
			Price:       214.33,
			Exchange: models.ExchangeSummary{
				Acronym:  "NASDAQ",
				Currency: "USD",
			},
		},
	}

	portfolio := buildPortfolioReadModel(42, models.PortfolioOwnerTypeClient, []seededPortfolioPosition{
		{Ticker: "MSFT", Qty: 5, Cost: 398.40},
		{Ticker: "AAPL", Qty: 8, Cost: 198.12},
	}, listings)

	if !portfolio.ReadOnly {
		t.Fatal("expected read-only portfolio model")
	}
	if portfolio.ModelType != models.PortfolioModelTypeSprint4SeededReadOnly {
		t.Fatalf("expected seeded read model type, got %q", portfolio.ModelType)
	}
	if portfolio.PositionSource != models.PortfolioPositionSourceDeterministicSeed {
		t.Fatalf("expected deterministic seed position source, got %q", portfolio.PositionSource)
	}
	if portfolio.PricingSource != models.PortfolioPricingSourceListingSnapshot {
		t.Fatalf("expected listing snapshot pricing source, got %q", portfolio.PricingSource)
	}
	if !portfolio.GeneratedAt.Equal(newer) || !portfolio.ValuationAsOf.Equal(newer) {
		t.Fatalf("expected valuation timestamp %s, got generatedAt=%s valuationAsOf=%s", newer, portfolio.GeneratedAt, portfolio.ValuationAsOf)
	}
	if portfolio.ValuationCurrency != "USD" {
		t.Fatalf("expected USD valuation currency, got %q", portfolio.ValuationCurrency)
	}
	if portfolio.PositionCount != 2 {
		t.Fatalf("expected 2 positions, got %d", portfolio.PositionCount)
	}
	if len(portfolio.Items) != 2 {
		t.Fatalf("expected 2 portfolio items, got %d", len(portfolio.Items))
	}
	if portfolio.Items[0].Ticker != "AAPL" || portfolio.Items[1].Ticker != "MSFT" {
		t.Fatalf("expected items sorted by ticker, got %+v", portfolio.Items)
	}
}

func TestBuildPortfolioReadModel_MarksMixedCurrencyPortfolio(t *testing.T) {
	snapshot := time.Date(2026, 3, 26, 9, 30, 0, 0, time.UTC)
	listings := map[string]models.Listing{
		"AAPL": {
			Ticker:      "AAPL",
			Name:        "Apple Inc.",
			LastRefresh: snapshot,
			Price:       214.33,
			Exchange: models.ExchangeSummary{
				Acronym:  "NASDAQ",
				Currency: "USD",
			},
		},
		"SAP": {
			Ticker:      "SAP",
			Name:        "SAP SE",
			LastRefresh: snapshot,
			Price:       178.26,
			Exchange: models.ExchangeSummary{
				Acronym:  "XETRA",
				Currency: "EUR",
			},
		},
	}

	portfolio := buildPortfolioReadModel(7, models.PortfolioOwnerTypeEmployee, []seededPortfolioPosition{
		{Ticker: "AAPL", Qty: 1, Cost: 200},
		{Ticker: "SAP", Qty: 2, Cost: 170},
	}, listings)

	if portfolio.OwnerType != models.PortfolioOwnerTypeEmployee {
		t.Fatalf("expected employee owner type, got %q", portfolio.OwnerType)
	}
	if portfolio.ValuationCurrency != models.PortfolioValuationCurrencyMixed {
		t.Fatalf("expected mixed valuation currency marker, got %q", portfolio.ValuationCurrency)
	}
}
