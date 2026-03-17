package repository

import (
	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/models"
	"gorm.io/gorm"
)

type FirmaRepository struct {
	db *gorm.DB
}

func NewFirmaRepository(db *gorm.DB) *FirmaRepository {
	return &FirmaRepository{db: db}
}

func (r *FirmaRepository) Create(firma *models.Firma) error {
	return r.db.Create(firma).Error
}

func (r *FirmaRepository) FindByID(id uint) (*models.Firma, error) {
	var firma models.Firma
	if err := r.db.Preload("SifraDelatnosti").First(&firma, id).Error; err != nil {
		return nil, err
	}
	return &firma, nil
}

func (r *FirmaRepository) FindByMaticniBroj(maticniBroj string) (*models.Firma, error) {
	var firma models.Firma
	if err := r.db.Preload("SifraDelatnosti").Where("maticni_broj = ?", maticniBroj).First(&firma).Error; err != nil {
		return nil, err
	}
	return &firma, nil
}

func (r *FirmaRepository) FindAll() ([]models.Firma, error) {
	var firme []models.Firma
	if err := r.db.Preload("SifraDelatnosti").Find(&firme).Error; err != nil {
		return nil, err
	}
	return firme, nil
}
