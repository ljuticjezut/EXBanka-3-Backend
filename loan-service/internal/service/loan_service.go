package service

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/loan-service/internal/models"
)

// LoanRepositoryInterface allows mocking in tests.
type LoanRepositoryInterface interface {
	Create(loan *models.Loan) error
	FindByID(id uint) (*models.Loan, error)
	Save(loan *models.Loan) error
}

// InstallmentRepositoryInterface allows mocking in tests.
type InstallmentRepositoryInterface interface {
	CreateBatch(items []models.LoanInstallment) error
}

// LoanService handles loan request, approval, and rejection logic.
type LoanService struct {
	loanRepo        LoanRepositoryInterface
	installmentRepo InstallmentRepositoryInterface
}

func NewLoanService(loanRepo LoanRepositoryInterface, installmentRepo InstallmentRepositoryInterface) *LoanService {
	return &LoanService{loanRepo: loanRepo, installmentRepo: installmentRepo}
}

// CalculateInstallment computes the monthly annuity installment.
//
//	A = P * r * (1+r)^n / ((1+r)^n - 1)
//	where r = annualRate / 12 / 100
//
// Exported so tests and handlers can verify the formula directly.
func CalculateInstallment(amount, annualRate float64, months int) float64 {
	if annualRate == 0 {
		return math.Round(amount/float64(months)*100) / 100
	}
	r := annualRate / 12.0 / 100.0
	n := float64(months)
	factor := math.Pow(1+r, n)
	a := amount * r * factor / (factor - 1)
	return math.Round(a*100) / 100
}

// BaseInterestRate returns the base annual interest rate (%) for the given
// loan amount in RSD and interest type (fiksna / varijabilna).
// Exported for use in tests and handler previews.
func BaseInterestRate(amountRSD float64, tipKamate string) float64 {
	type band struct {
		limit      float64
		fiksna     float64
		varijabilna float64
	}
	bands := []band{
		{100_000, 6.5, 4.5},
		{500_000, 5.8, 3.8},
		{1_000_000, 5.2, 3.2},
		{5_000_000, 4.5, 2.5},
		{math.MaxFloat64, 4.0, 2.0},
	}
	for _, b := range bands {
		if amountRSD <= b.limit {
			if tipKamate == "varijabilna" {
				return b.varijabilna
			}
			return b.fiksna
		}
	}
	if tipKamate == "varijabilna" {
		return 2.0
	}
	return 4.0
}

// MarginForVrsta returns the type-specific margin (%) to add to the base rate.
// Exported for use in tests and handler previews.
func MarginForVrsta(vrsta string) float64 {
	margins := map[string]float64{
		"gotovinski":     1.5,
		"stambeni":       0.0,
		"auto":           0.5,
		"refinansirajuci": 0.0,
		"studentski":     -0.5,
	}
	return margins[vrsta]
}

// CreateLoanInput carries the data for a new loan request.
type CreateLoanInput struct {
	Vrsta      string
	BrojRacuna string
	Iznos      float64
	Period     int
	TipKamate  string
	ClientID   uint
	CurrencyID uint
	// EURIBORRate is used only for varijabilna loans; defaults to 0.
	EURIBORRate float64
}

// RequestLoan creates a new loan request (status = "zahtev").
func (s *LoanService) RequestLoan(input CreateLoanInput) (*models.Loan, error) {
	if !contains(models.ValidLoanTypes(), input.Vrsta) {
		return nil, fmt.Errorf("invalid vrsta: %s", input.Vrsta)
	}
	if !contains(models.ValidInterestTypes(), input.TipKamate) {
		return nil, fmt.Errorf("invalid tip kamate: %s", input.TipKamate)
	}
	if input.Iznos <= 0 {
		return nil, fmt.Errorf("iznos must be positive")
	}
	if input.Period < 1 {
		return nil, fmt.Errorf("period must be at least 1 month")
	}

	base := BaseInterestRate(input.Iznos, input.TipKamate)
	margin := MarginForVrsta(input.Vrsta)
	kamatnaStopa := base + margin
	if input.TipKamate == "varijabilna" {
		kamatnaStopa += input.EURIBORRate
	}

	iznosRate := CalculateInstallment(input.Iznos, kamatnaStopa, input.Period)

	loan := &models.Loan{
		Vrsta:          input.Vrsta,
		BrojRacuna:     input.BrojRacuna,
		BrojKredita:    generateLoanNumber(),
		Iznos:          input.Iznos,
		Period:         input.Period,
		KamatnaStopa:   kamatnaStopa,
		TipKamate:      input.TipKamate,
		DatumKreiranja: time.Now(),
		DatumDospeca:   time.Now().AddDate(0, input.Period, 0),
		IznosRate:      iznosRate,
		Status:         "zahtev",
		ClientID:       input.ClientID,
		CurrencyID:     input.CurrencyID,
	}

	if err := s.loanRepo.Create(loan); err != nil {
		return nil, fmt.Errorf("failed to save loan: %w", err)
	}
	return loan, nil
}

// ApproveLoan approves a loan request, sets it to "aktivan", and generates installments.
func (s *LoanService) ApproveLoan(loanID, zaposleniID uint) (*models.Loan, error) {
	loan, err := s.loanRepo.FindByID(loanID)
	if err != nil {
		return nil, fmt.Errorf("loan not found: %w", err)
	}
	if loan.Status != "zahtev" {
		return nil, fmt.Errorf("loan must be in status 'zahtev' to approve, got '%s'", loan.Status)
	}

	loan.Status = "aktivan"
	loan.ZaposleniID = &zaposleniID

	if err := s.loanRepo.Save(loan); err != nil {
		return nil, fmt.Errorf("failed to save loan: %w", err)
	}

	installments := generateInstallments(loan)
	if err := s.installmentRepo.CreateBatch(installments); err != nil {
		return nil, fmt.Errorf("failed to create installments: %w", err)
	}

	return loan, nil
}

// RejectLoan rejects a loan request and sets it to "odbijen".
func (s *LoanService) RejectLoan(loanID, zaposleniID uint) (*models.Loan, error) {
	loan, err := s.loanRepo.FindByID(loanID)
	if err != nil {
		return nil, fmt.Errorf("loan not found: %w", err)
	}
	if loan.Status != "zahtev" {
		return nil, fmt.Errorf("loan must be in status 'zahtev' to reject, got '%s'", loan.Status)
	}

	loan.Status = "odbijen"
	loan.ZaposleniID = &zaposleniID

	if err := s.loanRepo.Save(loan); err != nil {
		return nil, fmt.Errorf("failed to save loan: %w", err)
	}
	return loan, nil
}

// generateInstallments builds the full installment schedule for a loan.
func generateInstallments(loan *models.Loan) []models.LoanInstallment {
	installments := make([]models.LoanInstallment, loan.Period)
	now := time.Now()
	for i := range installments {
		installments[i] = models.LoanInstallment{
			LoanID:              loan.ID,
			RedniBroj:           i + 1,
			Iznos:               loan.IznosRate,
			KamataStopaSnapshot: loan.KamatnaStopa,
			DatumDospeca:        now.AddDate(0, i+1, 0),
			Status:              "ocekuje",
		}
	}
	return installments
}

// generateLoanNumber produces a unique loan number string.
func generateLoanNumber() string {
	return fmt.Sprintf("KRED-%d-%06d", time.Now().UnixMilli(), rand.Intn(1_000_000))
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
