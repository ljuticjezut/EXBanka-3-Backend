package repository

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
)

// RemotePublicStockRepository persists one cached /public-stock response
// per partner bank. The cron writes through Upsert*; the handler reads
// through List.
type RemotePublicStockRepository struct {
	db *gorm.DB
}

func NewRemotePublicStockRepository(db *gorm.DB) *RemotePublicStockRepository {
	return &RemotePublicStockRepository{db: db}
}

// UpsertPayload writes a successful refresh: replaces payload_json, clears
// last_error, bumps fetched_at + updated_at. The upsert is on the
// primary key (partner_routing_number), so one row per partner.
func (r *RemotePublicStockRepository) UpsertPayload(partner int, payloadJSON string) error {
	now := time.Now().UTC()
	row := models.RemotePublicStockSnapshot{
		PartnerRoutingNumber: partner,
		PayloadJSON:          payloadJSON,
		LastError:            "",
		FetchedAt:            now,
		UpdatedAt:            now,
	}
	err := r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "partner_routing_number"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"payload_json": payloadJSON,
			"last_error":   "",
			"fetched_at":   now,
			"updated_at":   now,
		}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("upserting public-stock snapshot for partner %d: %w", partner, err)
	}
	return nil
}

// UpsertError records a failed refresh. We keep the previously-cached
// payload_json untouched (stale-but-good is better than blank) and
// only bump last_error + updated_at. Insert-or-update so a partner
// that's never succeeded still gets a row visible to the handler.
func (r *RemotePublicStockRepository) UpsertError(partner int, errMsg string) error {
	now := time.Now().UTC()
	existing := models.RemotePublicStockSnapshot{
		PartnerRoutingNumber: partner,
		PayloadJSON:          "",
		LastError:            errMsg,
		FetchedAt:            now,
		UpdatedAt:            now,
	}
	err := r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "partner_routing_number"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"last_error": errMsg,
			"updated_at": now,
		}),
	}).Create(&existing).Error
	if err != nil {
		return fmt.Errorf("recording public-stock snapshot error for partner %d: %w", partner, err)
	}
	return nil
}

// List returns every cached snapshot, in insertion order. Caller
// iterates and unmarshals the payloads.
func (r *RemotePublicStockRepository) List() ([]models.RemotePublicStockSnapshot, error) {
	var rows []models.RemotePublicStockSnapshot
	if err := r.db.Order("partner_routing_number ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing public-stock snapshots: %w", err)
	}
	return rows, nil
}

// Get returns a single partner's snapshot, or nil if no row exists.
func (r *RemotePublicStockRepository) Get(partner int) (*models.RemotePublicStockSnapshot, error) {
	var row models.RemotePublicStockSnapshot
	err := r.db.Where("partner_routing_number = ?", partner).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("loading public-stock snapshot for partner %d: %w", partner, err)
	}
	return &row, nil
}
