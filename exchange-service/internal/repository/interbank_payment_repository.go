package repository

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
)

// ErrInterbankPaymentNoSuchAccount is returned by the payment wallet
// lookup when the account number does not exist, is not active, or
// does not match the requested currency. The TxProcessor maps each
// distinct cause to a different NoVoteReason, so the caller checks
// against the more specific sentinels below before falling back to
// this.
var (
	ErrInterbankPaymentNoSuchAccount      = errors.New("interbank payment wallet: no such account")
	ErrInterbankPaymentAccountInactive    = errors.New("interbank payment wallet: account is not active")
	ErrInterbankPaymentCurrencyMismatch   = errors.New("interbank payment wallet: account currency does not match posting")
	ErrInterbankPaymentInsufficientFunds  = errors.New("interbank payment wallet: insufficient available balance")
)

// InterbankPaymentRepository persists and looks up InterbankPayment rows
// keyed by the protocol's globally-unique transactionId. Methods that
// must run inside a caller-supplied DB transaction take a *gorm.DB so
// the row write lands together with the wallet effect.
type InterbankPaymentRepository struct {
	db *gorm.DB
}

func NewInterbankPaymentRepository(db *gorm.DB) *InterbankPaymentRepository {
	return &InterbankPaymentRepository{db: db}
}

// DB exposes the underlying *gorm.DB so callers can open their own
// transactions when bundling repo writes with wallet effects.
func (r *InterbankPaymentRepository) DB() *gorm.DB { return r.db }

// GetByTxID returns the row keyed by the protocol transactionId
// (routingNumber, id), or nil if none exists.
func (r *InterbankPaymentRepository) GetByTxID(routing int, id string) (*models.InterbankPayment, error) {
	var row models.InterbankPayment
	err := r.db.Where("tx_routing_number = ? AND tx_id = ?", routing, id).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("loading payment %d/%s: %w", routing, id, err)
	}
	return &row, nil
}

// GetByID returns the row keyed by its local autoincrement ID, used by
// the local frontend to poll a payment it just initiated.
func (r *InterbankPaymentRepository) GetByID(id uint) (*models.InterbankPayment, error) {
	var row models.InterbankPayment
	err := r.db.First(&row, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("loading payment id %d: %w", id, err)
	}
	return &row, nil
}

// CreateTx inserts a new payment row under the caller's DB transaction.
// Used by both the sender-side initiator (after reserving funds) and
// the receiver-side TxProcessor (after voting YES).
func (r *InterbankPaymentRepository) CreateTx(tx *gorm.DB, row *models.InterbankPayment) error {
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	if row.Status == "" {
		row.Status = models.InterbankPaymentStatusPending
	}
	return tx.Create(row).Error
}

// ListOutboundForClient returns the most recent outbound payments owned
// by the given client. Newest first. Used by the frontend "my cross-bank
// payments" view.
func (r *InterbankPaymentRepository) ListOutboundForClient(clientID uint, limit int) ([]models.InterbankPayment, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []models.InterbankPayment
	err := r.db.
		Where("direction = ? AND local_client_id = ?", models.InterbankPaymentDirectionOutbound, clientID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing outbound payments: %w", err)
	}
	return rows, nil
}

// MarkCommittedCAS flips a pending row to committed atomically. Returns
// rowsAffected so the caller can distinguish "we did the work" (1) from
// "somebody else already resolved this" (0).
func (r *InterbankPaymentRepository) MarkCommittedCAS(tx *gorm.DB, routing int, id string) (int64, error) {
	now := time.Now().UTC()
	res := tx.Model(&models.InterbankPayment{}).
		Where("tx_routing_number = ? AND tx_id = ? AND status = ?",
			routing, id, models.InterbankPaymentStatusPending).
		Updates(map[string]interface{}{
			"status":      models.InterbankPaymentStatusCommitted,
			"resolved_at": now,
			"updated_at":  now,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("marking payment committed: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// MarkRolledBackCAS flips a pending row to rolled_back atomically.
func (r *InterbankPaymentRepository) MarkRolledBackCAS(tx *gorm.DB, routing int, id string) (int64, error) {
	now := time.Now().UTC()
	res := tx.Model(&models.InterbankPayment{}).
		Where("tx_routing_number = ? AND tx_id = ? AND status = ?",
			routing, id, models.InterbankPaymentStatusPending).
		Updates(map[string]interface{}{
			"status":      models.InterbankPaymentStatusRolledBack,
			"resolved_at": now,
			"updated_at":  now,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("marking payment rolled back: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// MarkRejectedCAS flips a pending row to rejected (partner voted NO) and
// records the reason text. Sender-side only.
func (r *InterbankPaymentRepository) MarkRejectedCAS(tx *gorm.DB, routing int, id string, reason string) (int64, error) {
	now := time.Now().UTC()
	res := tx.Model(&models.InterbankPayment{}).
		Where("tx_routing_number = ? AND tx_id = ? AND status = ?",
			routing, id, models.InterbankPaymentStatusPending).
		Updates(map[string]interface{}{
			"status":      models.InterbankPaymentStatusRejected,
			"last_error":  reason,
			"resolved_at": now,
			"updated_at":  now,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("marking payment rejected: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// MarkPartnerFinalised stamps PartnerFinalisedAt = now on a resolved
// row so the reconciliation cron stops retrying the terminal message.
// No-op (RowsAffected == 0) on rows that already have it set; callers
// don't need to distinguish.
func (r *InterbankPaymentRepository) MarkPartnerFinalised(tx *gorm.DB, routing int, id string) (int64, error) {
	now := time.Now().UTC()
	res := tx.Model(&models.InterbankPayment{}).
		Where("tx_routing_number = ? AND tx_id = ? AND partner_finalised_at IS NULL", routing, id).
		Updates(map[string]interface{}{
			"partner_finalised_at": now,
			"updated_at":           now,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("marking payment partner-finalised: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ListStuckPending returns outbound payments still in `pending` whose
// updated_at is older than threshold. Used by the reconciliation cron
// to retry NEW_TX. Limit caps the batch so a backlog can't starve the
// scheduler.
func (r *InterbankPaymentRepository) ListStuckPending(threshold time.Time, limit int) ([]models.InterbankPayment, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []models.InterbankPayment
	err := r.db.
		Where("direction = ? AND status = ? AND updated_at < ?",
			models.InterbankPaymentDirectionOutbound,
			models.InterbankPaymentStatusPending,
			threshold,
		).
		Order("updated_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing stuck pending payments: %w", err)
	}
	return rows, nil
}

// ListUndispatchedTerminal returns outbound payments whose status is
// resolved (committed / failed) but whose terminal partner message has
// not yet been acknowledged. The cron replays COMMIT_TX or ROLLBACK_TX
// for these. `rejected` is excluded — the partner already voted NO and
// holds no resources, so no terminal message is owed.
func (r *InterbankPaymentRepository) ListUndispatchedTerminal(threshold time.Time, limit int) ([]models.InterbankPayment, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []models.InterbankPayment
	err := r.db.
		Where("direction = ? AND status IN ? AND partner_finalised_at IS NULL AND updated_at < ?",
			models.InterbankPaymentDirectionOutbound,
			[]string{models.InterbankPaymentStatusCommitted, models.InterbankPaymentStatusFailed},
			threshold,
		).
		Order("updated_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing undispatched terminal payments: %w", err)
	}
	return rows, nil
}

// MarkFailedCAS flips a pending row to failed (transport error or
// timeout). Sender-side only.
func (r *InterbankPaymentRepository) MarkFailedCAS(tx *gorm.DB, routing int, id string, errMsg string) (int64, error) {
	now := time.Now().UTC()
	res := tx.Model(&models.InterbankPayment{}).
		Where("tx_routing_number = ? AND tx_id = ? AND status = ?",
			routing, id, models.InterbankPaymentStatusPending).
		Updates(map[string]interface{}{
			"status":      models.InterbankPaymentStatusFailed,
			"last_error":  errMsg,
			"resolved_at": now,
			"updated_at":  now,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("marking payment failed: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// InterbankPaymentWalletRepository wraps the local accounts table for
// the cross-bank payment flow. The protocol's payment shape uses
// TxAccount type ACCOUNT (currency account number), so all lookups
// here go by broj_racuna — distinct from InterbankWalletRepository
// which uses "first active account in currency" for the OTC flow.
type InterbankPaymentWalletRepository struct {
	db *gorm.DB
}

func NewInterbankPaymentWalletRepository(db *gorm.DB) *InterbankPaymentWalletRepository {
	return &InterbankPaymentWalletRepository{db: db}
}

// AccountSnapshot is the subset of the accounts row this flow needs.
type AccountSnapshot struct {
	ID                uint    `gorm:"column:id"`
	BrojRacuna        string  `gorm:"column:broj_racuna"`
	ClientID          *uint   `gorm:"column:client_id"`
	CurrencyCode      string  `gorm:"column:currency_code"`
	Status            string  `gorm:"column:status"`
	Stanje            float64 `gorm:"column:stanje"`
	RaspolozivoStanje float64 `gorm:"column:raspolozivo_stanje"`
}

// LockByNumber finds the account by broj_racuna, locks the row FOR
// UPDATE under the caller's tx, and validates status + currency.
// Returns the typed sentinels above so the caller can map each cause
// to the right NoVoteReason / HTTP error.
func (r *InterbankPaymentWalletRepository) LockByNumber(tx *gorm.DB, brojRacuna, currency string) (*AccountSnapshot, error) {
	var row AccountSnapshot
	err := tx.Table("accounts").
		Select("accounts.id, accounts.broj_racuna, accounts.client_id, currencies.kod AS currency_code, accounts.status, accounts.stanje, accounts.raspolozivo_stanje").
		Joins("JOIN currencies ON currencies.id = accounts.currency_id").
		Where("accounts.broj_racuna = ?", brojRacuna).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInterbankPaymentNoSuchAccount
		}
		return nil, fmt.Errorf("locking account %s: %w", brojRacuna, err)
	}
	if row.Status != "aktivan" {
		return &row, ErrInterbankPaymentAccountInactive
	}
	if row.CurrencyCode != currency {
		return &row, ErrInterbankPaymentCurrencyMismatch
	}
	return &row, nil
}

// Reserve decrements raspolozivo_stanje by amount on the locked sender
// account. Caller MUST have run LockByNumber inside the same tx first.
// Returns ErrInterbankPaymentInsufficientFunds when the available
// balance is below amount.
func (r *InterbankPaymentWalletRepository) Reserve(tx *gorm.DB, accountID uint, amount float64) error {
	res := tx.Table("accounts").
		Where("id = ? AND raspolozivo_stanje >= ?", accountID, amount).
		Updates(map[string]interface{}{
			"raspolozivo_stanje": gorm.Expr("raspolozivo_stanje - ?", amount),
		})
	if res.Error != nil {
		return fmt.Errorf("reserving sender funds: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrInterbankPaymentInsufficientFunds
	}
	return nil
}

// Debit completes the reservation: stanje -= amount. raspolozivo_stanje
// was already decremented by Reserve, so we don't touch it again.
func (r *InterbankPaymentWalletRepository) Debit(tx *gorm.DB, accountID uint, amount float64) error {
	res := tx.Table("accounts").
		Where("id = ?", accountID).
		Updates(map[string]interface{}{
			"stanje": gorm.Expr("stanje - ?", amount),
		})
	if res.Error != nil {
		return fmt.Errorf("debiting sender funds: %w", res.Error)
	}
	return nil
}

// Release refunds a Reserve by bumping raspolozivo_stanje back up.
// stanje was never touched on a reserved-but-not-committed payment.
func (r *InterbankPaymentWalletRepository) Release(tx *gorm.DB, accountID uint, amount float64) error {
	res := tx.Table("accounts").
		Where("id = ?", accountID).
		Updates(map[string]interface{}{
			"raspolozivo_stanje": gorm.Expr("raspolozivo_stanje + ?", amount),
		})
	if res.Error != nil {
		return fmt.Errorf("releasing sender reservation: %w", res.Error)
	}
	return nil
}

// Credit applies the recipient-side commit: both stanje and
// raspolozivo_stanje go up so the recipient can both see and spend the
// funds immediately.
func (r *InterbankPaymentWalletRepository) Credit(tx *gorm.DB, accountID uint, amount float64) error {
	res := tx.Table("accounts").
		Where("id = ?", accountID).
		Updates(map[string]interface{}{
			"stanje":             gorm.Expr("stanje + ?", amount),
			"raspolozivo_stanje": gorm.Expr("raspolozivo_stanje + ?", amount),
		})
	if res.Error != nil {
		return fmt.Errorf("crediting recipient funds: %w", res.Error)
	}
	return nil
}
