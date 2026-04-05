package repository

import (
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"gorm.io/gorm"
)

type TaxRepository struct {
	db *gorm.DB
}

func NewTaxRepository(db *gorm.DB) *TaxRepository {
	return &TaxRepository{db: db}
}

// CreateTaxRecord persists a new tax record.
func (r *TaxRepository) CreateTaxRecord(record *models.TaxRecord) error {
	return r.db.Create(record).Error
}

// ListTaxRecordsForUser returns all tax records for a user, optionally filtered by period ("YYYY-MM").
func (r *TaxRepository) ListTaxRecordsForUser(userID uint, userType, period string) ([]models.TaxRecord, error) {
	q := r.db.Where("user_id = ? AND user_type = ?", userID, userType)
	if period != "" {
		q = q.Where("period = ?", period)
	}
	var records []models.TaxRecord
	if err := q.Order("period DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// SumUnpaidTaxForUser returns the total unpaid tax amount for a user in a given period.
func (r *TaxRepository) SumUnpaidTaxForUser(userID uint, userType, period string) (float64, error) {
	var total float64
	err := r.db.Model(&models.TaxRecord{}).
		Where("user_id = ? AND user_type = ? AND period = ? AND status = 'unpaid'", userID, userType, period).
		Select("COALESCE(SUM(tax_rsd), 0)").
		Scan(&total).Error
	return total, err
}

// SumPaidTaxForUserYear returns the total paid tax amount for a user in a given year (e.g. "2026").
func (r *TaxRepository) SumPaidTaxForUserYear(userID uint, userType, year string) (float64, error) {
	var total float64
	err := r.db.Model(&models.TaxRecord{}).
		Where("user_id = ? AND user_type = ? AND period LIKE ? AND status = 'paid'", userID, userType, year+"-%").
		Select("COALESCE(SUM(tax_rsd), 0)").
		Scan(&total).Error
	return total, err
}

// ListAllTaxRecords returns all tax records, optionally filtered by period.
// Intended for supervisor use only.
func (r *TaxRepository) ListAllTaxRecords(period string) ([]models.TaxRecord, error) {
	q := r.db.Model(&models.TaxRecord{})
	if period != "" {
		q = q.Where("period = ?", period)
	}
	var records []models.TaxRecord
	if err := q.Order("period DESC, user_id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// TaxableUser identifies a user who has unpaid tax records.
type TaxableUser struct {
	UserID   uint
	UserType string
}

// ListDistinctUsersWithUnpaidTax returns all unique users who have unpaid tax
// records for the given period.
func (r *TaxRepository) ListDistinctUsersWithUnpaidTax(period string) ([]TaxableUser, error) {
	rows, err := r.db.Model(&models.TaxRecord{}).
		Select("DISTINCT user_id, user_type").
		Where("period = ? AND status = 'unpaid'", period).
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []TaxableUser
	for rows.Next() {
		var u TaxableUser
		if err := rows.Scan(&u.UserID, &u.UserType); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// MarkTaxRecordsPaid marks all unpaid records for a user+period as paid.
func (r *TaxRepository) MarkTaxRecordsPaid(userID uint, userType, period string) error {
	return r.db.Model(&models.TaxRecord{}).
		Where("user_id = ? AND user_type = ? AND period = ? AND status = 'unpaid'", userID, userType, period).
		Update("status", "paid").Error
}
