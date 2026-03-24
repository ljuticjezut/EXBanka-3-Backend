package repository

import (
	"github.com/RAF-SI-2025/EXBanka-3-Backend/loan-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/loan-service/internal/service"
	"gorm.io/gorm"
)

type LoanRepository struct {
	db *gorm.DB
}

func NewLoanRepository(db *gorm.DB) *LoanRepository {
	return &LoanRepository{db: db}
}

func (r *LoanRepository) Create(loan *models.Loan) error {
	return r.db.Create(loan).Error
}

func (r *LoanRepository) FindByID(id uint) (*models.Loan, error) {
	var loan models.Loan
	if err := r.db.First(&loan, id).Error; err != nil {
		return nil, err
	}
	return &loan, nil
}

func (r *LoanRepository) Save(loan *models.Loan) error {
	return r.db.Save(loan).Error
}

// ListByClientID returns all loans for a client ordered descending by amount.
func (r *LoanRepository) ListByClientID(clientID uint) ([]models.Loan, error) {
	var loans []models.Loan
	if err := r.db.Where("client_id = ?", clientID).
		Order("iznos DESC").
		Find(&loans).Error; err != nil {
		return nil, err
	}
	return loans, nil
}

// ListByStatus returns all loans with the given status.
func (r *LoanRepository) ListByStatus(status string) ([]models.Loan, error) {
	var loans []models.Loan
	if err := r.db.Where("status = ?", status).
		Order("created_at ASC").
		Find(&loans).Error; err != nil {
		return nil, err
	}
	return loans, nil
}

// FindActiveVariableLoans returns all active loans with variable interest rate.
func (r *LoanRepository) FindActiveVariableLoans() ([]models.Loan, error) {
	var loans []models.Loan
	if err := r.db.Where("status = ? AND tip_kamate = ?", "aktivan", "varijabilna").
		Find(&loans).Error; err != nil {
		return nil, err
	}
	return loans, nil
}

// SaveLoan persists changes to an existing loan.
func (r *LoanRepository) SaveLoan(loan *models.Loan) error {
	return r.db.Save(loan).Error
}

// ListFiltered returns loans matching the non-empty fields of the filter.
func (r *LoanRepository) ListFiltered(filter service.LoanFilter) ([]models.Loan, error) {
	var loans []models.Loan
	q := r.db.Order("created_at ASC")
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.Vrsta != "" {
		q = q.Where("vrsta = ?", filter.Vrsta)
	}
	if filter.BrojRacuna != "" {
		q = q.Where("broj_racuna = ?", filter.BrojRacuna)
	}
	if err := q.Find(&loans).Error; err != nil {
		return nil, err
	}
	return loans, nil
}
