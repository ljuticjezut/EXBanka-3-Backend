package repository

import (
	"errors"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/employee-service/internal/models"
	"gorm.io/gorm"
)

type ActuaryProfileRepository struct {
	db *gorm.DB
}

func NewActuaryProfileRepository(db *gorm.DB) *ActuaryProfileRepository {
	return &ActuaryProfileRepository{db: db}
}

func (r *ActuaryProfileRepository) FindByEmployeeID(employeeID uint) (*models.ActuaryProfile, error) {
	var profile models.ActuaryProfile
	if err := r.db.Where("employee_id = ?", employeeID).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &profile, nil
}

func (r *ActuaryProfileRepository) Upsert(profile *models.ActuaryProfile) error {
	return r.db.Where("employee_id = ?", profile.EmployeeID).
		Assign(profile).
		FirstOrCreate(profile).Error
}

func (r *ActuaryProfileRepository) DeleteByEmployeeID(employeeID uint) error {
	return r.db.Where("employee_id = ?", employeeID).Delete(&models.ActuaryProfile{}).Error
}
