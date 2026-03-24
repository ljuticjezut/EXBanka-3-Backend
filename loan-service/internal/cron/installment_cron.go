package cron

import (
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/loan-service/internal/models"
)

// installmentRepo is the subset of InstallmentRepository used by InstallmentCollector.
type installmentRepo interface {
	FindDueInstallments(asOf time.Time) ([]models.LoanInstallment, error)
	Save(inst *models.LoanInstallment) error
}

// InstallmentCollector processes due installments once per run.
type InstallmentCollector struct {
	repo installmentRepo
}

func NewInstallmentCollector(repo installmentRepo) *InstallmentCollector {
	return &InstallmentCollector{repo: repo}
}

// Run finds all installments due on or before asOf and marks them as "placena".
// In a real deployment, payment would be deducted from the client's account via
// the account-service before marking as paid; failed payments would be marked "kasni".
func (c *InstallmentCollector) Run(asOf time.Time) error {
	installments, err := c.repo.FindDueInstallments(asOf)
	if err != nil {
		return err
	}

	now := time.Now()
	for i := range installments {
		inst := &installments[i]
		inst.Status = "placena"
		inst.DatumPlacanja = &now
		if err := c.repo.Save(inst); err != nil {
			slog.Error("Failed to save installment", "id", inst.ID, "error", err)
			continue
		}
		slog.Info("Installment collected", "id", inst.ID, "loan_id", inst.LoanID, "iznos", inst.Iznos)
	}

	slog.Info("Installment collection run complete", "processed", len(installments))
	return nil
}

// loanRepo is the subset of LoanRepository used by InterestRateUpdater.
type loanRepo interface {
	FindActiveVariableLoans() ([]models.Loan, error)
	SaveLoan(loan *models.Loan) error
}

// InterestRateUpdater applies a monthly EURIBOR-style random adjustment to variable loans.
type InterestRateUpdater struct {
	repo loanRepo
}

func NewInterestRateUpdater(repo loanRepo) *InterestRateUpdater {
	return &InterestRateUpdater{repo: repo}
}

// Run adjusts each variable-rate loan's interest rate by a random delta in [-1.5%, +1.5%],
// recalculates the monthly installment, and saves.
func (u *InterestRateUpdater) Run() error {
	loans, err := u.repo.FindActiveVariableLoans()
	if err != nil {
		return err
	}

	for i := range loans {
		loan := &loans[i]

		// Random delta in [-1.5, +1.5] percent.
		delta := (rand.Float64()*3.0 - 1.5) // [-1.5, +1.5]
		newRate := math.Max(0, loan.KamatnaStopa+delta)
		loan.KamatnaStopa = newRate
		loan.IznosRate = annuity(loan.Iznos, newRate, loan.Period)

		if err := u.repo.SaveLoan(loan); err != nil {
			slog.Error("Failed to save loan after rate update", "id", loan.ID, "error", err)
			continue
		}
		slog.Info("Interest rate updated", "loan_id", loan.ID, "delta", delta, "new_rate", newRate)
	}

	slog.Info("Interest rate update run complete", "updated", len(loans))
	return nil
}

// annuity computes the monthly annuity payment: M = P * r*(1+r)^n / ((1+r)^n - 1)
// where r = annual rate / 12 / 100.
func annuity(principal, annualRate float64, months int) float64 {
	if annualRate == 0 {
		return principal / float64(months)
	}
	r := annualRate / 12.0 / 100.0
	n := float64(months)
	return principal * r * math.Pow(1+r, n) / (math.Pow(1+r, n) - 1)
}
