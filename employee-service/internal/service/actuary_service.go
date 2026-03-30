package service

import (
	"fmt"
	"sort"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/employee-service/internal/models"
)

func (s *EmployeeService) GetActuaryState(employeeID uint) (*models.ActuaryState, error) {
	emp, err := s.employeeRepo.FindByID(employeeID)
	if err != nil {
		return nil, fmt.Errorf("employee not found")
	}

	if !emp.IsActuaryRole() {
		return &models.ActuaryState{
			EmployeeID:   employeeID,
			IsActuary:    false,
			IsSupervisor: false,
		}, nil
	}

	profile, err := s.actuaryRepo.FindByEmployeeID(employeeID)
	if err != nil {
		return nil, err
	}

	if profile == nil {
		profile, err = s.syncActuaryProfile(emp)
		if err != nil {
			return nil, err
		}
	}

	return &models.ActuaryState{
		EmployeeID:   employeeID,
		IsActuary:    true,
		IsSupervisor: emp.IsSupervisorRole(),
		Limit:        cloneLimit(profile.Limit),
		UsedLimit:    profile.UsedLimit,
		NeedApproval: profile.NeedApproval,
	}, nil
}

func (s *EmployeeService) ListActuaryStates() ([]models.ActuaryManagementItem, error) {
	employees, err := s.employeeRepo.ListAll()
	if err != nil {
		return nil, err
	}

	items := make([]models.ActuaryManagementItem, 0)
	for i := range employees {
		emp := &employees[i]
		if !emp.IsActuaryRole() {
			continue
		}

		profile, err := s.actuaryRepo.FindByEmployeeID(emp.ID)
		if err != nil {
			return nil, err
		}
		if profile == nil {
			profile, err = s.syncActuaryProfile(emp)
			if err != nil {
				return nil, err
			}
		}

		items = append(items, models.ActuaryManagementItem{
			EmployeeID:      emp.ID,
			Ime:             emp.Ime,
			Prezime:         emp.Prezime,
			Email:           emp.Email,
			Username:        emp.Username,
			Pozicija:        emp.Pozicija,
			Departman:       emp.Departman,
			Aktivan:         emp.Aktivan,
			PermissionNames: emp.PermissionNames(),
			IsActuary:       true,
			IsSupervisor:    emp.IsSupervisorRole(),
			Limit:           cloneLimit(profile.Limit),
			UsedLimit:       profile.UsedLimit,
			NeedApproval:    profile.NeedApproval,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsSupervisor != items[j].IsSupervisor {
			return items[i].IsSupervisor && !items[j].IsSupervisor
		}
		if items[i].Prezime != items[j].Prezime {
			return items[i].Prezime < items[j].Prezime
		}
		return items[i].Ime < items[j].Ime
	})

	return items, nil
}

func (s *EmployeeService) syncActuaryProfile(emp *models.Employee) (*models.ActuaryProfile, error) {
	if !emp.IsActuaryRole() {
		if err := s.actuaryRepo.DeleteByEmployeeID(emp.ID); err != nil {
			return nil, err
		}
		return nil, nil
	}

	existing, err := s.actuaryRepo.FindByEmployeeID(emp.ID)
	if err != nil {
		return nil, err
	}

	profile := &models.ActuaryProfile{
		EmployeeID: emp.ID,
	}
	if existing != nil {
		profile = existing.Clone()
	}

	if profile.UsedLimit == 0 {
		profile.UsedLimit = emp.UsedLimit
	}

	if emp.IsSupervisorRole() {
		profile.Limit = nil
		profile.NeedApproval = false
	} else {
		if profile.Limit == nil {
			limit := emp.Limit
			if limit == 0 {
				limit = models.DefaultAgentTradingLimit
			}
			profile.Limit = &limit
		}
		if existing == nil {
			profile.NeedApproval = true
		}
	}

	if err := s.actuaryRepo.Upsert(profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func cloneLimit(limit *float64) *float64 {
	if limit == nil {
		return nil
	}
	copy := *limit
	return &copy
}
