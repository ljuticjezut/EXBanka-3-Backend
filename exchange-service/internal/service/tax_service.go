package service

import (
	"log/slog"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

const (
	taxRate        = 0.15  // 15% capital gains tax
	rsdCurrency    = "RSD"
	fallbackRSDRate = 1.0  // used when no forex pair is found (profit treated as RSD)
)

// TaxService records capital gains tax for sell transactions.
type TaxService struct {
	taxRepo    *repository.TaxRepository
	marketRepo *repository.MarketRepository
}

func NewTaxService(taxRepo *repository.TaxRepository, marketRepo *repository.MarketRepository) *TaxService {
	return &TaxService{taxRepo: taxRepo, marketRepo: marketRepo}
}

// RecordCapitalGainTax converts the realised profit to RSD and persists a
// TaxRecord if the profit is positive.
//
// Only applies to stock and option assets (not forex or futures).
//
// Parameters:
//   - realizedProfit: already-computed profit in the asset's native currency
//   - assetType: "stock", "option", "forex", "futures"
//   - currency: the exchange currency the asset trades in (e.g. "USD", "EUR")
func (s *TaxService) RecordCapitalGainTax(
	userID uint,
	userType string,
	assetID uint,
	realizedProfit float64,
	assetType, currency string,
) error {
	// Capital gains tax applies to stocks and options only.
	if assetType != "stock" && assetType != "option" {
		return nil
	}

	if realizedProfit <= 0 {
		return nil // no taxable gain
	}

	profit := realizedProfit

	// Convert profit to RSD.
	profitRSD := s.toRSD(profit, currency)
	taxRSD := roundPnL(profitRSD * taxRate)

	now := time.Now().UTC()
	record := &models.TaxRecord{
		UserID:    userID,
		UserType:  userType,
		AssetID:   assetID,
		Period:    now.Format("2006-01"),
		ProfitRSD: roundPnL(profitRSD),
		TaxRSD:    taxRSD,
		Status:    "unpaid",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.taxRepo.CreateTaxRecord(record); err != nil {
		slog.Error("tax service: failed to create tax record",
			"userID", userID, "assetID", assetID, "error", err)
		return err
	}

	slog.Info("tax service: recorded capital gain",
		"userID", userID, "assetID", assetID,
		"profitRSD", profitRSD, "taxRSD", taxRSD, "period", record.Period)
	return nil
}

// ListTaxRecords returns all tax records for a user, optionally filtered by period.
func (s *TaxService) ListTaxRecords(userID uint, userType, period string) ([]models.TaxRecord, error) {
	return s.taxRepo.ListTaxRecordsForUser(userID, userType, period)
}

// ListAllTaxRecords returns all tax records for all users (supervisor view), optionally
// filtered by period.
func (s *TaxService) ListAllTaxRecords(period string) ([]models.TaxRecord, error) {
	return s.taxRepo.ListAllTaxRecords(period)
}

// SumUnpaidTax returns the total unpaid tax for a user in a given period.
func (s *TaxService) SumUnpaidTax(userID uint, userType, period string) (float64, error) {
	return s.taxRepo.SumUnpaidTaxForUser(userID, userType, period)
}

// SumPaidTaxForYear returns the total paid tax for a user in a given calendar year (e.g. "2026").
func (s *TaxService) SumPaidTaxForYear(userID uint, userType, year string) (float64, error) {
	return s.taxRepo.SumPaidTaxForUserYear(userID, userType, year)
}

// toRSD converts an amount from the given currency to RSD.
// Falls back to the original amount when no forex pair is found.
func (s *TaxService) toRSD(amount float64, currency string) float64 {
	if currency == rsdCurrency || currency == "" {
		return amount
	}
	rate, err := s.marketRepo.GetForexRate(currency, rsdCurrency)
	if err != nil || rate == 0 {
		slog.Warn("tax service: no forex rate found, using 1:1 fallback",
			"from", currency, "to", rsdCurrency)
		return amount * fallbackRSDRate
	}
	return amount * rate
}
