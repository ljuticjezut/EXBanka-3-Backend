package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/util"
)

func TestRequireMarketReadAccessHTTP_AllowsClientTrading(t *testing.T) {
	rec := httptest.NewRecorder()
	claims := &util.Claims{
		ClientID:    42,
		TokenSource: "client",
		Permissions: []string{models.PermClientTrading},
	}

	if !requireMarketReadAccessHTTP(rec, claims) {
		t.Fatal("expected client with trading permission to be authorized")
	}
}

func TestRequireMarketReadAccessHTTP_RejectsClientWithoutTrading(t *testing.T) {
	rec := httptest.NewRecorder()
	claims := &util.Claims{
		ClientID:    42,
		TokenSource: "client",
		Permissions: []string{models.PermClientBasic},
	}

	if requireMarketReadAccessHTTP(rec, claims) {
		t.Fatal("expected client without trading permission to be rejected")
	}
	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequireMarketReadAccessHTTP_AllowsEmployeeAgentHierarchy(t *testing.T) {
	rec := httptest.NewRecorder()
	claims := &util.Claims{
		EmployeeID:  7,
		TokenSource: "employee",
		Permissions: []string{models.PermEmployeeSupervisor},
	}

	if !requireMarketReadAccessHTTP(rec, claims) {
		t.Fatal("expected employee supervisor to be authorized through agent hierarchy")
	}
}

func TestRequireMarketReadAccessHTTP_AllowsEmployeeAgent(t *testing.T) {
	rec := httptest.NewRecorder()
	claims := &util.Claims{
		EmployeeID:  9,
		TokenSource: "employee",
		Permissions: []string{models.PermEmployeeAgent},
	}

	if !requireMarketReadAccessHTTP(rec, claims) {
		t.Fatal("expected employee agent to be authorized")
	}
}

func TestRequireMarketReadAccessHTTP_AllowsEmployeeAdmin(t *testing.T) {
	rec := httptest.NewRecorder()
	claims := &util.Claims{
		EmployeeID:  11,
		TokenSource: "employee",
		Permissions: []string{models.PermEmployeeAdmin},
	}

	if !requireMarketReadAccessHTTP(rec, claims) {
		t.Fatal("expected employee admin to be authorized through agent hierarchy")
	}
}

func TestRequireMarketReadAccessHTTP_RejectsEmployeeWithoutActuaryRole(t *testing.T) {
	rec := httptest.NewRecorder()
	claims := &util.Claims{
		EmployeeID:  7,
		TokenSource: "employee",
		Permissions: []string{models.PermEmployeeBasic},
	}

	if requireMarketReadAccessHTTP(rec, claims) {
		t.Fatal("expected employee basic to be rejected")
	}
	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
