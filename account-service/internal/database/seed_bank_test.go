package database_test

import (
	"testing"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/database"
)

// BankSeedData returns the canonical bank Firma fields and the 8 currency codes
// that SeedBankAccounts will use. These tests verify the BUSINESS DATA is correct
// (MaticniBroj, PIB, currency list) without requiring a running database.

func TestBankFirmaMaticniBroj(t *testing.T) {
	data := database.BankFirmaData()
	if data.MaticniBroj != "99999999" {
		t.Errorf("expected MaticniBroj '99999999', got %q", data.MaticniBroj)
	}
}

func TestBankFirmaPIB(t *testing.T) {
	data := database.BankFirmaData()
	if data.PIB != "999999999" {
		t.Errorf("expected PIB '999999999', got %q", data.PIB)
	}
}

func TestBankFirmaNaziv(t *testing.T) {
	data := database.BankFirmaData()
	if data.Naziv != "EXBanka 3 DOO" {
		t.Errorf("expected Naziv 'EXBanka 3 DOO', got %q", data.Naziv)
	}
}

func TestBankFirmaAdresa(t *testing.T) {
	data := database.BankFirmaData()
	if data.Adresa == "" {
		t.Error("expected non-empty Adresa")
	}
}

func TestBankCurrencyCodes_Has8Entries(t *testing.T) {
	codes := database.BankCurrencyCodes()
	if len(codes) != 8 {
		t.Errorf("expected 8 bank currency codes, got %d", len(codes))
	}
}

func TestBankCurrencyCodes_ContainsRSD(t *testing.T) {
	codes := database.BankCurrencyCodes()
	for _, c := range codes {
		if c == "RSD" {
			return
		}
	}
	t.Error("expected RSD in bank currency codes")
}

func TestBankCurrencyCodes_ContainsAllExpected(t *testing.T) {
	expected := []string{"RSD", "EUR", "USD", "GBP", "CHF", "JPY", "CAD", "AUD"}
	codes := database.BankCurrencyCodes()
	codeSet := make(map[string]bool)
	for _, c := range codes {
		codeSet[c] = true
	}
	for _, e := range expected {
		if !codeSet[e] {
			t.Errorf("expected currency %s in BankCurrencyCodes", e)
		}
	}
}
