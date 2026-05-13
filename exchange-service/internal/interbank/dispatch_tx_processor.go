package interbank

import (
	"context"
	"fmt"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

// DispatchTxProcessor multiplexes inbound /interbank messages across
// the three flows we support: OTC option acceptance (4 postings with
// PERSON accounts and an OPTION asset), direct cross-bank payments (2
// postings with ACCOUNT accounts and only MONAS assets), and OTC
// option exercise (4 postings with one OPTION account and MONAS+STOCK
// assets).
//
// For NEW_TX, classification is by posting shape. For COMMIT_TX and
// ROLLBACK_TX, the inbound message carries only a TransactionID, so
// classification is by looking up which processor previously persisted
// a pending row for that TransactionID. The protocol guarantees
// TransactionIDs are globally unique, so the lookup yields at most one
// match across all flows.
type DispatchTxProcessor struct {
	otc          *OtcTxProcessor
	payment      *PaymentTxProcessor
	exercise     *ExerciseTxProcessor
	paymentRepo  *repository.InterbankPaymentRepository
	pendingRepo  *repository.InterbankPendingTxRepository
	exerciseRepo *repository.InterbankExerciseRepository
}

// NewDispatchTxProcessor wires the multiplexer. Any inner processor
// may be nil (e.g. in tests that exercise only one flow); we treat a
// nil inner as "vote NO with UNACCEPTABLE_ASSET" for that shape.
func NewDispatchTxProcessor(
	otc *OtcTxProcessor,
	payment *PaymentTxProcessor,
	exercise *ExerciseTxProcessor,
	paymentRepo *repository.InterbankPaymentRepository,
	pendingRepo *repository.InterbankPendingTxRepository,
	exerciseRepo *repository.InterbankExerciseRepository,
) *DispatchTxProcessor {
	return &DispatchTxProcessor{
		otc:          otc,
		payment:      payment,
		exercise:     exercise,
		paymentRepo:  paymentRepo,
		pendingRepo:  pendingRepo,
		exerciseRepo: exerciseRepo,
	}
}

// OnNewTx classifies by posting shape and forwards to the right
// processor. The order matters: exercise has OPTION-typed accounts
// (unique among the three flows), payment has all-ACCOUNT all-MONAS
// (also unique), and OTC acceptance is the catch-all for the
// 4-posting PERSON shape. Bodies that match no shape get UNACCEPTABLE
// _ASSET so the partner can correct its envelope.
func (d *DispatchTxProcessor) OnNewTx(ctx context.Context, partner *PartnerBank, tx *Transaction) (*TransactionVote, error) {
	if IsExerciseShape(tx) {
		if d.exercise == nil {
			return voteNo(NoVoteReason{Reason: ReasonUnacceptableAsset}), nil
		}
		return d.exercise.OnNewTx(ctx, partner, tx)
	}
	if IsPaymentShape(tx) {
		if d.payment == nil {
			return voteNo(NoVoteReason{Reason: ReasonUnacceptableAsset}), nil
		}
		return d.payment.OnNewTx(ctx, partner, tx)
	}
	if d.otc == nil {
		return voteNo(NoVoteReason{Reason: ReasonUnacceptableAsset}), nil
	}
	return d.otc.OnNewTx(ctx, partner, tx)
}

// OnCommitTx looks up the TransactionID in both pending-row tables.
// Whichever processor persisted the pending row owns the commit. If
// neither did, surface a 5xx so the partner sees that COMMIT_TX
// preceded NEW_TX (a protocol violation).
func (d *DispatchTxProcessor) OnCommitTx(ctx context.Context, partner *PartnerBank, txID ForeignBankId) error {
	target, err := d.classifyByTxID(txID)
	if err != nil {
		return err
	}
	switch target {
	case dispatchTargetPayment:
		return d.payment.OnCommitTx(ctx, partner, txID)
	case dispatchTargetExercise:
		return d.exercise.OnCommitTx(ctx, partner, txID)
	case dispatchTargetOtc:
		return d.otc.OnCommitTx(ctx, partner, txID)
	default:
		return fmt.Errorf("COMMIT_TX for unknown transaction %d/%s (no pending row in any flow)",
			txID.RoutingNumber, txID.ID)
	}
}

// OnRollbackTx mirrors OnCommitTx. A ROLLBACK_TX for an unknown
// TransactionID is treated as success — it means we voted NO (or never
// received NEW_TX), and the initiator's "notify all participants" rule
// allows the partner to send ROLLBACK_TX anyway.
func (d *DispatchTxProcessor) OnRollbackTx(ctx context.Context, partner *PartnerBank, txID ForeignBankId) error {
	target, err := d.classifyByTxID(txID)
	if err != nil {
		return err
	}
	switch target {
	case dispatchTargetPayment:
		return d.payment.OnRollbackTx(ctx, partner, txID)
	case dispatchTargetExercise:
		return d.exercise.OnRollbackTx(ctx, partner, txID)
	case dispatchTargetOtc:
		return d.otc.OnRollbackTx(ctx, partner, txID)
	default:
		// No row exists → we never voted YES on this TransactionID,
		// so there's nothing to roll back. Per protocol, return
		// success and let the inbound idempotence cache serve future
		// replays.
		return nil
	}
}

type dispatchTarget int

const (
	dispatchTargetNone dispatchTarget = iota
	dispatchTargetPayment
	dispatchTargetOtc
	dispatchTargetExercise
)

// classifyByTxID looks up which flow owns a given TransactionID. The
// protocol guarantees TransactionIDs are globally unique, so at most
// one repo will return a match — order of checks doesn't affect
// correctness, only the cheapest-first heuristic.
func (d *DispatchTxProcessor) classifyByTxID(txID ForeignBankId) (dispatchTarget, error) {
	if d.paymentRepo != nil {
		row, err := d.paymentRepo.GetByTxID(int(txID.RoutingNumber), txID.ID)
		if err != nil {
			return dispatchTargetNone, fmt.Errorf("classifying tx %d/%s by payment repo: %w",
				txID.RoutingNumber, txID.ID, err)
		}
		if row != nil {
			return dispatchTargetPayment, nil
		}
	}
	if d.exerciseRepo != nil {
		row, err := d.exerciseRepo.GetByTxID(int(txID.RoutingNumber), txID.ID)
		if err != nil {
			return dispatchTargetNone, fmt.Errorf("classifying tx %d/%s by exercise repo: %w",
				txID.RoutingNumber, txID.ID, err)
		}
		if row != nil {
			return dispatchTargetExercise, nil
		}
	}
	if d.pendingRepo != nil {
		row, err := d.pendingRepo.GetByTxID(int(txID.RoutingNumber), txID.ID)
		if err != nil {
			return dispatchTargetNone, fmt.Errorf("classifying tx %d/%s by OTC pending repo: %w",
				txID.RoutingNumber, txID.ID, err)
		}
		if row != nil {
			return dispatchTargetOtc, nil
		}
	}
	return dispatchTargetNone, nil
}
