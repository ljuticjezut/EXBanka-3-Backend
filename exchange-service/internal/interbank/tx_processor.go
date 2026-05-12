package interbank

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

// OtcTxProcessor is the real TxProcessor — the one that replaces
// NoopProcessor once the OTC option acceptance flow is wired
// end-to-end. It only recognises the four-posting NEW_TX shape that
// /negotiations/{...}/accept produces (cash leg + option leg with
// PERSON accounts and MONAS+OPTION assets). Any other shape gets
// VoteNo with an appropriate reason.
//
// Scope note: cash reservation against our local user's balance is
// NOT yet enforced — that requires calling bank-service across the
// service boundary, which is a follow-up. Until then we persist the
// reservation snapshot in InterbankPendingTx so it can be honoured
// retroactively once the bank-service shim lands.
type OtcTxProcessor struct {
	registry     *Registry
	negRepo      *repository.InterbankOtcRepository
	pendingRepo  *repository.InterbankPendingTxRepository
	contractRepo *repository.InterbankOptionContractRepository
}

// NewOtcTxProcessor wires up the real TxProcessor implementation.
func NewOtcTxProcessor(
	registry *Registry,
	negRepo *repository.InterbankOtcRepository,
	pendingRepo *repository.InterbankPendingTxRepository,
	contractRepo *repository.InterbankOptionContractRepository,
) *OtcTxProcessor {
	return &OtcTxProcessor{
		registry:     registry,
		negRepo:      negRepo,
		pendingRepo:  pendingRepo,
		contractRepo: contractRepo,
	}
}

// OnNewTx implements TxProcessor.OnNewTx. Validates the 4-posting OTC
// option acceptance shape against a stored InterbankOtcNegotiation row
// and votes YES on a match. Anything else gets VoteNo + a reason code.
func (p *OtcTxProcessor) OnNewTx(_ context.Context, partner *PartnerBank, tx *Transaction) (*TransactionVote, error) {
	// Tx must be balanced — this also guards against malformed shapes
	// that happen to have 4 postings but skew amounts.
	if reason := checkBalanced(tx); reason != nil {
		return voteNo(*reason), nil
	}

	parsed, reason := parseOtcAcceptance(tx)
	if reason != nil {
		return voteNo(*reason), nil
	}

	// Look up the negotiation by the OPTION asset's NegotiationID. The
	// negotiation row is keyed by (NegotiationRoutingNumber, NegotiationID)
	// = the seller's bank's coordinates.
	neg, err := p.negRepo.Get(
		int(parsed.option.NegotiationID.RoutingNumber),
		parsed.option.NegotiationID.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("looking up negotiation: %w", err)
	}
	if neg == nil {
		return voteNo(NoVoteReason{
			Reason:  ReasonOptionNegotiationNotFound,
			Posting: parsed.optionLegBuyerPosting,
		}), nil
	}

	// The partner that POSTed this NEW_TX must be the seller's bank
	// (the initiator). Anything else is either a spoofed message or
	// a misrouted relay.
	if int(partner.Code) != neg.SellerRoutingNumber {
		slog.Warn("interbank: NEW_TX partner does not match negotiation seller",
			"partner", partner.Code,
			"negotiation_seller_routing", neg.SellerRoutingNumber,
			"negotiation_id", neg.NegotiationID,
		)
		return voteNo(NoVoteReason{Reason: ReasonOptionNegotiationNotFound}), nil
	}

	// We must be the buyer's bank to receive this NEW_TX. If we're the
	// seller's bank we wouldn't have dispatched against ourselves; if
	// we're neither, the partner shouldn't have sent it.
	ownRouting := int(p.registry.OwnRoutingNumber())
	if neg.BuyerRoutingNumber != ownRouting {
		return voteNo(NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.optionLegBuyerPosting}), nil
	}

	// Validate posting terms match the stored negotiation.
	if reason := matchAcceptanceTerms(parsed, neg); reason != nil {
		return voteNo(*reason), nil
	}

	// Negotiation must still be ongoing for accept to make sense; a
	// partner trying to settle a closed/declined negotiation gets a
	// soft NO so the flow can be retried after re-opening.
	if !neg.IsOngoing {
		return voteNo(NoVoteReason{Reason: ReasonOptionUsedOrExpired, Posting: parsed.optionLegBuyerPosting}), nil
	}

	// Persist the pending row so COMMIT_TX/ROLLBACK_TX can find back
	// what we promised. If an identical row already exists (NEW_TX
	// replay sneaking past the idempotence layer) treat as a no-op
	// success.
	existing, err := p.pendingRepo.GetByTxID(
		int(tx.TransactionID.RoutingNumber),
		tx.TransactionID.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("looking up pending tx: %w", err)
	}
	if existing == nil {
		row := &models.InterbankPendingTx{
			TxRoutingNumber:          int(tx.TransactionID.RoutingNumber),
			TxID:                     tx.TransactionID.ID,
			PartnerRoutingNumber:     int(partner.Code),
			NegotiationRoutingNumber: neg.NegotiationRoutingNumber,
			NegotiationID:            neg.NegotiationID,

			ReservedFromLocalID: neg.BuyerID,
			ReservedCurrency:    neg.PremiumCurrency,
			ReservedAmount:      neg.PremiumAmount,

			StockTicker:          neg.StockTicker,
			OptionAmount:         neg.Amount,
			PricePerUnitCurrency: neg.PricePerUnitCurrency,
			PricePerUnitAmount:   neg.PricePerUnitAmount,
			SettlementDate:       neg.SettlementDate,

			BuyerRoutingNumber:  neg.BuyerRoutingNumber,
			BuyerID:             neg.BuyerID,
			SellerRoutingNumber: neg.SellerRoutingNumber,
			SellerID:            neg.SellerID,

			Status: models.InterbankPendingTxStatusPending,
		}
		if err := p.pendingRepo.Create(row); err != nil {
			return nil, fmt.Errorf("persisting pending tx: %w", err)
		}
	}

	// TODO(interbank-balance): once bank-service exposes a "reserve"
	// shim, call it here for ReservedAmount of ReservedCurrency on
	// the local buyer; on failure return ReasonInsufficientAsset. For
	// now we record the intent and trust the partner.
	slog.Info("interbank: OTC option NEW_TX accepted (cash reservation deferred)",
		"tx_routing", tx.TransactionID.RoutingNumber,
		"tx_id", tx.TransactionID.ID,
		"buyer_local_id", neg.BuyerID,
		"reserve_currency", neg.PremiumCurrency,
		"reserve_amount", neg.PremiumAmount,
	)

	return &TransactionVote{Vote: VoteYes}, nil
}

// OnCommitTx implements TxProcessor.OnCommitTx. On the first commit for
// a given TransactionID we create the local option-contract row and
// flip the negotiation closed; subsequent invocations are no-ops by
// design (the protocol mandates byte-identical replay).
func (p *OtcTxProcessor) OnCommitTx(_ context.Context, _ *PartnerBank, txID ForeignBankId) error {
	pending, err := p.pendingRepo.GetByTxID(int(txID.RoutingNumber), txID.ID)
	if err != nil {
		return fmt.Errorf("loading pending tx: %w", err)
	}
	if pending == nil {
		// COMMIT_TX without a corresponding NEW_TX is a protocol
		// violation. We surface it so the partner sees a 500 and
		// can investigate; we don't fabricate state.
		return fmt.Errorf("COMMIT_TX for unknown transaction %d/%s", txID.RoutingNumber, txID.ID)
	}

	switch pending.Status {
	case models.InterbankPendingTxStatusCommitted:
		return nil // idempotent replay
	case models.InterbankPendingTxStatusRolledBack:
		return fmt.Errorf("COMMIT_TX for already-rolled-back transaction %d/%s", txID.RoutingNumber, txID.ID)
	case models.InterbankPendingTxStatusPending:
		// fall through to apply the commit
	default:
		return fmt.Errorf("pending tx %d/%s is in unknown status %q",
			txID.RoutingNumber, txID.ID, pending.Status)
	}

	// Create the local option-contract row (idempotent on the
	// negotiation key — if a row already exists, skip).
	existingContract, err := p.contractRepo.Get(pending.NegotiationRoutingNumber, pending.NegotiationID)
	if err != nil {
		return fmt.Errorf("loading option contract: %w", err)
	}
	if existingContract == nil {
		contract := &models.InterbankOptionContract{
			NegotiationRoutingNumber: pending.NegotiationRoutingNumber,
			NegotiationID:            pending.NegotiationID,
			BuyerLocalID:             pending.BuyerID,
			SellerRoutingNumber:      pending.SellerRoutingNumber,
			SellerID:                 pending.SellerID,
			StockTicker:              pending.StockTicker,
			Amount:                   pending.OptionAmount,
			PricePerUnitCurrency:     pending.PricePerUnitCurrency,
			PricePerUnitAmount:       pending.PricePerUnitAmount,
			PremiumCurrency:          pending.ReservedCurrency,
			PremiumAmount:            pending.ReservedAmount,
			SettlementDate:           pending.SettlementDate,
			Status:                   models.InterbankOptionContractStatusValid,
		}
		if err := p.contractRepo.Create(contract); err != nil {
			return fmt.Errorf("creating option contract: %w", err)
		}
	}

	if err := p.negRepo.MarkClosed(pending.NegotiationRoutingNumber, pending.NegotiationID); err != nil {
		return fmt.Errorf("closing negotiation on commit: %w", err)
	}

	// TODO(interbank-balance): on commit we owe the bank-service shim
	// a "deduct reserved" call so the premium actually leaves the
	// buyer's wallet. Today we just mark the pending row committed.
	if err := p.pendingRepo.MarkCommitted(int(txID.RoutingNumber), txID.ID); err != nil {
		return fmt.Errorf("marking pending tx committed: %w", err)
	}

	slog.Info("interbank: OTC option COMMIT_TX applied",
		"tx_routing", txID.RoutingNumber,
		"tx_id", txID.ID,
		"negotiation_id", pending.NegotiationID,
	)
	return nil
}

// OnRollbackTx implements TxProcessor.OnRollbackTx. Releases the
// reservation snapshot (logged TODO until bank-service is wired) and
// flips the pending row to rolled_back. Idempotent: replays after the
// first rollback are no-ops.
func (p *OtcTxProcessor) OnRollbackTx(_ context.Context, _ *PartnerBank, txID ForeignBankId) error {
	pending, err := p.pendingRepo.GetByTxID(int(txID.RoutingNumber), txID.ID)
	if err != nil {
		return fmt.Errorf("loading pending tx: %w", err)
	}
	if pending == nil {
		// No record means we never voted YES (or the row was already
		// cleaned up). The protocol's "MUST return identical response"
		// is satisfied by the upstream idempotence cache; here we
		// just succeed.
		return nil
	}

	switch pending.Status {
	case models.InterbankPendingTxStatusRolledBack:
		return nil // idempotent
	case models.InterbankPendingTxStatusCommitted:
		return fmt.Errorf("ROLLBACK_TX for already-committed transaction %d/%s", txID.RoutingNumber, txID.ID)
	case models.InterbankPendingTxStatusPending:
		// proceed
	default:
		return fmt.Errorf("pending tx %d/%s is in unknown status %q",
			txID.RoutingNumber, txID.ID, pending.Status)
	}

	// TODO(interbank-balance): release the reservation in bank-service.
	if err := p.pendingRepo.MarkRolledBack(int(txID.RoutingNumber), txID.ID); err != nil {
		return fmt.Errorf("marking pending tx rolled back: %w", err)
	}

	slog.Info("interbank: OTC option ROLLBACK_TX applied",
		"tx_routing", txID.RoutingNumber,
		"tx_id", txID.ID,
		"negotiation_id", pending.NegotiationID,
	)
	return nil
}

// otcAcceptanceTx is a parsed view of a 4-posting OTC option acceptance
// NEW_TX. The four postings are: cash buyer (-P), cash seller (+P),
// option seller (-1), option buyer (+1).
type otcAcceptanceTx struct {
	cashLegBuyerPosting  *Posting
	cashLegSellerPosting *Posting
	optionLegSellerPosting *Posting
	optionLegBuyerPosting  *Posting

	premium     MonetaryAsset
	premiumAmt  float64
	option      *OptionDescription
	buyer       ForeignBankId
	seller      ForeignBankId
}

// parseOtcAcceptance classifies the four postings of an OTC acceptance
// NEW_TX. Returns a NoVoteReason if the shape doesn't match.
func parseOtcAcceptance(tx *Transaction) (*otcAcceptanceTx, *NoVoteReason) {
	if len(tx.Postings) != 4 {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset}
	}

	parsed := &otcAcceptanceTx{}
	for i := range tx.Postings {
		ptg := &tx.Postings[i]

		// All four postings must reference PERSON accounts (the OTC
		// acceptance shape; ACCOUNT-typed postings here would be a
		// different transaction kind we don't yet support).
		if ptg.Account.Type != TxAccountPerson || ptg.Account.ID == nil {
			return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
		}

		switch ptg.Asset.Type {
		case AssetMonas:
			if ptg.Asset.Monas == nil {
				return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
			}
			if ptg.Amount < 0 {
				if parsed.cashLegBuyerPosting != nil {
					return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
				}
				parsed.cashLegBuyerPosting = ptg
				parsed.buyer = *ptg.Account.ID
				parsed.premium = *ptg.Asset.Monas
				parsed.premiumAmt = -ptg.Amount
			} else {
				if parsed.cashLegSellerPosting != nil {
					return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
				}
				parsed.cashLegSellerPosting = ptg
				parsed.seller = *ptg.Account.ID
			}
		case AssetOption:
			if ptg.Asset.Option == nil {
				return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
			}
			if ptg.Amount < 0 {
				if parsed.optionLegSellerPosting != nil {
					return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
				}
				parsed.optionLegSellerPosting = ptg
				parsed.option = ptg.Asset.Option
			} else {
				if parsed.optionLegBuyerPosting != nil {
					return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
				}
				parsed.optionLegBuyerPosting = ptg
			}
		default:
			return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: ptg}
		}
	}

	if parsed.cashLegBuyerPosting == nil ||
		parsed.cashLegSellerPosting == nil ||
		parsed.optionLegSellerPosting == nil ||
		parsed.optionLegBuyerPosting == nil {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset}
	}

	// Option-leg amounts must be exactly +/-1: an option contract is a
	// single indivisible unit (Amount inside OptionDescription holds the
	// underlying stock count).
	if !nearlyEqual(parsed.optionLegSellerPosting.Amount, -1) || !nearlyEqual(parsed.optionLegBuyerPosting.Amount, 1) {
		return nil, &NoVoteReason{Reason: ReasonOptionAmountIncorrect, Posting: parsed.optionLegBuyerPosting}
	}

	// Cash leg amounts must offset.
	if !nearlyEqual(parsed.cashLegBuyerPosting.Amount, -parsed.cashLegSellerPosting.Amount) {
		return nil, &NoVoteReason{Reason: ReasonUnbalancedTx}
	}

	// Both option-leg postings must describe the same option.
	if !sameOption(parsed.option, parsed.optionLegBuyerPosting.Asset.Option) {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.optionLegBuyerPosting}
	}

	// The buyer of the cash leg must equal the buyer of the option leg
	// (and similarly for the seller). Otherwise the postings would not
	// describe a coherent option acceptance.
	if !sameForeignBankId(parsed.buyer, *parsed.optionLegBuyerPosting.Account.ID) {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.optionLegBuyerPosting}
	}
	if !sameForeignBankId(parsed.seller, *parsed.optionLegSellerPosting.Account.ID) {
		return nil, &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.optionLegSellerPosting}
	}

	return parsed, nil
}

// matchAcceptanceTerms checks that the parsed transaction's terms match
// the stored negotiation row exactly. Returns a NoVoteReason on
// mismatch.
func matchAcceptanceTerms(parsed *otcAcceptanceTx, neg *models.InterbankOtcNegotiation) *NoVoteReason {
	if int(parsed.buyer.RoutingNumber) != neg.BuyerRoutingNumber || parsed.buyer.ID != neg.BuyerID {
		return &NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.cashLegBuyerPosting}
	}
	if int(parsed.seller.RoutingNumber) != neg.SellerRoutingNumber || parsed.seller.ID != neg.SellerID {
		return &NoVoteReason{Reason: ReasonNoSuchAccount, Posting: parsed.cashLegSellerPosting}
	}
	if string(parsed.premium.Currency) != neg.PremiumCurrency {
		return &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.cashLegBuyerPosting}
	}
	if !nearlyEqual(parsed.premiumAmt, neg.PremiumAmount) {
		return &NoVoteReason{Reason: ReasonInsufficientAsset, Posting: parsed.cashLegBuyerPosting}
	}
	if parsed.option.Stock.Ticker != neg.StockTicker {
		return &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.optionLegBuyerPosting}
	}
	if string(parsed.option.PricePerUnit.Currency) != neg.PricePerUnitCurrency ||
		!nearlyEqual(parsed.option.PricePerUnit.Amount, neg.PricePerUnitAmount) {
		return &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.optionLegBuyerPosting}
	}
	if parsed.option.SettlementDate != neg.SettlementDate {
		return &NoVoteReason{Reason: ReasonUnacceptableAsset, Posting: parsed.optionLegBuyerPosting}
	}
	if !nearlyEqual(parsed.option.Amount, neg.Amount) {
		return &NoVoteReason{Reason: ReasonOptionAmountIncorrect, Posting: parsed.optionLegBuyerPosting}
	}
	if int(parsed.option.NegotiationID.RoutingNumber) != neg.NegotiationRoutingNumber ||
		parsed.option.NegotiationID.ID != neg.NegotiationID {
		return &NoVoteReason{Reason: ReasonOptionNegotiationNotFound, Posting: parsed.optionLegBuyerPosting}
	}
	return nil
}

// checkBalanced verifies that for every (account, asset) pair the
// posting amounts sum to zero. This is the §2.8.6 balance check.
//
// Implementation note: we group by asset type only (not account) since
// the spec says "regardless of account, all credited and debited
// amounts add up to zero" for a transaction to be balanced.
func checkBalanced(tx *Transaction) *NoVoteReason {
	sums := map[string]float64{}
	for i := range tx.Postings {
		key := assetGroupKey(&tx.Postings[i].Asset)
		sums[key] += tx.Postings[i].Amount
	}
	for _, v := range sums {
		if !nearlyEqual(v, 0) {
			return &NoVoteReason{Reason: ReasonUnbalancedTx}
		}
	}
	return nil
}

// assetGroupKey returns a key that uniquely identifies "the same
// asset" across postings. Two MONAS postings with the same currency
// are the same asset; two OPTION postings with the same negotiation id
// are the same asset; STOCK postings group by ticker.
func assetGroupKey(a *Asset) string {
	switch a.Type {
	case AssetMonas:
		if a.Monas == nil {
			return "monas:?"
		}
		return "monas:" + string(a.Monas.Currency)
	case AssetStock:
		if a.Stock == nil {
			return "stock:?"
		}
		return "stock:" + a.Stock.Ticker
	case AssetOption:
		if a.Option == nil {
			return "option:?"
		}
		return fmt.Sprintf("option:%d/%s",
			a.Option.NegotiationID.RoutingNumber,
			a.Option.NegotiationID.ID)
	default:
		return "?"
	}
}

func sameOption(a, b *OptionDescription) bool {
	if a == nil || b == nil {
		return false
	}
	return a.NegotiationID.RoutingNumber == b.NegotiationID.RoutingNumber &&
		a.NegotiationID.ID == b.NegotiationID.ID &&
		a.Stock.Ticker == b.Stock.Ticker &&
		a.PricePerUnit.Currency == b.PricePerUnit.Currency &&
		nearlyEqual(a.PricePerUnit.Amount, b.PricePerUnit.Amount) &&
		a.SettlementDate == b.SettlementDate &&
		nearlyEqual(a.Amount, b.Amount)
}

func sameForeignBankId(a, b ForeignBankId) bool {
	return a.RoutingNumber == b.RoutingNumber && a.ID == b.ID
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func voteNo(reason NoVoteReason) *TransactionVote {
	return &TransactionVote{Vote: VoteNo, Reasons: []NoVoteReason{reason}}
}
