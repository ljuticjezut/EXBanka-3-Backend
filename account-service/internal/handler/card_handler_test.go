package handler_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/handler"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/service"
)

// --- mock card service ---

type mockCardSvc struct {
	createdCard *models.Card
	foundCard   *models.Card
	cards       []models.Card
	err         error
}

func (m *mockCardSvc) CreateCard(input service.CreateCardInput) (*models.Card, error) {
	return m.createdCard, m.err
}
func (m *mockCardSvc) GetCard(id uint) (*models.Card, error) {
	return m.foundCard, m.err
}
func (m *mockCardSvc) ListByAccount(_ uint) ([]models.Card, error) {
	return m.cards, m.err
}
func (m *mockCardSvc) ListByClient(_ uint) ([]models.Card, error) {
	return m.cards, m.err
}
func (m *mockCardSvc) BlockCard(_, _ uint) (*models.Card, error) {
	return m.foundCard, m.err
}
func (m *mockCardSvc) BlockCardWithNotify(_, _ uint, _ *service.CardStatusNotifyInfo) (*models.Card, error) {
	return m.foundCard, m.err
}
func (m *mockCardSvc) UnblockCard(_ uint) (*models.Card, error) {
	return m.foundCard, m.err
}
func (m *mockCardSvc) UnblockCardWithNotify(_ uint, _ *service.CardStatusNotifyInfo) (*models.Card, error) {
	return m.foundCard, m.err
}
func (m *mockCardSvc) DeactivateCard(_ uint) (*models.Card, error) {
	return m.foundCard, m.err
}
func (m *mockCardSvc) DeactivateCardWithNotify(_ uint, _ *service.CardStatusNotifyInfo) (*models.Card, error) {
	return m.foundCard, m.err
}
func (m *mockCardSvc) RequestCardClient(_ service.ClientCardRequestInput) (*models.CardRequest, error) {
	return nil, m.err
}
func (m *mockCardSvc) VerifyCardRequest(_ uint, _ string) (*models.Card, error) {
	return m.foundCard, m.err
}

func newCardHandler(svc *mockCardSvc) http.Handler {
	return handler.NewCardHTTPHandler(svc)
}

// --- POST /api/v1/cards ---

func TestCardHandler_CreateCard_ReturnsCard(t *testing.T) {
	svc := &mockCardSvc{createdCard: &models.Card{ID: 1, VrstaKartice: "visa", Status: "aktivna"}}
	h := newCardHandler(svc)

	body, _ := json.Marshal(map[string]interface{}{
		"accountId":    1,
		"clientId":     5,
		"vrstaKartice": "visa",
		"nazivKartice": "Moja Visa",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCardHandler_CreateCard_InvalidBody_ReturnsBadRequest(t *testing.T) {
	h := newCardHandler(&mockCardSvc{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cards", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCardHandler_CreateCard_ServiceError_ReturnsBadRequest(t *testing.T) {
	svc := &mockCardSvc{err: errors.New("card limit exceeded")}
	h := newCardHandler(svc)

	body, _ := json.Marshal(map[string]interface{}{"accountId": 1, "clientId": 5, "vrstaKartice": "visa"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cards", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- GET /api/v1/cards/{id} ---

func TestCardHandler_GetCard_ReturnsCard(t *testing.T) {
	svc := &mockCardSvc{foundCard: &models.Card{ID: 7, VrstaKartice: "mastercard", Status: "aktivna"}}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cards/7", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["id"] == nil {
		t.Error("expected 'id' field in response")
	}
}

func TestCardHandler_GetCard_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newCardHandler(&mockCardSvc{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cards/not-a-number", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCardHandler_GetCard_NotFound_ReturnsNotFound(t *testing.T) {
	svc := &mockCardSvc{foundCard: nil}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cards/99", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// --- GET /api/v1/cards/account/{id} ---

func TestCardHandler_ListByAccount_ReturnsCards(t *testing.T) {
	svc := &mockCardSvc{cards: []models.Card{{ID: 1, AccountID: 10}, {ID: 2, AccountID: 10}}}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cards/account/10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp []map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Errorf("expected 2 cards, got %d", len(resp))
	}
}

func TestCardHandler_ListByAccount_InvalidID_ReturnsBadRequest(t *testing.T) {
	h := newCardHandler(&mockCardSvc{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cards/account/bad", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- GET /api/v1/cards/client/{id} ---

func TestCardHandler_ListByClient_ReturnsCards(t *testing.T) {
	svc := &mockCardSvc{cards: []models.Card{{ID: 3, ClientID: 5}}}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cards/client/5", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- PUT /api/v1/cards/{id}/block ---

func TestCardHandler_BlockCard_ReturnsBlockedCard(t *testing.T) {
	svc := &mockCardSvc{foundCard: &models.Card{ID: 1, Status: "blokirana"}}
	h := newCardHandler(svc)

	body, _ := json.Marshal(map[string]interface{}{"clientId": 5})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cards/1/block", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCardHandler_BlockCard_ServiceError_ReturnsBadRequest(t *testing.T) {
	svc := &mockCardSvc{err: errors.New("card does not belong to this client")}
	h := newCardHandler(svc)

	body, _ := json.Marshal(map[string]interface{}{"clientId": 99})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cards/1/block", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- PUT /api/v1/cards/{id}/unblock ---

func TestCardHandler_UnblockCard_ReturnsUnblockedCard(t *testing.T) {
	svc := &mockCardSvc{foundCard: &models.Card{ID: 1, Status: "aktivna"}}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/cards/1/unblock", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCardHandler_UnblockCard_ServiceError_ReturnsBadRequest(t *testing.T) {
	svc := &mockCardSvc{err: errors.New("cannot unblock card with status aktivna")}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/cards/2/unblock", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- PUT /api/v1/cards/{id}/deactivate ---

func TestCardHandler_DeactivateCard_ReturnsDeactivatedCard(t *testing.T) {
	svc := &mockCardSvc{foundCard: &models.Card{ID: 1, Status: "deaktivirana"}}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/cards/1/deactivate", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCardHandler_DeactivateCard_ServiceError_ReturnsBadRequest(t *testing.T) {
	svc := &mockCardSvc{err: errors.New("card is already deactivated")}
	h := newCardHandler(svc)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/cards/3/deactivate", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
