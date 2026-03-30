package database

import (
	"log/slog"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/employee-service/internal/models"
	"gorm.io/gorm"
)

func BackfillActuaryProfiles(db *gorm.DB) error {
	var employees []models.Employee
	if err := db.Preload("Permissions").Find(&employees).Error; err != nil {
		return err
	}

	for _, employee := range employees {
		var existing models.ActuaryProfile
		existingResult := db.Where("employee_id = ?", employee.ID).First(&existing)
		hasExisting := existingResult.Error == nil
		if existingResult.Error != nil && existingResult.Error != gorm.ErrRecordNotFound {
			return existingResult.Error
		}

		if !employee.IsActuaryRole() {
			if hasExisting {
				if err := db.Delete(&existing).Error; err != nil {
					return err
				}
			}
			continue
		}

		profile := models.ActuaryProfile{
			EmployeeID: employee.ID,
			UsedLimit:  employee.UsedLimit,
		}

		if hasExisting {
			profile = existing
		}

		if employee.IsSupervisorRole() {
			profile.Limit = nil
			profile.NeedApproval = false
		} else {
			if hasExisting && existing.Limit != nil {
				limit := *existing.Limit
				profile.Limit = &limit
			} else {
				limit := employee.Limit
				if limit == 0 {
					limit = models.DefaultAgentTradingLimit
				}
				profile.Limit = &limit
			}
			if hasExisting {
				profile.NeedApproval = existing.NeedApproval
			} else {
				profile.NeedApproval = true
			}
		}

		if err := db.Where("employee_id = ?", employee.ID).Assign(profile).FirstOrCreate(&profile).Error; err != nil {
			return err
		}
	}

	slog.Info("Actuary profile backfill complete")
	return nil
}
