package models

import (
	"time"

	"gorm.io/gorm"
)

type Employee struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Ime           string    `gorm:"not null" json:"ime"`
	Prezime       string    `gorm:"not null" json:"prezime"`
	DatumRodjenja time.Time `json:"datum_rodjenja"`
	Pol           string    `gorm:"not null" json:"pol"`
	Email         string    `gorm:"uniqueIndex;not null" json:"email"`
	BrojTelefona  string    `json:"broj_telefona"`
	Adresa        string    `json:"adresa"`
	Username      string    `gorm:"uniqueIndex;not null" json:"username"`
	Password      string    `gorm:"not null" json:"-"`
	SaltPassword  string    `gorm:"not null;column:salt_password" json:"-"`
	Pozicija      string    `json:"pozicija"`
	Departman     string    `json:"departman"`
	// Legacy trading flags kept only for safe transition/backfill.
	IsAgent        bool            `gorm:"default:false" json:"isAgent"`
	IsSupervisor   bool            `gorm:"default:false" json:"isSupervisor"`
	Limit          float64         `gorm:"column:trading_limit;default:0" json:"limit"`
	UsedLimit      float64         `gorm:"default:0" json:"usedLimit"`
	NeedApproval   bool            `gorm:"default:false" json:"needApproval"`
	Aktivan        bool            `gorm:"default:true" json:"aktivan"`
	Permissions    []Permission    `gorm:"many2many:employee_permissions;" json:"permissions,omitempty"`
	ActuaryProfile *ActuaryProfile `gorm:"foreignKey:EmployeeID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	DeletedAt      gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (e *Employee) IsAdmin() bool {
	for _, p := range e.Permissions {
		if p.Name == PermEmployeeAdmin {
			return true
		}
	}
	return false
}

func (e *Employee) PermissionNames() []string {
	names := make([]string, 0, len(e.Permissions))
	for _, p := range e.Permissions {
		names = append(names, p.Name)
	}
	return names
}

func (e *Employee) IsActuaryRole() bool {
	return HasEmployeeRole(e.PermissionNames(), PermEmployeeAgent)
}

func (e *Employee) IsSupervisorRole() bool {
	return HasEmployeeRole(e.PermissionNames(), PermEmployeeSupervisor)
}

func (e *Employee) IsAgentRole() bool {
	return e.IsActuaryRole() && !e.IsSupervisorRole()
}
