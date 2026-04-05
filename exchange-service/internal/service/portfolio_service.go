package service

import (
	"fmt"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

// PortfolioService maintains PortfolioHoldingRecords in response to order fills.
type PortfolioService struct {
	portfolioRepo *repository.PortfolioRepository
}

func NewPortfolioService(portfolioRepo *repository.PortfolioRepository) *PortfolioService {
	return &PortfolioService{portfolioRepo: portfolioRepo}
}

// RecordFill updates the caller's portfolio after an order fill.
//
// Buy fill: upsert holding — increase quantity, recalculate weighted avg buy price.
// Sell fill: decrease quantity, accumulate realized profit on the holding.
//
// The effective quantity stored is fillQty × contractSize so that contracts
// (futures, options) are tracked at the underlying unit level.
func (s *PortfolioService) RecordFill(order *models.OrderRecord, fillQty int64, fillPrice float64) error {
	effectiveQty := float64(fillQty) * float64(order.ContractSize)

	if order.Direction == "buy" {
		if err := s.portfolioRepo.RecordBuyFill(
			order.UserID,
			order.UserType,
			order.AssetID,
			order.AccountID,
			effectiveQty,
			fillPrice,
		); err != nil {
			return fmt.Errorf("portfolio: failed to record buy fill for order %d: %w", order.ID, err)
		}
		return nil
	}

	// Sell: decrease holding and accumulate realized profit.
	_, err := s.portfolioRepo.RecordSellFill(
		order.UserID,
		order.UserType,
		order.AssetID,
		effectiveQty,
		fillPrice,
	)
	if err != nil {
		return fmt.Errorf("portfolio: failed to record sell fill for order %d: %w", order.ID, err)
	}
	return nil
}

// GetHoldingByID returns a single holding by ID.
func (s *PortfolioService) GetHoldingByID(id uint) (*models.PortfolioHoldingRecord, error) {
	h, err := s.portfolioRepo.GetHoldingByID(id)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, fmt.Errorf("holding not found")
	}
	return h, nil
}

// ListHoldings returns all active holdings for a user.
func (s *PortfolioService) ListHoldings(userID uint, userType string) ([]models.PortfolioHoldingRecord, error) {
	return s.portfolioRepo.ListHoldingsForUser(userID, userType)
}

// SetPublic toggles the is_public flag on a holding.
func (s *PortfolioService) SetPublic(holdingID uint, isPublic bool) error {
	return s.portfolioRepo.SetHoldingPublic(holdingID, isPublic)
}
