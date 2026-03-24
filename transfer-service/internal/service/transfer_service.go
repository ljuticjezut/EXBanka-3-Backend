package service

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/transfer-service/internal/models"
)

// AccountRepositoryInterface defined here to avoid circular imports.
type AccountRepositoryInterface interface {
	FindByID(id uint) (*models.Account, error)
	UpdateFields(id uint, fields map[string]interface{}) error
}

// TransferRepositoryInterface defined here to avoid circular imports.
type TransferRepositoryInterface interface {
	Create(transfer *models.Transfer) error
	FindByID(id uint) (*models.Transfer, error)
	Save(transfer *models.Transfer) error
	ListByAccountID(accountID uint, filter models.TransferFilter) ([]models.Transfer, int64, error)
	ListByClientID(clientID uint, filter models.TransferFilter) ([]models.Transfer, int64, error)
}

// ExchangeRateServiceInterface allows mocking in tests.
type ExchangeRateServiceInterface interface {
	GetRate(fromCurrencyKod, toCurrencyKod string) (float64, error)
}

type CreateTransferInput struct {
	RacunPosiljaocaID uint
	RacunPrimaocaID   uint
	Iznos             float64
	Svrha             string
}

type TransferService struct {
	accountRepo  AccountRepositoryInterface
	transferRepo TransferRepositoryInterface
	exchangeSvc  ExchangeRateServiceInterface
}

func NewTransferServiceWithRepos(
	accountRepo AccountRepositoryInterface,
	transferRepo TransferRepositoryInterface,
	exchangeSvc ExchangeRateServiceInterface,
) *TransferService {
	return &TransferService{
		accountRepo:  accountRepo,
		transferRepo: transferRepo,
		exchangeSvc:  exchangeSvc,
	}
}

func (s *TransferService) ListTransfersByAccount(accountID uint, filter models.TransferFilter) ([]models.Transfer, int64, error) {
	return s.transferRepo.ListByAccountID(accountID, filter)
}

func (s *TransferService) ListTransfersByClient(clientID uint, filter models.TransferFilter) ([]models.Transfer, int64, error) {
	return s.transferRepo.ListByClientID(clientID, filter)
}

func (s *TransferService) CreateTransfer(input CreateTransferInput) (*models.Transfer, error) {
	if input.Iznos <= 0 {
		return nil, fmt.Errorf("transfer amount must be positive")
	}
	if input.RacunPosiljaocaID == input.RacunPrimaocaID {
		return nil, fmt.Errorf("sender and receiver accounts must be different")
	}

	sender, err := s.accountRepo.FindByID(input.RacunPosiljaocaID)
	if err != nil {
		return nil, fmt.Errorf("sender account not found: %w", err)
	}
	receiver, err := s.accountRepo.FindByID(input.RacunPrimaocaID)
	if err != nil {
		return nil, fmt.Errorf("receiver account not found: %w", err)
	}

	if sender.RaspolozivoStanje < input.Iznos {
		return nil, fmt.Errorf("insufficient balance: available %.2f, requested %.2f",
			sender.RaspolozivoStanje, input.Iznos)
	}
	if input.Iznos > sender.DnevniLimit {
		return nil, fmt.Errorf("amount %.2f exceeds daily limit %.2f",
			input.Iznos, sender.DnevniLimit)
	}
	if sender.DnevnaPotrosnja+input.Iznos > sender.DnevniLimit {
		return nil, fmt.Errorf("daily spending limit exceeded: spent %.2f, limit %.2f, requested %.2f",
			sender.DnevnaPotrosnja, sender.DnevniLimit, input.Iznos)
	}
	if sender.MesecnaPotrosnja+input.Iznos > sender.MesecniLimit {
		return nil, fmt.Errorf("monthly spending limit exceeded: spent %.2f, limit %.2f, requested %.2f",
			sender.MesecnaPotrosnja, sender.MesecniLimit, input.Iznos)
	}

	// Calculate exchange rate and commission (for cross-currency) but do NOT update balances yet.
	kurs := 1.0
	konvertovaniIznos := input.Iznos
	valutaIznosa := sender.Currency.Kod
	provizijaProcent := 0.0
	provizija := 0.0

	if sender.CurrencyID != receiver.CurrencyID {
		var rsdAmount float64
		if sender.Currency.Kod == "RSD" {
			rsdAmount = input.Iznos
		} else {
			kursToRSD, err2 := s.exchangeSvc.GetRate(sender.Currency.Kod, "RSD")
			if err2 != nil {
				return nil, fmt.Errorf("failed to get exchange rate %s→RSD: %w", sender.Currency.Kod, err2)
			}
			rsdAmount = input.Iznos * kursToRSD
			kurs = kursToRSD
		}
		if receiver.Currency.Kod == "RSD" {
			konvertovaniIznos = math.Round(rsdAmount*100) / 100
		} else {
			kursFromRSD, err2 := s.exchangeSvc.GetRate("RSD", receiver.Currency.Kod)
			if err2 != nil {
				return nil, fmt.Errorf("failed to get exchange rate RSD→%s: %w", receiver.Currency.Kod, err2)
			}
			konvertovaniIznos = math.Round(rsdAmount*kursFromRSD*100) / 100
			if input.Iznos > 0 {
				kurs = konvertovaniIznos / input.Iznos
			}
		}
		provizijaProcent = math.Round(rand.Float64()*100) / 100
		provizija = math.Round(input.Iznos*provizijaProcent) / 100

		ukupnoZaSkidanje := input.Iznos + provizija
		if sender.RaspolozivoStanje < ukupnoZaSkidanje {
			return nil, fmt.Errorf("insufficient balance: available %.2f, required %.2f (amount %.2f + commission %.2f)",
				sender.RaspolozivoStanje, ukupnoZaSkidanje, input.Iznos, provizija)
		}
	}

	code := fmt.Sprintf("%06d", rand.Intn(1_000_000))

	transfer := &models.Transfer{
		RacunPosiljaocaID: input.RacunPosiljaocaID,
		RacunPrimaocaID:   input.RacunPrimaocaID,
		Iznos:             input.Iznos,
		ValutaIznosa:      valutaIznosa,
		KonvertovaniIznos: konvertovaniIznos,
		Kurs:              kurs,
		Provizija:         provizija,
		ProvizijaProcent:  provizijaProcent,
		Svrha:             input.Svrha,
		Status:            "u_obradi",
		VerifikacioniKod:  code,
		VremeTransakcije:  time.Now(),
	}

	if err := s.transferRepo.Create(transfer); err != nil {
		return nil, fmt.Errorf("failed to save transfer: %w", err)
	}

	return transfer, nil
}

func (s *TransferService) VerifyTransfer(transferID uint, verificationCode string) (*models.Transfer, error) {
	transfer, err := s.transferRepo.FindByID(transferID)
	if err != nil {
		return nil, fmt.Errorf("transfer not found: %w", err)
	}

	if transfer.Status != "u_obradi" {
		return nil, fmt.Errorf("transfer is not pending: status=%s", transfer.Status)
	}

	if time.Since(transfer.CreatedAt) > 5*time.Minute {
		transfer.Status = "stornirano"
		_ = s.transferRepo.Save(transfer)
		return nil, fmt.Errorf("verification code expired")
	}

	if transfer.VerifikacioniKod != verificationCode {
		transfer.BrojPokusaja++
		if transfer.BrojPokusaja >= 3 {
			transfer.Status = "stornirano"
			_ = s.transferRepo.Save(transfer)
			return nil, fmt.Errorf("maximum verification attempts exceeded, transfer cancelled")
		}
		_ = s.transferRepo.Save(transfer)
		return nil, fmt.Errorf("invalid verification code")
	}

	sender, err := s.accountRepo.FindByID(transfer.RacunPosiljaocaID)
	if err != nil {
		return nil, fmt.Errorf("sender account not found: %w", err)
	}
	receiver, err := s.accountRepo.FindByID(transfer.RacunPrimaocaID)
	if err != nil {
		return nil, fmt.Errorf("receiver account not found: %w", err)
	}

	ukupnoZaSkidanje := transfer.Iznos + transfer.Provizija

	if err := s.accountRepo.UpdateFields(sender.ID, map[string]interface{}{
		"stanje":             sender.Stanje - ukupnoZaSkidanje,
		"raspolozivo_stanje": sender.RaspolozivoStanje - ukupnoZaSkidanje,
		"dnevna_potrosnja":   sender.DnevnaPotrosnja + transfer.Iznos,
		"mesecna_potrosnja":  sender.MesecnaPotrosnja + transfer.Iznos,
	}); err != nil {
		return nil, fmt.Errorf("failed to update sender balance: %w", err)
	}

	if err := s.accountRepo.UpdateFields(receiver.ID, map[string]interface{}{
		"stanje":             receiver.Stanje + transfer.KonvertovaniIznos,
		"raspolozivo_stanje": receiver.RaspolozivoStanje + transfer.KonvertovaniIznos,
	}); err != nil {
		return nil, fmt.Errorf("failed to update receiver balance: %w", err)
	}

	transfer.Status = "uspesno"
	if err := s.transferRepo.Save(transfer); err != nil {
		return nil, fmt.Errorf("failed to save transfer: %w", err)
	}

	return transfer, nil
}
