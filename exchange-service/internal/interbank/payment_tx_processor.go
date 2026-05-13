package interbank

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

// PaymentTxProcessor is the receiver-side TxProcessor branch for the
// 2-posting MONAS ACCOUNT shape that represents a direct cross-bank
// payment (protocol §2.8 + §2.6). The initiator's bank holds the only
// credited posting locally and reserves there; this processor is what
// runs in the RECIPIENT bank — we have one debited posting (recipient
// gains funds), so NEW_TX needs only verification + a pending row, no
// reservation.
//
// State flow on this side:
//
//	NEW_TX     → validate, persist InterbankPayment(direction=inbound,
//	             status=pending), vote YES
//	COMMIT_TX  → CAS pending→committed, credit recipient (stanje +=,
//	             raspolozivo_stanje +=) — both effects in one DB tx
//	ROLLBACK_TX→ CAS pending→rolled_back, no wallet effect (we never
//	             debited)
type PaymentTxProcessor struct {
	db          *gorm.DB
	registry    *Registry
	paymentRepo *repository.InterbankPaymentRepository
	walletRepo  *repository.InterbankPaymentWalletRepository
}

// NewPaymentTxProcessor wires the receiver-side payment processor.
func NewPaymentTxProcessor(
	db *gorm.DB,
	registry *Registry,
	paymentRepo *repository.InterbankPaymentRepository,
	walletRepo *repository.InterbankPaymentWalletRepository,
) *PaymentTxProcessor {
	return &PaymentTxProcessor{
		db:          db,
		registry:    registry,
		paymentRepo: paymentRepo,
		walletRepo:  walletRepo,
	}
}

// OnNewTx implements TxProcessor.OnNewTx for the payment shape. The
// inbound-message idempotence layer (Server.replayCached) already
// dedupes by (partner, idempotenceKey); business-layer idempotence by
// transactionId is handled here so a fresh idempotence key carrying
// the same transactionId can't double-create the pending row.
func (p *PaymentTxProcessor) OnNewTx(_ context.Context, partner *PartnerBank, tx *Transaction) (*TransactionVote, error) {
	if reason := checkBalanced(tx); reason != nil {
		return voteNo(*reason), nil
	}

	parsed, reason := parsePaymentTx(tx)
	if reason != nil {
		return voteNo(*reason), nil
	}

	// We must be the recipient's bank. The recipient posting is the
	// positive-amount one — its routing must match our own.
	ownRouting := p.registry.OwnRoutingNumber()
	if parsed.recipientRouting != ownRouting {
		return voteNo(NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.recipientPosting}), nil
	}

	// Sender posting's routing must match the partner that POSTed this
	// NEW_TX. Otherwise the envelope was forwarded or spoofed.
	if parsed.senderRouting != partner.Code {
		slog.Warn("interbank: payment NEW_TX sender routing does not match partner",
			"partner", partner.Code,
			"sender_routing", parsed.senderRouting,
			"tx_routing", tx.TransactionID.RoutingNumber,
			"tx_id", tx.TransactionID.ID,
		)
		return voteNo(NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.senderPosting}), nil
	}

	// Idempotent replay past the inbound layer: if we already created
	// the pending row, the vote stays YES.
	existing, err := p.paymentRepo.GetByTxID(
		int(tx.TransactionID.RoutingNumber),
		tx.TransactionID.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("looking up pending payment: %w", err)
	}
	if existing != nil {
		return &TransactionVote{Vote: VoteYes}, nil
	}

	// First time we've seen this TransactionID. Verify the recipient
	// account exists, is active, and matches the posting's currency,
	// then persist the inbound pending row — all under one DB tx so
	// the lookup + persist are atomic with respect to any concurrent
	// COMMIT_TX of the same transactionId (impossible per protocol but
	// cheap to guard against).
	var (
		accountID         uint
		voteNoReason      *NoVoteReason
		businessErrMsg    string
	)
	txErr := p.db.Transaction(func(dbtx *gorm.DB) error {
		snap, lockErr := p.walletRepo.LockByNumber(dbtx, parsed.recipientAccount, parsed.currency)
		switch {
		case errors.Is(lockErr, repository.ErrInterbankPaymentNoSuchAccount):
			voteNoReason = &NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.recipientPosting}
			businessErrMsg = "recipient account not found"
			return errPaymentVoteNo
		case errors.Is(lockErr, repository.ErrInterbankPaymentAccountInactive):
			voteNoReason = &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.recipientPosting}
			businessErrMsg = "recipient account is not active"
			return errPaymentVoteNo
		case errors.Is(lockErr, repository.ErrInterbankPaymentCurrencyMismatch):
			voteNoReason = &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.recipientPosting}
			businessErrMsg = "recipient account currency does not match posting"
			return errPaymentVoteNo
		case lockErr != nil:
			return lockErr
		}
		accountID = snap.ID

		row := &models.InterbankPayment{
			TxRoutingNumber:        int(tx.TransactionID.RoutingNumber),
			TxID:                   tx.TransactionID.ID,
			Direction:              models.InterbankPaymentDirectionInbound,
			PartnerRoutingNumber:   int(partner.Code),
			SenderAccountNumber:    parsed.senderAccount,
			RecipientAccountNumber: parsed.recipientAccount,
			Currency:               parsed.currency,
			Amount:                 parsed.amount,
			LocalAccountID:         &accountID,
			Message:                tx.Message,
			PaymentCode:            tx.PaymentCode,
			PaymentPurpose:         tx.PaymentPurpose,
			Status:                 models.InterbankPaymentStatusPending,
			CreatedAt:              time.Now().UTC(),
		}
		return p.paymentRepo.CreateTx(dbtx, row)
	})
	if errors.Is(txErr, errPaymentVoteNo) {
		slog.Info("interbank: payment NEW_TX vote NO",
			"tx_routing", tx.TransactionID.RoutingNumber,
			"tx_id", tx.TransactionID.ID,
			"reason", voteNoReason.Reason,
			"msg", businessErrMsg,
		)
		return voteNo(*voteNoReason), nil
	}
	if txErr != nil {
		return nil, fmt.Errorf("recording inbound payment: %w", txErr)
	}

	slog.Info("interbank: payment NEW_TX accepted",
		"tx_routing", tx.TransactionID.RoutingNumber,
		"tx_id", tx.TransactionID.ID,
		"partner", partner.Code,
		"recipient_account", parsed.recipientAccount,
		"currency", parsed.currency,
		"amount", parsed.amount,
	)
	return &TransactionVote{Vote: VoteYes}, nil
}

// OnCommitTx applies the credit + status flip atomically. Idempotent
// on already-committed rows; errors on unknown or rolled-back rows so
// the partner sees a 5xx and can investigate.
func (p *PaymentTxProcessor) OnCommitTx(_ context.Context, _ *PartnerBank, txID ForeignBankId) error {
	pending, err := p.paymentRepo.GetByTxID(int(txID.RoutingNumber), txID.ID)
	if err != nil {
		return fmt.Errorf("loading inbound payment: %w", err)
	}
	if pending == nil {
		return fmt.Errorf("COMMIT_TX for unknown payment %d/%s", txID.RoutingNumber, txID.ID)
	}
	if pending.Direction != models.InterbankPaymentDirectionInbound {
		return fmt.Errorf("COMMIT_TX dispatched to payment processor for non-inbound row %d/%s", txID.RoutingNumber, txID.ID)
	}

	switch pending.Status {
	case models.InterbankPaymentStatusCommitted:
		return nil
	case models.InterbankPaymentStatusRolledBack, models.InterbankPaymentStatusRejected, models.InterbankPaymentStatusFailed:
		return fmt.Errorf("COMMIT_TX for already-resolved payment %d/%s (status=%s)",
			txID.RoutingNumber, txID.ID, pending.Status)
	case models.InterbankPaymentStatusPending:
		// fall through to apply the commit
	default:
		return fmt.Errorf("inbound payment %d/%s has unknown status %q",
			txID.RoutingNumber, txID.ID, pending.Status)
	}

	if pending.LocalAccountID == nil {
		return fmt.Errorf("inbound payment %d/%s missing local_account_id", txID.RoutingNumber, txID.ID)
	}

	txErr := p.db.Transaction(func(dbtx *gorm.DB) error {
		rows, err := p.paymentRepo.MarkCommittedCAS(dbtx, int(txID.RoutingNumber), txID.ID)
		if err != nil {
			return err
		}
		if rows == 0 {
			return errPaymentAlreadyResolved
		}
		return p.walletRepo.Credit(dbtx, *pending.LocalAccountID, pending.Amount)
	})
	if errors.Is(txErr, errPaymentAlreadyResolved) {
		return nil
	}
	if txErr != nil {
		return txErr
	}

	slog.Info("interbank: payment COMMIT_TX applied",
		"tx_routing", txID.RoutingNumber,
		"tx_id", txID.ID,
		"recipient_account", pending.RecipientAccountNumber,
		"currency", pending.Currency,
		"amount", pending.Amount,
	)
	return nil
}

// OnRollbackTx flips the pending row to rolled_back. No wallet effect:
// we never reserved or debited the recipient. Idempotent.
func (p *PaymentTxProcessor) OnRollbackTx(_ context.Context, _ *PartnerBank, txID ForeignBankId) error {
	pending, err := p.paymentRepo.GetByTxID(int(txID.RoutingNumber), txID.ID)
	if err != nil {
		return fmt.Errorf("loading inbound payment: %w", err)
	}
	if pending == nil {
		// We never voted YES — nothing to undo. The protocol allows
		// the initiator to send ROLLBACK_TX to participants that
		// voted NO; treat that as success.
		return nil
	}
	if pending.Direction != models.InterbankPaymentDirectionInbound {
		return fmt.Errorf("ROLLBACK_TX dispatched to payment processor for non-inbound row %d/%s", txID.RoutingNumber, txID.ID)
	}

	switch pending.Status {
	case models.InterbankPaymentStatusRolledBack:
		return nil
	case models.InterbankPaymentStatusCommitted:
		return fmt.Errorf("ROLLBACK_TX for already-committed payment %d/%s", txID.RoutingNumber, txID.ID)
	case models.InterbankPaymentStatusPending:
		// proceed
	default:
		return fmt.Errorf("inbound payment %d/%s has unknown status %q",
			txID.RoutingNumber, txID.ID, pending.Status)
	}

	txErr := p.db.Transaction(func(dbtx *gorm.DB) error {
		rows, err := p.paymentRepo.MarkRolledBackCAS(dbtx, int(txID.RoutingNumber), txID.ID)
		if err != nil {
			return err
		}
		if rows == 0 {
			return errPaymentAlreadyResolved
		}
		return nil
	})
	if errors.Is(txErr, errPaymentAlreadyResolved) {
		return nil
	}
	if txErr != nil {
		return txErr
	}

	slog.Info("interbank: payment ROLLBACK_TX applied",
		"tx_routing", txID.RoutingNumber,
		"tx_id", txID.ID,
	)
	return nil
}

// errPaymentVoteNo is an internal sentinel used inside OnNewTx's DB
// transaction to roll back the open tx and propagate the precomputed
// NoVoteReason out to the caller.
var errPaymentVoteNo = errors.New("interbank: payment NEW_TX must vote NO")

// errPaymentAlreadyResolved is the per-flow analogue of
// errAlreadyResolved in the OTC processor — surfaced when the CAS
// update finds the pending row already flipped by a concurrent caller.
var errPaymentAlreadyResolved = errors.New("interbank: payment already resolved by concurrent caller")

// paymentTx is the parsed view of a 2-posting MONAS ACCOUNT payment.
type paymentTx struct {
	senderPosting    *Posting
	recipientPosting *Posting

	senderAccount    string
	recipientAccount string
	senderRouting    RoutingNumber
	recipientRouting RoutingNumber

	currency string
	amount   float64
}

// parsePaymentTx classifies a 2-posting MONAS-only ACCOUNT-only NEW_TX
// as a payment. Any other shape returns nil so the dispatcher can try
// the next processor (currently: only OTC and payment exist).
func parsePaymentTx(tx *Transaction) (*paymentTx, *NoVoteReason) {
	if len(tx.Postings) != 2 {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset}
	}

	parsed := &paymentTx{}
	for i := range tx.Postings {
		ptg := &tx.Postings[i]
		if ptg.Account.Type != TxAccountAccount {
			return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
		}
		if strings.TrimSpace(ptg.Account.Num) == "" {
			return nil, &NoVoteReason{Reason: ReasonNoSuchAccount, Posting: ptg}
		}
		if ptg.Asset.Type != AssetMonas || ptg.Asset.Monas == nil {
			return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
		}
		if ptg.Amount < 0 {
			if parsed.senderPosting != nil {
				return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
			}
			parsed.senderPosting = ptg
			parsed.senderAccount = ptg.Account.Num
			parsed.amount = -ptg.Amount
			parsed.currency = string(ptg.Asset.Monas.Currency)
		} else {
			if parsed.recipientPosting != nil {
				return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
			}
			parsed.recipientPosting = ptg
			parsed.recipientAccount = ptg.Account.Num
		}
	}
	if parsed.senderPosting == nil || parsed.recipientPosting == nil {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset}
	}
	if !nearlyEqual(parsed.senderPosting.Amount, -parsed.recipientPosting.Amount) {
		return nil, &NoVoteReason{Reason: ReasonUnbalancedTx}
	}
	if parsed.senderPosting.Asset.Monas.Currency != parsed.recipientPosting.Asset.Monas.Currency {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.recipientPosting}
	}
	if parsed.amount <= 0 {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.senderPosting}
	}

	sendRouting, err := RoutingNumberFromAccount(parsed.senderAccount)
	if err != nil {
		return nil, &NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.senderPosting}
	}
	recvRouting, err := RoutingNumberFromAccount(parsed.recipientAccount)
	if err != nil {
		return nil, &NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.recipientPosting}
	}
	parsed.senderRouting = sendRouting
	parsed.recipientRouting = recvRouting

	return parsed, nil
}

// BuildPaymentTx assembles the 2-posting MONAS ACCOUNT Transaction that
// represents a direct cross-bank payment on the wire. Sender posting is
// credited (-amount, account loses funds), recipient is debited
// (+amount, account gains funds), both in the same currency. Used by
// both the sender-side HTTP handler and the reconciliation cron when
// retrying a stuck NEW_TX.
func BuildPaymentTx(txID ForeignBankId, senderAcc, recipientAcc, currency string, amount float64, message, paymentCode, paymentPurpose string) Transaction {
	monas := Asset{
		Type:  AssetMonas,
		Monas: &MonetaryAsset{Currency: CurrencyCode(currency)},
	}
	return Transaction{
		TransactionID:  txID,
		Message:        message,
		PaymentCode:    paymentCode,
		PaymentPurpose: paymentPurpose,
		Postings: []Posting{
			{
				Account: TxAccount{Type: TxAccountAccount, Num: senderAcc},
				Amount:  -amount,
				Asset:   monas,
			},
			{
				Account: TxAccount{Type: TxAccountAccount, Num: recipientAcc},
				Amount:  amount,
				Asset:   monas,
			},
		},
	}
}

// IsPaymentShape returns true when the transaction looks like a direct
// cross-bank payment (2 MONAS ACCOUNT postings) rather than an OTC
// option acceptance. Used by the dispatching TxProcessor to route
// NEW_TX bodies to the right processor.
func IsPaymentShape(tx *Transaction) bool {
	if len(tx.Postings) != 2 {
		return false
	}
	for i := range tx.Postings {
		ptg := &tx.Postings[i]
		if ptg.Account.Type != TxAccountAccount {
			return false
		}
		if ptg.Asset.Type != AssetMonas {
			return false
		}
	}
	return true
}
