package handler

import (
	"net/http"
	"strconv"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/employee-service/internal/config"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/employee-service/internal/models"
	svc "github.com/RAF-SI-2025/EXBanka-3-Backend/employee-service/internal/service"
)

type ActuaryHTTPHandler struct {
	cfg *config.Config
	svc *svc.EmployeeService
}

func NewActuaryHTTPHandler(cfg *config.Config, svc *svc.EmployeeService) *ActuaryHTTPHandler {
	return &ActuaryHTTPHandler{cfg: cfg, svc: svc}
}

func (h *ActuaryHTTPHandler) ListActuaries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims, ok := requireAuthenticatedEmployeeHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireEmployeePermissionHTTP(w, claims, models.PermEmployeeSupervisor) {
		return
	}

	items, err := h.svc.ListActuaryStates()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to load actuaries"})
		return
	}

	response := make([]actuaryManagementResponse, 0, len(items))
	for _, item := range items {
		response = append(response, actuaryManagementToResponse(item))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"actuaries": response,
		"count":     len(response),
	})
}

type actuaryManagementResponse struct {
	EmployeeID      string   `json:"employeeId"`
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

func actuaryManagementToResponse(item models.ActuaryManagementItem) actuaryManagementResponse {
	return actuaryManagementResponse{
		EmployeeID:      strconv.FormatUint(uint64(item.EmployeeID), 10),
		Ime:             item.Ime,
		Prezime:         item.Prezime,
		Email:           item.Email,
		Username:        item.Username,
		Pozicija:        item.Pozicija,
		Departman:       item.Departman,
		Aktivan:         item.Aktivan,
		PermissionNames: item.PermissionNames,
		IsActuary:       item.IsActuary,
		IsSupervisor:    item.IsSupervisor,
		Limit:           item.Limit,
		UsedLimit:       item.UsedLimit,
		NeedApproval:    item.NeedApproval,
	}
}
