package models

import "time"

const (
	InterbankInboundStatusReceived  = "received"
	InterbankInboundStatusProcessed = "processed"
	InterbankInboundStatusFailed    = "failed"
)

const (
	InterbankNegotiationRoleBuyer  = "buyer"
	InterbankNegotiationRoleSeller = "seller"
)

const (
	InterbankPendingTxStatusPending     = "pending"
	InterbankPendingTxStatusCommitted   = "committed"
	InterbankPendingTxStatusRolledBack  = "rolled_back"
	InterbankPendingTxStatusRejected    = "rejected"
)

const (
	InterbankOptionContractStatusValid     = "valid"
	InterbankOptionContractStatusExercised = "exercised"
	InterbankOptionContractStatusExpired   = "expired"
)

// InterbankInboundMessage is the audit + idempotence log for every
// /interbank request we accept from a partner bank. The composite
// (routing_number, locally_generated_key) is the protocol-defined
// idempotence key; the receiver MUST return an identical response on
// replay, so we persist the rendered response body alongside.
type InterbankInboundMessage struct {
	ID                  uint   `gorm:"primaryKey"`
	RoutingNumber       int    `gorm:"column:routing_number;not null;uniqueIndex:idx_interbank_idem_key,priority:1"`
	LocallyGeneratedKey string `gorm:"column:locally_generated_key;type:varchar(64);not null;uniqueIndex:idx_interbank_idem_key,priority:2"`

	MessageType string `gorm:"column:message_type;not null;index"`
	RequestBody string `gorm:"column:request_body;type:text;not null"`

	Status       string `gorm:"not null;default:'received';index"`
	HTTPStatus   int    `gorm:"column:http_status;not null;default:0"`
	ResponseBody string `gorm:"column:response_body;type:text"`
	Error        string `gorm:"type:text"`

	CreatedAt   time.Time  `gorm:"not null"`
	ProcessedAt *time.Time `gorm:"column:processed_at"`
}

func (InterbankInboundMessage) TableName() string { return "interbank_inbound_messages" }

// InterbankOtcNegotiation is our local copy of a cross-bank OTC option
// negotiation. The negotiation's globally-unique key is
// (NegotiationRoutingNumber, NegotiationID), which is the seller bank's
// routing number + the seller bank's locally-generated id (per spec
// §3.2 — the seller's bank mints the id when POST /negotiations
// arrives). Both banks store an identical copy and update it on each
// counter-offer.
type InterbankOtcNegotiation struct {
	ID uint `gorm:"primaryKey"`

	NegotiationRoutingNumber int    `gorm:"column:negotiation_routing_number;not null;uniqueIndex:idx_interbank_negotiation_key,priority:1"`
	NegotiationID            string `gorm:"column:negotiation_id;type:varchar(64);not null;uniqueIndex:idx_interbank_negotiation_key,priority:2"`

	// LocalRole is which side of the negotiation we play —
	// "buyer" if our client initiated, "seller" if a partner's
	// client posted to us.
	LocalRole string `gorm:"column:local_role;not null;index"`

	// CounterpartyRoutingNumber is the OTHER bank in this negotiation.
	// We always have exactly one counterparty (the buyer's bank or
	// the seller's bank — whichever isn't us).
	CounterpartyRoutingNumber int `gorm:"column:counterparty_routing_number;not null;index"`

	// Buyer / seller identities. The local-side identity is encoded
	// via interbank.EncodeLocalParticipantID; the remote-side is the
	// partner's opaque string.
	BuyerRoutingNumber  int    `gorm:"column:buyer_routing_number;not null"`
	BuyerID             string `gorm:"column:buyer_id;type:varchar(64);not null"`
	SellerRoutingNumber int    `gorm:"column:seller_routing_number;not null"`
	SellerID            string `gorm:"column:seller_id;type:varchar(64);not null"`

	StockTicker string `gorm:"column:stock_ticker;not null;index"`
	Amount      float64

	PricePerUnitCurrency string  `gorm:"column:price_per_unit_currency;not null"`
	PricePerUnitAmount   float64 `gorm:"column:price_per_unit_amount;not null"`
	PremiumCurrency      string  `gorm:"column:premium_currency;not null"`
	PremiumAmount        float64 `gorm:"column:premium_amount;not null"`

	// SettlementDate stores the ISO8601 timestamp as a string at the
	// DB boundary so we don't lose the partner's original timezone.
	SettlementDate string `gorm:"column:settlement_date;not null"`

	LastModifiedByRoutingNumber int    `gorm:"column:last_modified_by_routing_number;not null"`
	LastModifiedByID            string `gorm:"column:last_modified_by_id;type:varchar(64);not null"`

	IsOngoing bool `gorm:"column:is_ongoing;not null;default:true;index"`

	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (InterbankOtcNegotiation) TableName() string { return "interbank_otc_negotiations" }

// InterbankPendingTx is the per-TransactionID state machine row for an
// inbound NEW_TX that we voted YES on but haven't yet seen a
// COMMIT_TX/ROLLBACK_TX for. The protocol allows arbitrary delay between
// the vote and the resolution, so we persist what we reserved + what
// posting effect should fire on commit. Idempotence is enforced at the
// /interbank handler layer by InterbankInboundMessage; this table is
// the BUSINESS-layer record of "we promised to do X at commit time".
//
// The composite (TxRoutingNumber, TxID) is the protocol's
// transactionId.routingNumber + transactionId.id — i.e. the
// initiating bank's coordinates, which the protocol guarantees are
// globally unique.
type InterbankPendingTx struct {
	ID uint `gorm:"primaryKey"`

	TxRoutingNumber int    `gorm:"column:tx_routing_number;not null;uniqueIndex:idx_interbank_pending_tx_key,priority:1"`
	TxID            string `gorm:"column:tx_id;type:varchar(64);not null;uniqueIndex:idx_interbank_pending_tx_key,priority:2"`

	// PartnerRoutingNumber is the partner that POSTed the NEW_TX to us;
	// it's almost always equal to TxRoutingNumber (the initiator is
	// also the sender) but we record it separately to catch a
	// hypothetical relay-through-third-party case during audit.
	PartnerRoutingNumber int `gorm:"column:partner_routing_number;not null;index"`

	// NegotiationRoutingNumber + NegotiationID let us find back the
	// InterbankOtcNegotiation row that this NEW_TX is settling, so
	// COMMIT_TX can flip it closed + write the option contract.
	NegotiationRoutingNumber int    `gorm:"column:negotiation_routing_number;not null;index"`
	NegotiationID            string `gorm:"column:negotiation_id;type:varchar(64);not null"`

	// Resource-reservation snapshot. We persist what was reserved on
	// NEW_TX so we can release it on ROLLBACK_TX even if the original
	// envelope can't be replayed.
	ReservedFromLocalID  string  `gorm:"column:reserved_from_local_id;type:varchar(64);not null"`
	ReservedCurrency     string  `gorm:"column:reserved_currency;not null"`
	ReservedAmount       float64 `gorm:"column:reserved_amount;not null"`

	// Snapshot of the option contract terms — applied on COMMIT_TX to
	// create the InterbankOptionContract row.
	StockTicker          string  `gorm:"column:stock_ticker;not null"`
	OptionAmount         float64 `gorm:"column:option_amount;not null"`
	PricePerUnitCurrency string  `gorm:"column:price_per_unit_currency;not null"`
	PricePerUnitAmount   float64 `gorm:"column:price_per_unit_amount;not null"`
	SettlementDate       string  `gorm:"column:settlement_date;not null"`

	// Identities of the two sides; the local side is our customer.
	BuyerRoutingNumber  int    `gorm:"column:buyer_routing_number;not null"`
	BuyerID             string `gorm:"column:buyer_id;type:varchar(64);not null"`
	SellerRoutingNumber int    `gorm:"column:seller_routing_number;not null"`
	SellerID            string `gorm:"column:seller_id;type:varchar(64);not null"`

	Status string `gorm:"not null;default:'pending';index"`

	CreatedAt   time.Time  `gorm:"not null"`
	ResolvedAt  *time.Time `gorm:"column:resolved_at"`
}

func (InterbankPendingTx) TableName() string { return "interbank_pending_txs" }

// InterbankOptionContract is our local record of an option contract
// formed with a partner bank. We always sit on the BUYER side of these
// rows — when we're the seller, the contract is just the negotiation
// we've already closed plus a corresponding reservation in our local
// portfolio.
//
// The contract's global identity is the negotiation's identity
// (NegotiationRoutingNumber, NegotiationID) per spec §3.6.1 ("Set the
// OTC option contract negotiationId to the ID of the negotiation that
// lead to its creation. This ensures that the option pseudo-account is
// always in the bank of the seller").
type InterbankOptionContract struct {
	ID uint `gorm:"primaryKey"`

	NegotiationRoutingNumber int    `gorm:"column:negotiation_routing_number;not null;uniqueIndex:idx_interbank_option_contract_key,priority:1"`
	NegotiationID            string `gorm:"column:negotiation_id;type:varchar(64);not null;uniqueIndex:idx_interbank_option_contract_key,priority:2"`

	// The local user holding the option (always our side; this table
	// is only written when WE are the buyer's bank). For the
	// symmetric "we are the seller's bank" case the local-side
	// effect lives in the existing portfolio holding reservation, not
	// in this table.
	BuyerLocalID string `gorm:"column:buyer_local_id;type:varchar(64);not null;index"`

	SellerRoutingNumber int    `gorm:"column:seller_routing_number;not null"`
	SellerID            string `gorm:"column:seller_id;type:varchar(64);not null"`

	StockTicker          string  `gorm:"column:stock_ticker;not null;index"`
	Amount               float64 `gorm:"not null"`
	PricePerUnitCurrency string  `gorm:"column:price_per_unit_currency;not null"`
	PricePerUnitAmount   float64 `gorm:"column:price_per_unit_amount;not null"`
	PremiumCurrency      string  `gorm:"column:premium_currency;not null"`
	PremiumAmount        float64 `gorm:"column:premium_amount;not null"`
	SettlementDate       string  `gorm:"column:settlement_date;not null"`

	Status string `gorm:"not null;default:'valid';index"`

	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (InterbankOptionContract) TableName() string { return "interbank_option_contracts" }
