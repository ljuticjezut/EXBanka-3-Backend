package repository_test

import (
	"testing"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/repository"
)

// Compile-time interface compliance checks are in the implementation files.
// These tests verify that the constructors exist and return the correct types.

func TestNewFirmaRepository_ReturnsNonNil(t *testing.T) {
	repo := repository.NewFirmaRepository(nil)
	if repo == nil {
		t.Error("expected non-nil FirmaRepository")
	}
}

func TestNewSifraDelatnostiRepository_ReturnsNonNil(t *testing.T) {
	repo := repository.NewSifraDelatnostiRepository(nil)
	if repo == nil {
		t.Error("expected non-nil SifraDelatnostiRepository")
	}
}
