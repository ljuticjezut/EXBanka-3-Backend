package provider

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
)

type MarketDataProvider interface {
	GetExchanges() ([]models.Exchange, error)
	GetListings() ([]models.Listing, error)
	GetListing(ticker string) (*models.Listing, error)
	GetHistory(ticker string) ([]models.ListingDailyPriceInfo, error)
	GetPortfolio(ownerID uint, ownerType models.PortfolioOwnerType) (*models.Portfolio, error)
}

type MockMarketProvider struct {
	exchanges map[string]models.Exchange
	listings  map[string]models.Listing
	history   map[string][]models.ListingDailyPriceInfo
}

func NewMockMarketProvider() *MockMarketProvider {
	exchanges := []models.Exchange{
		{Name: "New York Stock Exchange", Acronym: "NYSE", MICCode: "XNYS", Polity: "United States", Currency: "USD", Timezone: "America/New_York", WorkingHours: "09:30-16:00", Enabled: true},
		{Name: "NASDAQ", Acronym: "NASDAQ", MICCode: "XNAS", Polity: "United States", Currency: "USD", Timezone: "America/New_York", WorkingHours: "09:30-16:00", Enabled: true},
		{Name: "London Stock Exchange", Acronym: "LSE", MICCode: "XLON", Polity: "United Kingdom", Currency: "GBP", Timezone: "Europe/London", WorkingHours: "08:00-16:30", Enabled: true},
		{Name: "Xetra", Acronym: "XETRA", MICCode: "XETR", Polity: "Germany", Currency: "EUR", Timezone: "Europe/Berlin", WorkingHours: "09:00-17:30", Enabled: true},
		{Name: "Euronext Paris", Acronym: "EPA", MICCode: "XPAR", Polity: "France", Currency: "EUR", Timezone: "Europe/Paris", WorkingHours: "09:00-17:30", Enabled: true},
		{Name: "Tokyo Stock Exchange", Acronym: "TSE", MICCode: "XTKS", Polity: "Japan", Currency: "JPY", Timezone: "Asia/Tokyo", WorkingHours: "09:00-15:00", Enabled: true},
	}

	exchangeMap := make(map[string]models.Exchange, len(exchanges))
	for _, exchange := range exchanges {
		exchangeMap[exchange.Acronym] = exchange
	}

	baseListings := []struct {
		Ticker   string
		Name     string
		Exchange string
		Price    float64
		Volume   int64
	}{
		{Ticker: "AAPL", Name: "Apple Inc.", Exchange: "NASDAQ", Price: 214.33, Volume: 68123412},
		{Ticker: "MSFT", Name: "Microsoft Corp.", Exchange: "NASDAQ", Price: 421.84, Volume: 35219873},
		{Ticker: "NVDA", Name: "NVIDIA Corp.", Exchange: "NASDAQ", Price: 905.18, Volume: 58741239},
		{Ticker: "GOOGL", Name: "Alphabet Inc. Class A", Exchange: "NASDAQ", Price: 172.56, Volume: 24117893},
		{Ticker: "AMZN", Name: "Amazon.com Inc.", Exchange: "NASDAQ", Price: 188.14, Volume: 42631518},
		{Ticker: "JPM", Name: "JPMorgan Chase & Co.", Exchange: "NYSE", Price: 197.41, Volume: 14328761},
		{Ticker: "KO", Name: "Coca-Cola Co.", Exchange: "NYSE", Price: 63.74, Volume: 11873491},
		{Ticker: "SAP", Name: "SAP SE", Exchange: "XETRA", Price: 178.26, Volume: 3124876},
		{Ticker: "BMW", Name: "BMW AG", Exchange: "XETRA", Price: 108.44, Volume: 2987461},
		{Ticker: "AIR", Name: "Airbus SE", Exchange: "EPA", Price: 161.58, Volume: 2251874},
		{Ticker: "VOD", Name: "Vodafone Group Plc", Exchange: "LSE", Price: 71.18, Volume: 18743122},
		{Ticker: "SONY", Name: "Sony Group Corp.", Exchange: "TSE", Price: 13180.0, Volume: 4287193},
	}

	now := time.Now().UTC().Truncate(time.Minute)
	listingMap := make(map[string]models.Listing, len(baseListings))
	historyMap := make(map[string][]models.ListingDailyPriceInfo, len(baseListings))

	for _, item := range baseListings {
		exchange := exchangeMap[item.Exchange]
		price := round2(item.Price)
		listing := models.Listing{
			Ticker: item.Ticker,
			Name:   item.Name,
			Exchange: models.ExchangeSummary{
				Name:     exchange.Name,
				Acronym:  exchange.Acronym,
				MICCode:  exchange.MICCode,
				Currency: exchange.Currency,
			},
			LastRefresh: now,
			Price:       price,
			Ask:         round2(price * 1.002),
			Bid:         round2(price * 0.998),
			Volume:      item.Volume,
			Type:        models.ListingTypeStock,
		}
		listingMap[item.Ticker] = listing
		historyMap[item.Ticker] = buildHistory(item.Ticker, price, item.Volume)
	}

	return &MockMarketProvider{
		exchanges: exchangeMap,
		listings:  listingMap,
		history:   historyMap,
	}
}

func (p *MockMarketProvider) GetExchanges() ([]models.Exchange, error) {
	items := make([]models.Exchange, 0, len(p.exchanges))
	for _, exchange := range p.exchanges {
		items = append(items, exchange)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Acronym < items[j].Acronym
	})
	return items, nil
}

func (p *MockMarketProvider) GetListings() ([]models.Listing, error) {
	items := make([]models.Listing, 0, len(p.listings))
	for _, listing := range p.listings {
		items = append(items, listing)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Ticker < items[j].Ticker
	})
	return items, nil
}

func (p *MockMarketProvider) GetListing(ticker string) (*models.Listing, error) {
	listing, ok := p.listings[ticker]
	if !ok {
		return nil, nil
	}
	copy := listing
	return &copy, nil
}

func (p *MockMarketProvider) GetHistory(ticker string) ([]models.ListingDailyPriceInfo, error) {
	history, ok := p.history[ticker]
	if !ok {
		return nil, nil
	}
	items := make([]models.ListingDailyPriceInfo, len(history))
	copy(items, history)
	return items, nil
}

func (p *MockMarketProvider) GetPortfolio(ownerID uint, ownerType models.PortfolioOwnerType) (*models.Portfolio, error) {
	positions := selectSeededPortfolioPositions(ownerID)
	listings := make(map[string]models.Listing, len(positions))
	for _, position := range positions {
		listing, ok := p.listings[position.Ticker]
		if ok {
			listings[position.Ticker] = listing
		}
	}
	return buildPortfolioReadModel(ownerID, ownerType, positions, listings), nil
}

func buildHistory(ticker string, currentPrice float64, volume int64) []models.ListingDailyPriceInfo {
	seed := float64(hashTicker(ticker)%17 + 3)
	today := time.Now().UTC()
	history := make([]models.ListingDailyPriceInfo, 0, 30)

	var previous float64
	for dayOffset := 29; dayOffset >= 0; dayOffset-- {
		date := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -dayOffset)
		drift := math.Sin(float64(dayOffset+1)/4.0+seed/10.0) * 0.024
		step := (float64(29-dayOffset) * 0.0012) - 0.018
		price := round2(currentPrice * (1 + drift + step))
		high := round2(price * (1 + 0.008 + seed/1000))
		low := round2(price * (1 - 0.008 - seed/1200))
		dayVolume := int64(math.Round(float64(volume) * (0.78 + math.Mod(seed, 5)/10 + float64(dayOffset%4)/40)))
		change := 0.0
		if previous > 0 {
			change = round2(price - previous)
		}
		previous = price

		history = append(history, models.ListingDailyPriceInfo{
			Date:   date,
			Price:  price,
			High:   high,
			Low:    low,
			Change: change,
			Volume: dayVolume,
		})
	}

	return history
}

func hashTicker(ticker string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(ticker))
	return h.Sum32()
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func (p *MockMarketProvider) String() string {
	return fmt.Sprintf("MockMarketProvider(%d listings)", len(p.listings))
}
