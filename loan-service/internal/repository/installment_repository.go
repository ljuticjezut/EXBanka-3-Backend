package repository

import (
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/loan-service/internal/models"
	"gorm.io/gorm"
)

type InstallmentRepository struct {
	db *gorm.DB
}

func NewInstallmentRepository(db *gorm.DB) *InstallmentRepository {
	return &InstallmentRepository{db: db}
}

func (r *InstallmentRepository) CreateBatch(items []models.LoanInstallment) error {
	return r.db.Create(&items).Error
}

// FindDueInstallments returns all installments with datum_dospeca <= asOf and status = "ocekuje".
func (r *InstallmentRepository) FindDueInstallments(asOf time.Time) ([]models.LoanInstallment, error) {
	var items []models.LoanInstallment
	if err := r.db.Where("datum_dospeca <= ? AND status = ?", asOf, "ocekuje").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// Save persists changes to an existing installment.
func (r *InstallmentRepository) Save(inst *models.LoanInstallment) error {
	return r.db.Save(inst).Error
}

// ListByLoanID returns all installments for a loan ordered by RedniBroj.
func (r *InstallmentRepository) ListByLoanID(loanID uint) ([]models.LoanInstallment, error) {
	var items []models.LoanInstallment
	if err := r.db.Where("loan_id = ?", loanID).
		Order("redni_broj ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
