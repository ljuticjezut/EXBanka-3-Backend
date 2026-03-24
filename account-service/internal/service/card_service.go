package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/util"
)

// ErrCardLimitExceeded is returned when card creation would exceed the allowed limit.
var ErrCardLimitExceeded = errors.New("card limit exceeded")

// CardRepositoryInterface allows mocking in tests.
type CardRepositoryInterface interface {
	Create(card *models.Card) error
	FindByID(id uint) (*models.Card, error)
	CountByAccountID(accountID uint) (int64, error)
	CountByClientAndAccount(clientID, accountID uint) (int64, error)
	ListByAccountID(accountID uint) ([]models.Card, error)
	ListByClientID(clientID uint) ([]models.Card, error)
	Save(card *models.Card) error
}

// CreateCardInput carries the data needed to issue a new card.
type CreateCardInput struct {
	AccountID    uint
	ClientID     uint
	VrstaKartice string // visa, mastercard, dinacard, amex
	NazivKartice string
	ClientEmail  string
	ClientName   string
}

// CardService handles card creation and status management.
type CardService struct {
	cardRepo    CardRepositoryInterface
	accountRepo AccountRepositoryInterface
	notifSvc    *NotificationService
}

func NewCardService(
	cardRepo CardRepositoryInterface,
	accountRepo AccountRepositoryInterface,
	notifSvc *NotificationService,
) *CardService {
	return &CardService{cardRepo: cardRepo, accountRepo: accountRepo, notifSvc: notifSvc}
}

func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// CreateCard issues a new card for the given account, enforcing per-account limits.
//
//   - Lični račun:   max 2 cards total on the account
//   - Poslovni račun: max 1 card per client on the account
func (s *CardService) CreateCard(input CreateCardInput) (*models.Card, error) {
	if !containsStr(models.ValidCardTypes(), input.VrstaKartice) {
		return nil, fmt.Errorf("invalid vrsta kartice: %s", input.VrstaKartice)
	}

	// Look up the account to determine its kind (licni/poslovni).
	account, err := s.accountRepo.FindByID(input.AccountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}

	// Enforce card limits.
	if account.Vrsta == "poslovni" {
		count, err := s.cardRepo.CountByClientAndAccount(input.ClientID, input.AccountID)
		if err != nil {
			return nil, fmt.Errorf("failed to count cards: %w", err)
		}
		if count >= 1 {
			return nil, fmt.Errorf("%w: poslovni account allows max 1 card per person", ErrCardLimitExceeded)
		}
	} else {
		// licni (and anything else): max 2 per account
		count, err := s.cardRepo.CountByAccountID(input.AccountID)
		if err != nil {
			return nil, fmt.Errorf("failed to count cards: %w", err)
		}
		if count >= 2 {
			return nil, fmt.Errorf("%w: licni account allows max 2 cards", ErrCardLimitExceeded)
		}
	}

	now := time.Now()
	card := &models.Card{
		BrojKartice:    util.GenerateCardNumber(input.VrstaKartice),
		CVV:            util.GenerateCVV(),
		VrstaKartice:   input.VrstaKartice,
		NazivKartice:   input.NazivKartice,
		AccountID:      input.AccountID,
		ClientID:       input.ClientID,
		Status:         "aktivna",
		DatumKreiranja: now,
		DatumIsteka:    now.AddDate(5, 0, 0),
	}

	if err := s.cardRepo.Create(card); err != nil {
		return nil, fmt.Errorf("failed to create card: %w", err)
	}

	if s.notifSvc != nil && input.ClientEmail != "" {
		_ = s.notifSvc.SendCardCreatedEmail(input.ClientEmail, input.ClientName, card.BrojKartice, input.VrstaKartice)
	}

	return card, nil
}
