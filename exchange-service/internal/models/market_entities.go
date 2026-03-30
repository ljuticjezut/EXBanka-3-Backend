package models

import "time"

type MarketExchangeRecord struct {
	ID           uint                  `gorm:"primaryKey"`
	Name         string                `gorm:"not null"`
	Acronym      string                `gorm:"not null;uniqueIndex"`
	MICCode      string                `gorm:"column:mic_code;not null;uniqueIndex"`
	Polity       string                `gorm:"not null"`
	Currency     string                `gorm:"not null"`
	Timezone     string                `gorm:"not null"`
	WorkingHours string                `gorm:"column:working_hours;not null"`
	Enabled      bool                  `gorm:"not null;default:true"`
	Listings     []MarketListingRecord `gorm:"foreignKey:ExchangeID"`
}

func (MarketExchangeRecord) TableName() string {
	return "market_exchanges"
}

func (r MarketExchangeRecord) ToDomain() Exchange {
	return Exchange{
		Name:         r.Name,
		Acronym:      r.Acronym,
		MICCode:      r.MICCode,
		Polity:       r.Polity,
		Currency:     r.Currency,
		Timezone:     r.Timezone,
		WorkingHours: r.WorkingHours,
		Enabled:      r.Enabled,
	}
}

func (r MarketExchangeRecord) ToSummary() ExchangeSummary {
	return ExchangeSummary{
		Name:     r.Name,
		Acronym:  r.Acronym,
		MICCode:  r.MICCode,
		Currency: r.Currency,
	}
}

type MarketListingRecord struct {
	ID          uint                                `gorm:"primaryKey"`
	Ticker      string                              `gorm:"not null;uniqueIndex"`
	Name        string                              `gorm:"not null"`
	ExchangeID  uint                                `gorm:"column:exchange_id;not null;index"`
	Exchange    MarketExchangeRecord                `gorm:"foreignKey:ExchangeID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	LastRefresh time.Time                           `gorm:"column:last_refresh;not null"`
	Price       float64                             `gorm:"not null"`
	Ask         float64                             `gorm:"not null"`
	Bid         float64                             `gorm:"not null"`
	Volume      int64                               `gorm:"not null"`
	Type        string                              `gorm:"not null"`
	History     []MarketListingDailyPriceInfoRecord `gorm:"foreignKey:ListingID"`
}

func (MarketListingRecord) TableName() string {
	return "market_listings"
}

func (r MarketListingRecord) ToDomain() Listing {
	return Listing{
		Ticker:      r.Ticker,
		Name:        r.Name,
		Exchange:    r.Exchange.ToSummary(),
		LastRefresh: r.LastRefresh,
		Price:       r.Price,
		Ask:         r.Ask,
		Bid:         r.Bid,
		Volume:      r.Volume,
		Type:        ListingType(r.Type),
	}
}

type MarketListingDailyPriceInfoRecord struct {
	ID        uint                `gorm:"primaryKey"`
	ListingID uint                `gorm:"column:listing_id;not null;index;uniqueIndex:idx_market_listing_date"`
	Listing   MarketListingRecord `gorm:"foreignKey:ListingID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Date      time.Time           `gorm:"type:date;not null;uniqueIndex:idx_market_listing_date"`
	Price     float64             `gorm:"not null"`
	High      float64             `gorm:"not null"`
	Low       float64             `gorm:"not null"`
	Change    float64             `gorm:"not null"`
	Volume    int64               `gorm:"not null"`
}

func (MarketListingDailyPriceInfoRecord) TableName() string {
	return "market_listing_daily_price_infos"
}

func (r MarketListingDailyPriceInfoRecord) ToDomain() ListingDailyPriceInfo {
	return ListingDailyPriceInfo{
		Date:   r.Date,
		Price:  r.Price,
		High:   r.High,
		Low:    r.Low,
		Change: r.Change,
		Volume: r.Volume,
	}
}
