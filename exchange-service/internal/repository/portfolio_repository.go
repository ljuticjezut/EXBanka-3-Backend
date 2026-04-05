package repository

import (
	"errors"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"gorm.io/gorm"
)

type PortfolioRepository struct {
	db *gorm.DB
}

func NewPortfolioRepository(db *gorm.DB) *PortfolioRepository {
	return &PortfolioRepository{db: db}
}

// GetHoldingByUserAndAsset returns the holding for a user+asset pair, or nil if not found.
func (r *PortfolioRepository) GetHoldingByUserAndAsset(userID uint, userType string, assetID uint) (*models.PortfolioHoldingRecord, error) {
	var h models.PortfolioHoldingRecord
	err := r.db.Where("user_id = ? AND user_type = ? AND asset_id = ?", userID, userType, assetID).
		First(&h).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &h, nil
}

// GetHoldingByID returns a holding by primary key with Asset preloaded.
func (r *PortfolioRepository) GetHoldingByID(id uint) (*models.PortfolioHoldingRecord, error) {
	var h models.PortfolioHoldingRecord
	if err := r.db.Preload("Asset").Preload("Asset.Exchange").First(&h, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &h, nil
}

// ListHoldingsForUser returns all holdings for a user with Asset preloaded.
func (r *PortfolioRepository) ListHoldingsForUser(userID uint, userType string) ([]models.PortfolioHoldingRecord, error) {
	var records []models.PortfolioHoldingRecord
	if err := r.db.Preload("Asset").Preload("Asset.Exchange").
		Where("user_id = ? AND user_type = ? AND quantity > 0", userID, userType).
		Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// RecordBuyFill atomically upserts the portfolio holding for a buy fill.
// Creates a new holding if none exists; otherwise increases quantity and
// recalculates the weighted average buy price.
func (r *PortfolioRepository) RecordBuyFill(userID uint, userType string, assetID, accountID uint, filledQty float64, fillPrice float64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var h models.PortfolioHoldingRecord
		err := tx.Where("user_id = ? AND user_type = ? AND asset_id = ?", userID, userType, assetID).
			First(&h).Error

		now := time.Now().UTC()

		if errors.Is(err, gorm.ErrRecordNotFound) {
			// New holding.
			h = models.PortfolioHoldingRecord{
				UserID:      userID,
				UserType:    userType,
				AssetID:     assetID,
				AccountID:   accountID,
				Quantity:    filledQty,
				AvgBuyPrice: fillPrice,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			return tx.Create(&h).Error
		}
		if err != nil {
			return err
		}

		// Weighted average: (old_qty * old_avg + new_qty * new_price) / total_qty
		newQty := h.Quantity + filledQty
		newAvg := (h.Quantity*h.AvgBuyPrice + filledQty*fillPrice) / newQty

		return tx.Model(&h).Updates(map[string]interface{}{
			"quantity":      newQty,
			"avg_buy_price": newAvg,
			"updated_at":    now,
		}).Error
	})
}

// RecordSellFill atomically decreases the holding quantity and accumulates
// realized profit. Returns the realized profit for this fill.
func (r *PortfolioRepository) RecordSellFill(userID uint, userType string, assetID uint, filledQty float64, sellPrice float64) (realizedProfit float64, err error) {
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var h models.PortfolioHoldingRecord
		if txErr := tx.Where("user_id = ? AND user_type = ? AND asset_id = ?", userID, userType, assetID).
			First(&h).Error; txErr != nil {
			return txErr
		}

		realizedProfit = (sellPrice - h.AvgBuyPrice) * filledQty
		newQty := h.Quantity - filledQty
		if newQty < 0 {
			newQty = 0
		}

		return tx.Model(&h).Updates(map[string]interface{}{
			"quantity":        newQty,
			"realized_profit": h.RealizedProfit + realizedProfit,
			"updated_at":      time.Now().UTC(),
		}).Error
	})
	return
}

// SetHoldingPublic toggles the is_public flag on a holding (used for OTC shares).
func (r *PortfolioRepository) SetHoldingPublic(id uint, isPublic bool) error {
	return r.db.Model(&models.PortfolioHoldingRecord{}).Where("id = ?", id).Updates(map[string]interface{}{
		"is_public":  isPublic,
		"updated_at": time.Now().UTC(),
	}).Error
}
