package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/employee-service/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openEmployeeBackfillTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.AutoMigrate(&models.Employee{}, &models.ActuaryProfile{}, &models.Permission{}, &models.Token{}); err != nil {
		t.Fatalf("failed to migrate employee tables: %v", err)
	}

	return db
}

func attachEmployeePermissions(t *testing.T, db *gorm.DB, emp *models.Employee, permissions ...models.Permission) {
	t.Helper()

	if len(permissions) == 0 {
		return
	}

	if err := db.Model(emp).Association("Permissions").Append(permissions); err != nil {
		t.Fatalf("failed to attach employee permissions: %v", err)
	}
}

func TestBackfillActuaryProfiles_CreatesProfilesFromActuaryHierarchy(t *testing.T) {
	db := openEmployeeBackfillTestDB(t, "employee_actuary_backfill")

	permissions := []models.Permission{
		{Name: models.PermEmployeeBasic, SubjectType: models.PermissionSubjectEmployee},
		{Name: models.PermEmployeeAgent, SubjectType: models.PermissionSubjectEmployee},
		{Name: models.PermEmployeeSupervisor, SubjectType: models.PermissionSubjectEmployee},
	}
	for i := range permissions {
		if err := db.Create(&permissions[i]).Error; err != nil {
			t.Fatalf("failed to seed permission %q: %v", permissions[i].Name, err)
		}
	}

	dateOfBirth := time.Date(1990, 1, 10, 0, 0, 0, 0, time.UTC)
	agent := models.Employee{
		Ime:           "Agent",
		Prezime:       "One",
		DatumRodjenja: dateOfBirth,
		Pol:           "M",
		Email:         "agent.test@bank.com",
		BrojTelefona:  "0611111111",
		Adresa:        "Agent Street 1",
		Username:      "agent-one",
		Password:      "pending",
		SaltPassword:  "pending",
		Pozicija:      "Agent",
		Departman:     "Trading",
		Limit:         125000,
		UsedLimit:     3200,
		Aktivan:       true,
	}
	supervisor := models.Employee{
		Ime:           "Supervisor",
		Prezime:       "One",
		DatumRodjenja: dateOfBirth,
		Pol:           "Z",
		Email:         "supervisor.test@bank.com",
		BrojTelefona:  "0622222222",
		Adresa:        "Supervisor Street 2",
		Username:      "supervisor-one",
		Password:      "pending",
		SaltPassword:  "pending",
		Pozicija:      "Supervisor",
		Departman:     "Trading",
		Limit:         999999,
		UsedLimit:     1800,
		Aktivan:       true,
	}
	basic := models.Employee{
		Ime:           "Basic",
		Prezime:       "One",
		DatumRodjenja: dateOfBirth,
		Pol:           "M",
		Email:         "basic.test@bank.com",
		BrojTelefona:  "0633333333",
		Adresa:        "Basic Street 3",
		Username:      "basic-one",
		Password:      "pending",
		SaltPassword:  "pending",
		Pozicija:      "Clerk",
		Departman:     "Ops",
		Aktivan:       true,
	}

	for _, emp := range []*models.Employee{&agent, &supervisor, &basic} {
		if err := db.Create(emp).Error; err != nil {
			t.Fatalf("failed to create employee %s: %v", emp.Email, err)
		}
	}

	attachEmployeePermissions(t, db, &agent, permissions[1])
	attachEmployeePermissions(t, db, &supervisor, permissions[2])
	attachEmployeePermissions(t, db, &basic, permissions[0])

	if err := BackfillActuaryProfiles(db); err != nil {
		t.Fatalf("backfill failed: %v", err)
	}

	var agentProfile models.ActuaryProfile
	if err := db.Where("employee_id = ?", agent.ID).First(&agentProfile).Error; err != nil {
		t.Fatalf("expected agent actuary profile: %v", err)
	}
	if agentProfile.Limit == nil || *agentProfile.Limit != 125000 {
		t.Fatalf("expected agent limit to be preserved, got %#v", agentProfile.Limit)
	}
	if agentProfile.UsedLimit != 3200 {
		t.Fatalf("expected agent usedLimit 3200, got %.2f", agentProfile.UsedLimit)
	}
	if !agentProfile.NeedApproval {
		t.Fatal("expected new agent profile to require approval by default")
	}

	var supervisorProfile models.ActuaryProfile
	if err := db.Where("employee_id = ?", supervisor.ID).First(&supervisorProfile).Error; err != nil {
		t.Fatalf("expected supervisor actuary profile: %v", err)
	}
	if supervisorProfile.Limit != nil {
		t.Fatalf("expected supervisor to have no limit, got %#v", supervisorProfile.Limit)
	}
	if supervisorProfile.UsedLimit != 1800 {
		t.Fatalf("expected supervisor usedLimit 1800, got %.2f", supervisorProfile.UsedLimit)
	}
	if supervisorProfile.NeedApproval {
		t.Fatal("expected supervisor profile to skip approval by default")
	}

	var basicCount int64
	if err := db.Model(&models.ActuaryProfile{}).Where("employee_id = ?", basic.ID).Count(&basicCount).Error; err != nil {
		t.Fatalf("failed to count basic employee profile rows: %v", err)
	}
	if basicCount != 0 {
		t.Fatalf("expected non-actuary employee to have no profile, got %d rows", basicCount)
	}
}
