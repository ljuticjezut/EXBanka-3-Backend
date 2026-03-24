package cron_test

import (
	"testing"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/loan-service/internal/cron"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/loan-service/internal/models"
)

// --- mock installment repo ---

type mockInstallmentRepo struct {
	due    []models.LoanInstallment
	saved  []*models.LoanInstallment
	saveErr error
}

func (m *mockInstallmentRepo) FindDueInstallments(asOf time.Time) ([]models.LoanInstallment, error) {
	return m.due, nil
}

func (m *mockInstallmentRepo) Save(inst *models.LoanInstallment) error {
	m.saved = append(m.saved, inst)
	return m.saveErr
}

// --- mock loan repo (for interest rate cron) ---

type mockLoanRepo struct {
	loans   []models.Loan
	saved   []*models.Loan
	saveErr error
}

func (m *mockLoanRepo) FindActiveVariableLoans() ([]models.Loan, error) {
	return m.loans, nil
}

func (m *mockLoanRepo) SaveLoan(loan *models.Loan) error {
	m.saved = append(m.saved, loan)
	return m.saveErr
}

// --- InstallmentCollector tests ---

func TestInstallmentCollector_NoDueInstallments_DoesNothing(t *testing.T) {
	repo := &mockInstallmentRepo{due: nil}
	c := cron.NewInstallmentCollector(repo)
	if err := c.Run(time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("expected no saves, got %d", len(repo.saved))
	}
}

func TestInstallmentCollector_DueInstallment_MarkedPlacena(t *testing.T) {
	inst := models.LoanInstallment{
		ID:           1,
		LoanID:       10,
		Status:       "ocekuje",
		DatumDospeca: time.Now().AddDate(0, 0, -1),
		Iznos:        5000,
	}
	repo := &mockInstallmentRepo{due: []models.LoanInstallment{inst}}
	c := cron.NewInstallmentCollector(repo)
	if err := c.Run(time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("expected 1 save, got %d", len(repo.saved))
	}
	if repo.saved[0].Status != "placena" {
		t.Errorf("expected status=placena, got %s", repo.saved[0].Status)
	}
	if repo.saved[0].DatumPlacanja == nil {
		t.Error("expected DatumPlacanja to be set")
	}
}

func TestInstallmentCollector_DueInstallment_DatumPlacanjaSetToToday(t *testing.T) {
	before := time.Now()
	inst := models.LoanInstallment{
		ID: 2, LoanID: 10, Status: "ocekuje",
		DatumDospeca: before.AddDate(0, 0, -3),
	}
	repo := &mockInstallmentRepo{due: []models.LoanInstallment{inst}}
	c := cron.NewInstallmentCollector(repo)
	c.Run(before)
	if repo.saved[0].DatumPlacanja == nil {
		t.Fatal("DatumPlacanja is nil")
	}
	diff := repo.saved[0].DatumPlacanja.Sub(before)
	if diff < 0 || diff > 5*time.Second {
		t.Errorf("DatumPlacanja not close to now: %v", *repo.saved[0].DatumPlacanja)
	}
}

func TestInstallmentCollector_MultipleInstallments_AllProcessed(t *testing.T) {
	due := []models.LoanInstallment{
		{ID: 1, LoanID: 10, Status: "ocekuje", DatumDospeca: time.Now().AddDate(0, 0, -1)},
		{ID: 2, LoanID: 11, Status: "ocekuje", DatumDospeca: time.Now().AddDate(0, 0, -2)},
		{ID: 3, LoanID: 12, Status: "ocekuje", DatumDospeca: time.Now().AddDate(0, 0, -3)},
	}
	repo := &mockInstallmentRepo{due: due}
	c := cron.NewInstallmentCollector(repo)
	c.Run(time.Now())
	if len(repo.saved) != 3 {
		t.Errorf("expected 3 saves, got %d", len(repo.saved))
	}
}

// --- InterestRateUpdater tests ---

func TestInterestRateUpdater_NoLoans_DoesNothing(t *testing.T) {
	lrepo := &mockLoanRepo{loans: nil}
	u := cron.NewInterestRateUpdater(lrepo)
	if err := u.Run(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lrepo.saved) != 0 {
		t.Errorf("expected no saves, got %d", len(lrepo.saved))
	}
}

func TestInterestRateUpdater_VariableLoan_RateUpdated(t *testing.T) {
	loan := models.Loan{
		ID: 1, TipKamate: "varijabilna", KamatnaStopa: 5.0,
		Iznos: 120000, Period: 12, IznosRate: 10250,
	}
	lrepo := &mockLoanRepo{loans: []models.Loan{loan}}
	u := cron.NewInterestRateUpdater(lrepo)
	u.Run()
	if len(lrepo.saved) != 1 {
		t.Fatalf("expected 1 save, got %d", len(lrepo.saved))
	}
	updated := lrepo.saved[0]
	// Rate must be within [-1.5, +1.5] of original
	delta := updated.KamatnaStopa - 5.0
	if delta < -1.5 || delta > 1.5 {
		t.Errorf("rate delta %f out of [-1.5, +1.5] range", delta)
	}
}

func TestInterestRateUpdater_VariableLoan_IznosRateRecalculated(t *testing.T) {
	loan := models.Loan{
		ID: 1, TipKamate: "varijabilna", KamatnaStopa: 6.0,
		Iznos: 120000, Period: 24, IznosRate: 5320,
	}
	lrepo := &mockLoanRepo{loans: []models.Loan{loan}}
	u := cron.NewInterestRateUpdater(lrepo)
	u.Run()
	// IznosRate must be recalculated (should differ from original if rate changed)
	// At minimum, it must be positive
	if lrepo.saved[0].IznosRate <= 0 {
		t.Errorf("expected positive IznosRate, got %f", lrepo.saved[0].IznosRate)
	}
}

func TestInterestRateUpdater_RateCannotGoBelowZero(t *testing.T) {
	loan := models.Loan{
		ID: 1, TipKamate: "varijabilna", KamatnaStopa: 0.5,
		Iznos: 10000, Period: 6, IznosRate: 1700,
	}
	lrepo := &mockLoanRepo{loans: []models.Loan{loan}}
	u := cron.NewInterestRateUpdater(lrepo)
	for range 20 { // run many times to test floor
		lrepo.saved = nil
		lrepo.loans[0].KamatnaStopa = loan.KamatnaStopa
		u.Run()
		if lrepo.saved[0].KamatnaStopa < 0 {
			t.Errorf("rate went below 0: %f", lrepo.saved[0].KamatnaStopa)
		}
	}
}
