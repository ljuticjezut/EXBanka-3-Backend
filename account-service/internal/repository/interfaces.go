package repository

import "github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/models"

type FirmaRepositoryInterface interface {
	Create(firma *models.Firma) error
	FindByID(id uint) (*models.Firma, error)
	FindByMaticniBroj(maticniBroj string) (*models.Firma, error)
	FindAll() ([]models.Firma, error)
}

type SifraDelatnostiRepositoryInterface interface {
	FindAll() ([]models.SifraDelatnosti, error)
	FindByID(id uint) (*models.SifraDelatnosti, error)
}

// Compile-time interface compliance checks.
var _ FirmaRepositoryInterface = (*FirmaRepository)(nil)
var _ SifraDelatnostiRepositoryInterface = (*SifraDelatnostiRepository)(nil)
