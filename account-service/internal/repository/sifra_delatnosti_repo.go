package repository

import (
	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/models"
	"gorm.io/gorm"
)

type SifraDelatnostiRepository struct {
	db *gorm.DB
}

func NewSifraDelatnostiRepository(db *gorm.DB) *SifraDelatnostiRepository {
	return &SifraDelatnostiRepository{db: db}
}

func (r *SifraDelatnostiRepository) FindAll() ([]models.SifraDelatnosti, error) {
	var sifre []models.SifraDelatnosti
	if err := r.db.Find(&sifre).Error; err != nil {
		return nil, err
	}
	return sifre, nil
}

func (r *SifraDelatnostiRepository) FindByID(id uint) (*models.SifraDelatnosti, error) {
	var sifra models.SifraDelatnosti
	if err := r.db.First(&sifra, id).Error; err != nil {
		return nil, err
	}
	return &sifra, nil
}
