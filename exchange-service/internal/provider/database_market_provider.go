package provider

import (
	"fmt"
	"math"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

type DatabaseMarketProvider struct {
	repo *repository.MarketRepository
}

func NewDatabaseMarketProvider(repo *repository.MarketRepository) *DatabaseMarketProvider {
	return &DatabaseMarketProvider{repo: repo}
}

func (p *DatabaseMarketProvider) GetExchanges() ([]models.Exchange, error) {
	return p.repo.ListExchanges()
}

func (p *DatabaseMarketProvider) GetListings() ([]models.Listing, error) {
	return p.repo.ListListings()
}

func (p *DatabaseMarketProvider) GetListing(ticker string) (*models.Listing, error) {
	return p.repo.GetListing(ticker)
}

func (p *DatabaseMarketProvider) GetHistory(ticker string) ([]models.ListingDailyPriceInfo, error) {
	return p.repo.GetHistory(ticker)
}

func (p *DatabaseMarketProvider) GetPortfolio(ownerID uint, ownerType models.PortfolioOwnerType) (*models.Portfolio, error) {
	positions := selectSeededPortfolioPositions(ownerID)
	selected := make([]string, 0, len(positions))
	for _, position := range positions {
		selected = append(selected, position.Ticker)
	}
	listings, err := p.repo.GetListingsByTickers(selected)
	if err != nil {
		return nil, err
	}
	return buildPortfolioReadModel(ownerID, ownerType, positions, listings), nil
}

func (p *DatabaseMarketProvider) String() string {
	return fmt.Sprintf("DatabaseMarketProvider")
}

func marketRound2(value float64) float64 {
	return math.Round(value*100) / 100
}
