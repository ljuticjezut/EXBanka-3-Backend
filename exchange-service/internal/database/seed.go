package database

import (
	"hash/fnv"
	"log/slog"
	"math"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"gorm.io/gorm"
)

type listingSeed struct {
	Ticker          string
	Name            string
	ExchangeAcronym string
	Price           float64
	Volume          int64
	Type            models.ListingType
}

func SeedMarketData(db *gorm.DB) error {
	exchangeIDs := make(map[string]uint, len(seedExchanges()))

	for _, exchange := range seedExchanges() {
		record := models.MarketExchangeRecord{
			Name:         exchange.Name,
			Acronym:      exchange.Acronym,
			MICCode:      exchange.MICCode,
			Polity:       exchange.Polity,
			Currency:     exchange.Currency,
			Timezone:     exchange.Timezone,
			WorkingHours: exchange.WorkingHours,
			Enabled:      exchange.Enabled,
		}

		if err := db.Where("acronym = ?", exchange.Acronym).
			Assign(record).
			FirstOrCreate(&record).Error; err != nil {
			return err
		}
		exchangeIDs[exchange.Acronym] = record.ID
	}

	referenceTime := seedReferenceTime()
	for _, listing := range seedListings() {
		exchangeID, ok := exchangeIDs[listing.ExchangeAcronym]
		if !ok {
			continue
		}

		record := models.MarketListingRecord{
			Ticker:      listing.Ticker,
			Name:        listing.Name,
			ExchangeID:  exchangeID,
			LastRefresh: referenceTime,
			Price:       round2(listing.Price),
			Ask:         round2(listing.Price * 1.002),
			Bid:         round2(listing.Price * 0.998),
			Volume:      listing.Volume,
			Type:        string(listing.Type),
		}

		if err := db.Where("ticker = ?", listing.Ticker).
			Assign(record).
			FirstOrCreate(&record).Error; err != nil {
			return err
		}

		history := buildSeedHistory(listing.Ticker, listing.Price, listing.Volume)
		for _, item := range history {
			historyRecord := models.MarketListingDailyPriceInfoRecord{
				ListingID: record.ID,
				Date:      item.Date,
				Price:     item.Price,
				High:      item.High,
				Low:       item.Low,
				Change:    item.Change,
				Volume:    item.Volume,
			}

			if err := db.Where("listing_id = ? AND date = ?", record.ID, item.Date).
				Assign(historyRecord).
				FirstOrCreate(&historyRecord).Error; err != nil {
				return err
			}
		}
	}

	slog.Info("Market seed complete",
		"exchanges", len(seedExchanges()),
		"listings", len(seedListings()),
		"history_days", 30,
	)
	return nil
}

func seedReferenceTime() time.Time {
	return time.Date(2026, 3, 30, 19, 0, 0, 0, time.UTC)
}

func seedExchanges() []models.Exchange {
	return []models.Exchange{
		{Name: "New York Stock Exchange", Acronym: "NYSE", MICCode: "XNYS", Polity: "United States", Currency: "USD", Timezone: "America/New_York", WorkingHours: "09:30-16:00", Enabled: true},
		{Name: "NASDAQ", Acronym: "NASDAQ", MICCode: "XNAS", Polity: "United States", Currency: "USD", Timezone: "America/New_York", WorkingHours: "09:30-16:00", Enabled: true},
		{Name: "London Stock Exchange", Acronym: "LSE", MICCode: "XLON", Polity: "United Kingdom", Currency: "GBP", Timezone: "Europe/London", WorkingHours: "08:00-16:30", Enabled: true},
		{Name: "Xetra", Acronym: "XETRA", MICCode: "XETR", Polity: "Germany", Currency: "EUR", Timezone: "Europe/Berlin", WorkingHours: "09:00-17:30", Enabled: true},
		{Name: "Euronext Paris", Acronym: "EPA", MICCode: "XPAR", Polity: "France", Currency: "EUR", Timezone: "Europe/Paris", WorkingHours: "09:00-17:30", Enabled: true},
		{Name: "Tokyo Stock Exchange", Acronym: "TSE", MICCode: "XTKS", Polity: "Japan", Currency: "JPY", Timezone: "Asia/Tokyo", WorkingHours: "09:00-15:00", Enabled: true},
	}
}

func seedListings() []listingSeed {
	return []listingSeed{
		{Ticker: "AAPL", Name: "Apple Inc.", ExchangeAcronym: "NASDAQ", Price: 214.33, Volume: 68123412, Type: models.ListingTypeStock},
		{Ticker: "MSFT", Name: "Microsoft Corp.", ExchangeAcronym: "NASDAQ", Price: 421.84, Volume: 35219873, Type: models.ListingTypeStock},
		{Ticker: "NVDA", Name: "NVIDIA Corp.", ExchangeAcronym: "NASDAQ", Price: 905.18, Volume: 58741239, Type: models.ListingTypeStock},
		{Ticker: "GOOGL", Name: "Alphabet Inc. Class A", ExchangeAcronym: "NASDAQ", Price: 172.56, Volume: 24117893, Type: models.ListingTypeStock},
		{Ticker: "AMZN", Name: "Amazon.com Inc.", ExchangeAcronym: "NASDAQ", Price: 188.14, Volume: 42631518, Type: models.ListingTypeStock},
		{Ticker: "JPM", Name: "JPMorgan Chase & Co.", ExchangeAcronym: "NYSE", Price: 197.41, Volume: 14328761, Type: models.ListingTypeStock},
		{Ticker: "KO", Name: "Coca-Cola Co.", ExchangeAcronym: "NYSE", Price: 63.74, Volume: 11873491, Type: models.ListingTypeStock},
		{Ticker: "SAP", Name: "SAP SE", ExchangeAcronym: "XETRA", Price: 178.26, Volume: 3124876, Type: models.ListingTypeStock},
		{Ticker: "BMW", Name: "BMW AG", ExchangeAcronym: "XETRA", Price: 108.44, Volume: 2987461, Type: models.ListingTypeStock},
		{Ticker: "AIR", Name: "Airbus SE", ExchangeAcronym: "EPA", Price: 161.58, Volume: 2251874, Type: models.ListingTypeStock},
		{Ticker: "VOD", Name: "Vodafone Group Plc", ExchangeAcronym: "LSE", Price: 71.18, Volume: 18743122, Type: models.ListingTypeStock},
		{Ticker: "SONY", Name: "Sony Group Corp.", ExchangeAcronym: "TSE", Price: 13180.0, Volume: 4287193, Type: models.ListingTypeStock},
	}
}

func buildSeedHistory(ticker string, currentPrice float64, volume int64) []models.ListingDailyPriceInfo {
	seed := float64(hashTicker(ticker)%17 + 3)
	referenceDate := seedReferenceTime()
	history := make([]models.ListingDailyPriceInfo, 0, 30)

	var previous float64
	for dayOffset := 29; dayOffset >= 0; dayOffset-- {
		date := time.Date(referenceDate.Year(), referenceDate.Month(), referenceDate.Day(), 0, 0, 0, 0, time.UTC).
			AddDate(0, 0, -dayOffset)
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
