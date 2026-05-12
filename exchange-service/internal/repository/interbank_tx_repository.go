package repository

import (
	"errors"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"gorm.io/gorm"
)

// InterbankPendingTxRepository persists the per-TransactionID state we
// hold between voting YES on a NEW_TX and seeing the matching
// COMMIT_TX / ROLLBACK_TX. The /interbank handler dedupes by
// idempotence key already; this repo is the business-layer record of
// "what we promised to do on commit".
type InterbankPendingTxRepository struct {
	db *gorm.DB
}

func NewInterbankPendingTxRepository(db *gorm.DB) *InterbankPendingTxRepository {
	return &InterbankPendingTxRepository{db: db}
}

// Create writes a new pending-tx row in status "pending". Timestamps
// are set here so callers don't have to remember.
func (r *InterbankPendingTxRepository) Create(p *models.InterbankPendingTx) error {
	p.CreatedAt = time.Now().UTC()
	if p.Status == "" {
		p.Status = models.InterbankPendingTxStatusPending
	}
	return r.db.Create(p).Error
}

// GetByTxID fetches a pending tx by the protocol transaction identity.
// Returns (nil, nil) when there's no row — used to detect duplicate
// COMMIT/ROLLBACK that arrived before NEW_TX (shouldn't happen in
// practice but the handler must be defensive).
func (r *InterbankPendingTxRepository) GetByTxID(txRoutingNumber int, txID string) (*models.InterbankPendingTx, error) {
	var row models.InterbankPendingTx
	err := r.db.
		Where("tx_routing_number = ? AND tx_id = ?", txRoutingNumber, txID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// MarkCommitted flips status → committed and stamps resolved_at. Safe
// to call on an already-committed row (no-op).
func (r *InterbankPendingTxRepository) MarkCommitted(txRoutingNumber int, txID string) error {
	now := time.Now().UTC()
	return r.db.Model(&models.InterbankPendingTx{}).
		Where("tx_routing_number = ? AND tx_id = ? AND status = ?",
			txRoutingNumber, txID, models.InterbankPendingTxStatusPending).
		Updates(map[string]interface{}{
			"status":      models.InterbankPendingTxStatusCommitted,
			"resolved_at": now,
		}).Error
}

// MarkRolledBack flips status → rolled_back and stamps resolved_at.
// Safe to call on an already-rolled-back row (no-op).
func (r *InterbankPendingTxRepository) MarkRolledBack(txRoutingNumber int, txID string) error {
	now := time.Now().UTC()
	return r.db.Model(&models.InterbankPendingTx{}).
		Where("tx_routing_number = ? AND tx_id = ? AND status = ?",
			txRoutingNumber, txID, models.InterbankPendingTxStatusPending).
		Updates(map[string]interface{}{
			"status":      models.InterbankPendingTxStatusRolledBack,
			"resolved_at": now,
		}).Error
}

// InterbankOptionContractRepository persists local option-contract rows
// formed via cross-bank OTC negotiations. We only write to this table
// when WE are the buyer's bank; when we're the seller, the local-side
// effect lives in the regular portfolio holding reservation.
type InterbankOptionContractRepository struct {
	db *gorm.DB
}

func NewInterbankOptionContractRepository(db *gorm.DB) *InterbankOptionContractRepository {
	return &InterbankOptionContractRepository{db: db}
}

// Create writes a new contract row. Timestamps are set here.
func (r *InterbankOptionContractRepository) Create(c *models.InterbankOptionContract) error {
	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = models.InterbankOptionContractStatusValid
	}
	return r.db.Create(c).Error
}

// Get fetches a contract by the negotiation identity (= the contract's
// global identity per spec §3.6.1). Returns (nil, nil) when no row
// exists.
func (r *InterbankOptionContractRepository) Get(negotiationRoutingNumber int, negotiationID string) (*models.InterbankOptionContract, error) {
	var row models.InterbankOptionContract
	err := r.db.
		Where("negotiation_routing_number = ? AND negotiation_id = ?", negotiationRoutingNumber, negotiationID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}
