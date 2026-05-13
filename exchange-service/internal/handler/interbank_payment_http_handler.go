package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/config"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/interbank"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

// InterbankPaymentHTTPHandler exposes the sender-side cross-bank
// payment flow to our own JWT-authenticated frontend. It owns the
// "user clicks Send → 2PC over /interbank → status visible to user"
// path; the recipient-side ledger effects live in
// interbank.PaymentTxProcessor.
type InterbankPaymentHTTPHandler struct {
	cfg         *config.Config
	registry    *interbank.Registry
	client      *interbank.Client
	paymentRepo *repository.InterbankPaymentRepository
	walletRepo  *repository.InterbankPaymentWalletRepository
	db          *gorm.DB
}

func NewInterbankPaymentHTTPHandler(
	cfg *config.Config,
	registry *interbank.Registry,
	client *interbank.Client,
	paymentRepo *repository.InterbankPaymentRepository,
	walletRepo *repository.InterbankPaymentWalletRepository,
	db *gorm.DB,
) *InterbankPaymentHTTPHandler {
	return &InterbankPaymentHTTPHandler{
		cfg:         cfg,
		registry:    registry,
		client:      client,
		paymentRepo: paymentRepo,
		walletRepo:  walletRepo,
		db:          db,
	}
}

// Routes dispatches /api/v1/payments/cross-bank/* paths. Three endpoints:
//
//	POST /api/v1/payments/cross-bank         — initiate a new payment
//	GET  /api/v1/payments/cross-bank         — list caller's outbound payments
//	GET  /api/v1/payments/cross-bank/{id}    — fetch one payment's status (FE polling)
func (h *InterbankPaymentHTTPHandler) Routes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/payments/cross-bank"), "/")
	if path == "" {
		switch r.Method {
		case http.MethodPost:
			h.createPayment(w, r)
		case http.MethodGet:
			h.listPayments(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "payment id must be numeric"})
			return
		}
		h.getPayment(w, r, uint(id))
		return
	}
	http.NotFound(w, r)
}

// createPayment is the sender-side initiator. The flow is:
//
//  1. Authenticate as a client.
//  2. Validate body + resolve recipient routing. Must point at a registered
//     partner bank; local recipients are rejected (use the local transfer
//     flow instead).
//  3. In a single DB tx: lock the sender's account by broj_racuna, verify
//     ownership + status + currency + available balance, reserve the funds
//     by decrementing raspolozivo_stanje, and persist an InterbankPayment
//     row in pending state.
//  4. Outside the tx: POST NEW_TX to the partner.
//  5. Apply the partner's response under a fresh DB tx:
//     YES → debit stanje, mark committed, then send COMMIT_TX.
//     NO  → release raspolozivo, mark rejected (with the partner's
//     reasons). No ROLLBACK_TX needed — the partner never reserved.
//     err → release raspolozivo, mark failed. Best-effort ROLLBACK_TX so
//     the partner can drop any half-applied state.
func (h *InterbankPaymentHTTPHandler) createPayment(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if claims.TokenSource != "client" || claims.ClientID == 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can initiate cross-bank payments"})
		return
	}

	var body struct {
		SenderAccountNumber    string  `json:"senderAccountNumber"`
		RecipientAccountNumber string  `json:"recipientAccountNumber"`
		Currency               string  `json:"currency"`
		Amount                 float64 `json:"amount"`
		Message                string  `json:"message"`
		PaymentCode            string  `json:"paymentCode"`
		PaymentPurpose         string  `json:"paymentPurpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})
		return
	}

	body.SenderAccountNumber = strings.TrimSpace(body.SenderAccountNumber)
	body.RecipientAccountNumber = strings.TrimSpace(body.RecipientAccountNumber)
	body.Currency = strings.TrimSpace(strings.ToUpper(body.Currency))

	if body.SenderAccountNumber == "" || body.RecipientAccountNumber == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "senderAccountNumber and recipientAccountNumber are required"})
		return
	}
	if body.Currency == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "currency is required"})
		return
	}
	if !interbank.IsKnownCurrency(interbank.CurrencyCode(body.Currency)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": fmt.Sprintf("unknown currency %q", body.Currency)})
		return
	}
	if body.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "amount must be positive"})
		return
	}
	if body.SenderAccountNumber == body.RecipientAccountNumber {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "sender and recipient accounts must differ"})
		return
	}

	ownRouting := h.registry.OwnRoutingNumber()
	senderRouting, _, isLocalSender, err := h.registry.ResolveBankFromAccount(body.SenderAccountNumber)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": fmt.Sprintf("resolving sender account routing: %v", err)})
		return
	}
	if !isLocalSender {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": fmt.Sprintf("sender account routing %d is not this bank — initiate the payment from the partner bank's frontend instead", senderRouting)})
		return
	}

	recipientRouting, _, isLocalRecipient, err := h.registry.ResolveBankFromAccount(body.RecipientAccountNumber)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": fmt.Sprintf("resolving recipient account routing: %v", err)})
		return
	}
	if isLocalRecipient {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "recipient is on this bank — use the local transfer flow instead"})
		return
	}
	if h.registry.Lookup(recipientRouting) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": fmt.Sprintf("no partner bank registered for routing number %d", recipientRouting)})
		return
	}

	// Phase 1 — local prepare: lock sender, validate ownership, reserve
	// funds, persist pending row. Single DB tx so the reservation and
	// the audit row land together.
	clientID := claims.ClientID
	txID := interbank.ForeignBankId{
		RoutingNumber: ownRouting,
		ID:            uuid.NewString(),
	}
	var paymentRow *models.InterbankPayment
	prepareErr := h.db.Transaction(func(dbtx *gorm.DB) error {
		snap, lockErr := h.walletRepo.LockByNumber(dbtx, body.SenderAccountNumber, body.Currency)
		switch {
		case errors.Is(lockErr, repository.ErrInterbankPaymentNoSuchAccount):
			return preparePaymentErr{status: http.StatusNotFound, msg: "sender account not found"}
		case errors.Is(lockErr, repository.ErrInterbankPaymentAccountInactive):
			return preparePaymentErr{status: http.StatusConflict, msg: "sender account is not active"}
		case errors.Is(lockErr, repository.ErrInterbankPaymentCurrencyMismatch):
			return preparePaymentErr{status: http.StatusBadRequest, msg: "currency does not match sender account's currency"}
		case lockErr != nil:
			return lockErr
		}
		if snap.ClientID == nil || *snap.ClientID != clientID {
			return preparePaymentErr{status: http.StatusForbidden, msg: "you do not own that sender account"}
		}
		if err := h.walletRepo.Reserve(dbtx, snap.ID, body.Amount); err != nil {
			if errors.Is(err, repository.ErrInterbankPaymentInsufficientFunds) {
				return preparePaymentErr{status: http.StatusUnprocessableEntity, msg: "insufficient available balance on sender account"}
			}
			return err
		}
		accountID := snap.ID
		clID := clientID
		row := &models.InterbankPayment{
			TxRoutingNumber:        int(txID.RoutingNumber),
			TxID:                   txID.ID,
			Direction:              models.InterbankPaymentDirectionOutbound,
			PartnerRoutingNumber:   int(recipientRouting),
			SenderAccountNumber:    body.SenderAccountNumber,
			RecipientAccountNumber: body.RecipientAccountNumber,
			Currency:               body.Currency,
			Amount:                 body.Amount,
			LocalAccountID:         &accountID,
			LocalClientID:          &clID,
			Message:                body.Message,
			PaymentCode:            body.PaymentCode,
			PaymentPurpose:         body.PaymentPurpose,
			Status:                 models.InterbankPaymentStatusPending,
			CreatedAt:              time.Now().UTC(),
		}
		if err := h.paymentRepo.CreateTx(dbtx, row); err != nil {
			return err
		}
		paymentRow = row
		return nil
	})
	if prepareErr != nil {
		var pe preparePaymentErr
		if errors.As(prepareErr, &pe) {
			writeJSON(w, pe.status, map[string]string{"message": pe.msg})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("preparing payment: %v", prepareErr)})
		return
	}

	// Phase 2 — send NEW_TX to the partner. This is the only network
	// hop in the happy path; the partner's 202-poll loop is handled
	// inside interbank.Client.SendNewTx.
	tx := interbank.BuildPaymentTx(txID, body.SenderAccountNumber, body.RecipientAccountNumber,
		body.Currency, body.Amount, body.Message, body.PaymentCode, body.PaymentPurpose)
	idemKey := h.client.NewIdempotenceKey()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	vote, dispatchErr := h.client.SendNewTx(ctx, recipientRouting, idemKey, &tx)
	if dispatchErr != nil {
		h.finaliseTransportFailure(paymentRow, recipientRouting, idemKey, dispatchErr)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"message":   fmt.Sprintf("dispatching NEW_TX to partner failed: %v", dispatchErr),
			"paymentId": paymentRow.ID,
			"status":    models.InterbankPaymentStatusFailed,
		})
		return
	}
	if vote.Vote != interbank.VoteYes {
		reason := summariseNoVote(vote)
		if err := h.finaliseRejected(paymentRow, reason); err != nil {
			slog.Error("interbank: payment rejection finalise failed",
				"err", err, "payment_id", paymentRow.ID)
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"message":   "partner refused the payment",
			"paymentId": paymentRow.ID,
			"status":    models.InterbankPaymentStatusRejected,
			"vote":      vote,
		})
		return
	}

	// Partner voted YES — apply local commit and notify partner.
	if err := h.finaliseCommit(paymentRow); err != nil {
		// Local commit failed AFTER the partner voted YES — operator
		// action required. We do not roll back; the partner is
		// holding nothing on its side until COMMIT_TX arrives, but
		// our books haven't been debited either. The retry loop
		// (later milestone — BANKA-BE-8) is where reconciliation
		// belongs.
		slog.Error("interbank: payment local commit failed after YES vote",
			"err", err, "payment_id", paymentRow.ID, "tx_id", txID.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"message":   fmt.Sprintf("partner voted YES but local commit failed: %v", err),
			"paymentId": paymentRow.ID,
			"status":    paymentRow.Status,
		})
		return
	}

	commitKey := h.client.NewIdempotenceKey()
	if err := h.client.SendCommitTx(ctx, recipientRouting, commitKey, txID); err != nil {
		// COMMIT_TX failed after we already debited locally. The
		// reconciliation cron will resend it on the next tick; the
		// payment row stays committed locally. Surface as 202-style
		// success-with-warning so the frontend can show "in progress".
		slog.Warn("interbank: payment COMMIT_TX dispatch failed after local commit",
			"err", err, "payment_id", paymentRow.ID, "tx_id", txID.ID)
		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"message":   fmt.Sprintf("local commit succeeded but COMMIT_TX dispatch failed; partner will reconcile: %v", err),
			"paymentId": paymentRow.ID,
			"status":    models.InterbankPaymentStatusCommitted,
		})
		return
	}
	h.markCommitDispatched(paymentRow)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"paymentId": paymentRow.ID,
		"status":    models.InterbankPaymentStatusCommitted,
		"payment":   paymentRowToResponse(paymentRow),
	})
}

// listPayments returns the caller's recent outbound cross-bank payments,
// newest first. Inbound payments live on the partner's customer rows,
// not ours.
func (h *InterbankPaymentHTTPHandler) listPayments(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if claims.TokenSource != "client" || claims.ClientID == 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can list cross-bank payments"})
		return
	}

	limit := 50
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			limit = v
		}
	}

	rows, err := h.paymentRepo.ListOutboundForClient(claims.ClientID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("listing payments: %v", err)})
		return
	}

	items := make([]map[string]interface{}, 0, len(rows))
	for i := range rows {
		items = append(items, paymentRowToResponse(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"payments": items,
		"count":    len(items),
	})
}

// getPayment is the polling endpoint for the FE. Returns the current
// state of a payment owned by the caller.
func (h *InterbankPaymentHTTPHandler) getPayment(w http.ResponseWriter, r *http.Request, id uint) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if claims.TokenSource != "client" || claims.ClientID == 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can read cross-bank payments"})
		return
	}

	row, err := h.paymentRepo.GetByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("loading payment: %v", err)})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such payment"})
		return
	}
	if row.Direction != models.InterbankPaymentDirectionOutbound || row.LocalClientID == nil || *row.LocalClientID != claims.ClientID {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "you do not own that payment"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"payment": paymentRowToResponse(row),
	})
}

// finaliseCommit applies the local commit: debit stanje on the sender
// account and CAS-mark the payment row committed.
func (h *InterbankPaymentHTTPHandler) finaliseCommit(row *models.InterbankPayment) error {
	if row.LocalAccountID == nil {
		return fmt.Errorf("payment row missing local_account_id")
	}
	err := h.db.Transaction(func(dbtx *gorm.DB) error {
		rows, err := h.paymentRepo.MarkCommittedCAS(dbtx, row.TxRoutingNumber, row.TxID)
		if err != nil {
			return err
		}
		if rows == 0 {
			// Already resolved by a concurrent caller — leave alone.
			return nil
		}
		if err := h.walletRepo.Debit(dbtx, *row.LocalAccountID, row.Amount); err != nil {
			return err
		}
		row.Status = models.InterbankPaymentStatusCommitted
		return nil
	})
	return err
}

// finaliseRejected applies the partner-voted-NO finalise: release the
// reservation, CAS-mark the payment rejected, and stamp
// partner_finalised_at — the partner voted NO so it never reserved
// anything and no terminal message is owed.
func (h *InterbankPaymentHTTPHandler) finaliseRejected(row *models.InterbankPayment, reason string) error {
	if row.LocalAccountID == nil {
		return fmt.Errorf("payment row missing local_account_id")
	}
	return h.db.Transaction(func(dbtx *gorm.DB) error {
		rows, err := h.paymentRepo.MarkRejectedCAS(dbtx, row.TxRoutingNumber, row.TxID, reason)
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		row.Status = models.InterbankPaymentStatusRejected
		row.LastError = reason
		if _, err := h.paymentRepo.MarkPartnerFinalised(dbtx, row.TxRoutingNumber, row.TxID); err != nil {
			return err
		}
		return h.walletRepo.Release(dbtx, *row.LocalAccountID, row.Amount)
	})
}

// markCommitDispatched stamps PartnerFinalisedAt after SendCommitTx
// succeeds, so the reconciliation cron stops re-sending it. Best-effort
// — a failure here just means the cron will resend COMMIT_TX, which is
// idempotent on the partner side.
func (h *InterbankPaymentHTTPHandler) markCommitDispatched(row *models.InterbankPayment) {
	err := h.db.Transaction(func(dbtx *gorm.DB) error {
		_, mErr := h.paymentRepo.MarkPartnerFinalised(dbtx, row.TxRoutingNumber, row.TxID)
		return mErr
	})
	if err != nil {
		slog.Warn("interbank: failed to stamp payment partner_finalised_at after COMMIT_TX",
			"err", err, "payment_id", row.ID)
	}
}

// finaliseTransportFailure releases the reservation and CAS-marks the
// payment as failed when NEW_TX itself returned a transport-level
// error (timeout, connection refused, 5xx, 202-poll budget exhausted).
// Then best-effort sends ROLLBACK_TX so the partner can drop any
// half-applied state.
func (h *InterbankPaymentHTTPHandler) finaliseTransportFailure(row *models.InterbankPayment, recipientRouting interbank.RoutingNumber, _ interbank.IdempotenceKey, dispatchErr error) {
	if row.LocalAccountID == nil {
		slog.Error("interbank: cannot release failed payment — missing local_account_id",
			"payment_id", row.ID)
		return
	}
	txErr := h.db.Transaction(func(dbtx *gorm.DB) error {
		rows, err := h.paymentRepo.MarkFailedCAS(dbtx, row.TxRoutingNumber, row.TxID, dispatchErr.Error())
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		row.Status = models.InterbankPaymentStatusFailed
		row.LastError = dispatchErr.Error()
		return h.walletRepo.Release(dbtx, *row.LocalAccountID, row.Amount)
	})
	if txErr != nil {
		slog.Error("interbank: failed to release payment after transport failure",
			"err", txErr, "payment_id", row.ID)
		return
	}

	// Best-effort ROLLBACK_TX so the partner clears any partial state.
	// The partner may have never seen NEW_TX (timeout BEFORE delivery)
	// — in that case the partner's idempotence cache will treat
	// ROLLBACK_TX as a no-op (no pending row to flip). Either way the
	// partner is left in a consistent state.
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rbKey := h.client.NewIdempotenceKey()
	txID := interbank.ForeignBankId{
		RoutingNumber: interbank.RoutingNumber(row.TxRoutingNumber),
		ID:            row.TxID,
	}
	if err := h.client.SendRollbackTx(rollbackCtx, recipientRouting, rbKey, txID); err != nil {
		slog.Warn("interbank: best-effort ROLLBACK_TX failed",
			"err", err, "payment_id", row.ID, "tx_id", row.TxID)
		return
	}
	if dbErr := h.db.Transaction(func(dbtx *gorm.DB) error {
		_, mErr := h.paymentRepo.MarkPartnerFinalised(dbtx, row.TxRoutingNumber, row.TxID)
		return mErr
	}); dbErr != nil {
		slog.Warn("interbank: failed to stamp payment partner_finalised_at after ROLLBACK_TX",
			"err", dbErr, "payment_id", row.ID)
	}
}

// preparePaymentErr carries a status code + user-facing message out of
// the prepare DB transaction without leaking a 500 for predictable
// validation failures (insufficient funds, no such account, etc.).
type preparePaymentErr struct {
	status int
	msg    string
}

func (e preparePaymentErr) Error() string { return e.msg }

// summariseNoVote turns the partner's reason list into a short string
// for storage in InterbankPayment.last_error and for surfacing to the
// FE. Keeps the structured vote on the wire but renders something the
// user can read.
func summariseNoVote(vote *interbank.TransactionVote) string {
	if vote == nil || len(vote.Reasons) == 0 {
		return "partner voted NO without reason"
	}
	parts := make([]string, 0, len(vote.Reasons))
	for _, r := range vote.Reasons {
		parts = append(parts, string(r.Reason))
	}
	return "partner voted NO: " + strings.Join(parts, ", ")
}

// paymentRowToResponse shapes a payment row for the FE.
func paymentRowToResponse(row *models.InterbankPayment) map[string]interface{} {
	out := map[string]interface{}{
		"id":                     row.ID,
		"transactionId":          map[string]interface{}{"routingNumber": row.TxRoutingNumber, "id": row.TxID},
		"direction":              row.Direction,
		"partnerRoutingNumber":   row.PartnerRoutingNumber,
		"senderAccountNumber":    row.SenderAccountNumber,
		"recipientAccountNumber": row.RecipientAccountNumber,
		"currency":               row.Currency,
		"amount":                 row.Amount,
		"message":                row.Message,
		"paymentCode":            row.PaymentCode,
		"paymentPurpose":         row.PaymentPurpose,
		"status":                 row.Status,
		"lastError":              row.LastError,
		"createdAt":              row.CreatedAt,
		"updatedAt":              row.UpdatedAt,
	}
	if row.ResolvedAt != nil {
		out["resolvedAt"] = row.ResolvedAt
	}
	return out
}
