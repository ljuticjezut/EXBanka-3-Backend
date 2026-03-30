package provider

import (
	"sort"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
)

type seededPortfolioPosition struct {
	Ticker string
	Qty    float64
	Cost   float64
}

var seededPortfolioBlueprint = []seededPortfolioPosition{
	{Ticker: "AAPL", Qty: 8, Cost: 198.12},
	{Ticker: "MSFT", Qty: 5, Cost: 398.40},
	{Ticker: "NVDA", Qty: 3, Cost: 861.55},
	{Ticker: "JPM", Qty: 12, Cost: 182.15},
	{Ticker: "AMZN", Qty: 9, Cost: 173.45},
	{Ticker: "GOOGL", Qty: 11, Cost: 164.32},
}

func selectSeededPortfolioPositions(ownerID uint) []seededPortfolioPosition {
	start := int(ownerID) % len(seededPortfolioBlueprint)
	positions := make([]seededPortfolioPosition, 0, 4)
	for i := 0; i < 4; i++ {
		positions = append(positions, seededPortfolioBlueprint[(start+i)%len(seededPortfolioBlueprint)])
	}
	return positions
}

func buildPortfolioReadModel(ownerID uint, ownerType models.PortfolioOwnerType, positions []seededPortfolioPosition, listings map[string]models.Listing) *models.Portfolio {
	items := make([]models.PortfolioItem, 0, len(positions))
	var estimatedValue float64
	var pnl float64
	var valuationAsOf time.Time
	currencies := make(map[string]struct{})

	for _, position := range positions {
		listing, ok := listings[position.Ticker]
		if !ok {
			continue
		}

		marketValue := marketRound2(position.Qty * listing.Price)
		itemPnL := marketRound2((listing.Price - position.Cost) * position.Qty)
		itemPnLPercent := 0.0
		if position.Cost > 0 {
			itemPnLPercent = marketRound2(((listing.Price - position.Cost) / position.Cost) * 100)
		}

		if listing.LastRefresh.After(valuationAsOf) {
			valuationAsOf = listing.LastRefresh.UTC().Truncate(time.Minute)
		}
		currencies[listing.Exchange.Currency] = struct{}{}

		items = append(items, models.PortfolioItem{
			Ticker:       listing.Ticker,
			Name:         listing.Name,
			Exchange:     listing.Exchange.Acronym,
			Currency:     listing.Exchange.Currency,
			Quantity:     position.Qty,
			AveragePrice: position.Cost,
			CurrentPrice: listing.Price,
			MarketValue:  marketValue,
			PnL:          itemPnL,
			PnLPercent:   itemPnLPercent,
		})
		estimatedValue += marketValue
		pnl += itemPnL
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Ticker < items[j].Ticker
	})

	valuationCurrency := ""
	switch len(currencies) {
	case 0:
		valuationCurrency = ""
	case 1:
		for currency := range currencies {
			valuationCurrency = currency
		}
	default:
		valuationCurrency = models.PortfolioValuationCurrencyMixed
	}

	return &models.Portfolio{
		OwnerID:           ownerID,
		OwnerType:         ownerType,
		GeneratedAt:       valuationAsOf,
		ValuationAsOf:     valuationAsOf,
		ValuationCurrency: valuationCurrency,
		EstimatedValue:    marketRound2(estimatedValue),
		UnrealizedPnL:     marketRound2(pnl),
		PositionCount:     len(items),
		ReadOnly:          true,
		ModelType:         models.PortfolioModelTypeSprint4SeededReadOnly,
		PositionSource:    models.PortfolioPositionSourceDeterministicSeed,
		PricingSource:     models.PortfolioPricingSourceListingSnapshot,
		Items:             items,
	}
}
