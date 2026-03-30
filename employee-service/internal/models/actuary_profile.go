package models

import "time"

const DefaultAgentTradingLimit = 0.0

type ActuaryProfile struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	EmployeeID   uint      `gorm:"not null;uniqueIndex" json:"employeeId"`
	Limit        *float64  `gorm:"column:trading_limit" json:"limit,omitempty"`
	UsedLimit    float64   `gorm:"default:0" json:"usedLimit"`
	NeedApproval bool      `gorm:"default:false" json:"needApproval"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ActuaryState struct {
	EmployeeID   uint     `json:"employeeId"`
	IsActuary    bool     `json:"isActuary"`
	IsSupervisor bool     `json:"isSupervisor"`
	Limit        *float64 `json:"limit,omitempty"`
	UsedLimit    float64  `json:"usedLimit"`
	NeedApproval bool     `json:"needApproval"`
}

type ActuaryManagementItem struct {
	EmployeeID      uint     `json:"employeeId"`
	Ime             string   `json:"ime"`
	Prezime         string   `json:"prezime"`
	Email           string   `json:"email"`
	Username        string   `json:"username"`
	Pozicija        string   `json:"pozicija"`
	Departman       string   `json:"departman"`
	Aktivan         bool     `json:"aktivan"`
	PermissionNames []string `json:"permissionNames"`
	IsActuary       bool     `json:"isActuary"`
	IsSupervisor    bool     `json:"isSupervisor"`
	Limit           *float64 `json:"limit,omitempty"`
	UsedLimit       float64  `json:"usedLimit"`
	NeedApproval    bool     `json:"needApproval"`
}

func (a *ActuaryProfile) Clone() *ActuaryProfile {
	if a == nil {
		return nil
	}
	copy := *a
	if a.Limit != nil {
		limit := *a.Limit
		copy.Limit = &limit
	}
	return &copy
}
