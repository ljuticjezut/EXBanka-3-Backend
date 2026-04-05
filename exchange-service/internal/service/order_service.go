package service

import (
	"fmt"
	"math"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

const (
	// Market order commission: min(14% * price, $7)
	marketCommissionRate = 0.14
	marketCommissionCap  = 7.0

	// Limit order commission: min(24% * price, $12)
	limitCommissionRate = 0.24
	limitCommissionCap  = 12.0
)

// OrderService handles order creation, approval, and cancellation.
type OrderService struct {
	orderRepo  *repository.OrderRepository
	marketRepo *repository.MarketRepository
}

func NewOrderService(orderRepo *repository.OrderRepository, marketRepo *repository.MarketRepository) *OrderService {
	return &OrderService{
		orderRepo:  orderRepo,
		marketRepo: marketRepo,
	}
}

// CreateOrderInput holds all fields required to place an order.
type CreateOrderInput struct {
	UserID       uint
	UserType     string   // "client" or "employee"
	AssetTicker  string
	OrderType    string   // "market", "limit", "stop", "stop_limit"
	Direction    string   // "buy" or "sell"
	Quantity     int64
	ContractSize int64    // defaults to 1 if 0
	LimitValue   *float64 // required for limit / stop_limit
	StopValue    *float64 // required for stop / stop_limit
	IsAON        bool
	IsMargin     bool
	AccountID    uint
	AfterHours   bool
}

// CreateOrderResult is the full order record returned after creation.
type CreateOrderResult struct {
	Order      *models.OrderRecord
	Commission float64
	TotalPrice float64
}

// CreateOrder validates input and persists a new order.
// Market orders use current ask/bid. Limit orders use limit_value (or better if
// conditions are already met). Stop/stop-limit conditions are evaluated by the
// cron executor (Phase 3).
func (s *OrderService) CreateOrder(input CreateOrderInput) (*CreateOrderResult, error) {
	if err := validateOrderInput(input); err != nil {
		return nil, err
	}

	contractSize := input.ContractSize
	if contractSize <= 0 {
		contractSize = 1
	}

	// Fetch the listing to get current prices.
	listing, err := s.marketRepo.GetListingRecordByTicker(input.AssetTicker)
	if err != nil || listing == nil {
		return nil, fmt.Errorf("asset not found: %s", input.AssetTicker)
	}

	// Determine price per unit based on order type and current market.
	pricePerUnit := orderPricePerUnit(listing, input)

	// Total order value (before commission).
	totalPrice := round2(float64(contractSize) * pricePerUnit * float64(input.Quantity))

	// Commission depends on order type.
	commission := calcCommission(input.OrderType, totalPrice)

	// Determine initial order status.
	status, needsApproval, err := s.resolveStatus(input, totalPrice)
	if err != nil {
		return nil, err
	}

	// For agent orders that don't need approval, increment their usedLimit.
	if input.UserType == "employee" && !needsApproval {
		if err := s.orderRepo.IncrementUsedLimit(input.UserID, totalPrice); err != nil {
			return nil, fmt.Errorf("failed to update actuary used limit: %w", err)
		}
	}

	now := time.Now().UTC()
	order := &models.OrderRecord{
		UserID:            input.UserID,
		UserType:          input.UserType,
		AssetID:           listing.ID,
		OrderType:         input.OrderType,
		Direction:         input.Direction,
		Quantity:          input.Quantity,
		ContractSize:      contractSize,
		PricePerUnit:      pricePerUnit,
		LimitValue:        input.LimitValue,
		StopValue:         input.StopValue,
		IsAON:             input.IsAON,
		IsMargin:          input.IsMargin,
		Status:            status,
		IsDone:            false,
		RemainingPortions: input.Quantity,
		AfterHours:        input.AfterHours,
		AccountID:         input.AccountID,
		LastModification:  now,
		CreatedAt:         now,
	}

	if err := s.orderRepo.CreateOrder(order); err != nil {
		return nil, fmt.Errorf("failed to persist order: %w", err)
	}

	return &CreateOrderResult{
		Order:      order,
		Commission: commission,
		TotalPrice: totalPrice,
	}, nil
}

// resolveStatus determines the initial status and whether supervisor approval is needed.
func (s *OrderService) resolveStatus(input CreateOrderInput, totalPrice float64) (status string, needsApproval bool, err error) {
	// Client orders are always auto-approved.
	if input.UserType == "client" {
		return "approved", false, nil
	}

	// Employee orders: check actuary profile.
	profile, err := s.orderRepo.GetActuaryProfile(input.UserID)
	if err != nil {
		return "", false, fmt.Errorf("failed to read actuary profile: %w", err)
	}

	// No actuary profile means basic employee — cannot place orders.
	if profile == nil {
		return "", false, fmt.Errorf("employee does not have trading permissions")
	}

	// Supervisor: no limit, always approved without supervisor review of self.
	if profile.Limit == nil {
		return "approved", false, nil
	}

	// Agent: check if approval required.
	remaining := *profile.Limit - profile.UsedLimit
	if profile.NeedApproval || profile.UsedLimit >= *profile.Limit || totalPrice > remaining {
		return "pending", true, nil
	}

	return "approved", false, nil
}

// GetOrder returns a single order by ID.
func (s *OrderService) GetOrder(id uint) (*models.OrderRecord, error) {
	order, err := s.orderRepo.GetOrderByID(id)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, fmt.Errorf("order not found")
	}
	return order, nil
}

// ListOrdersForUser returns orders for a user with optional status filter.
func (s *OrderService) ListOrdersForUser(userID uint, userType, statusFilter string) ([]models.OrderRecord, error) {
	return s.orderRepo.ListOrdersForUser(userID, userType, statusFilter)
}

// ListTransactionsForOrder returns all fill events for an order.
func (s *OrderService) ListTransactionsForOrder(orderID uint) ([]models.OrderTransactionRecord, error) {
	return s.orderRepo.ListTransactionsForOrder(orderID)
}

// --- Helpers ---

func validateOrderInput(input CreateOrderInput) error {
	if input.UserID == 0 {
		return fmt.Errorf("user ID is required")
	}
	if input.UserType != "client" && input.UserType != "employee" {
		return fmt.Errorf("user type must be 'client' or 'employee'")
	}
	if input.AssetTicker == "" {
		return fmt.Errorf("asset ticker is required")
	}
	if input.Direction != "buy" && input.Direction != "sell" {
		return fmt.Errorf("direction must be 'buy' or 'sell'")
	}
	if input.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	if input.AccountID == 0 {
		return fmt.Errorf("account ID is required")
	}

	switch input.OrderType {
	case "market":
		// no extra fields required
	case "limit":
		if input.LimitValue == nil {
			return fmt.Errorf("limit_value is required for limit orders")
		}
	case "stop":
		if input.StopValue == nil {
			return fmt.Errorf("stop_value is required for stop orders")
		}
	case "stop_limit":
		if input.StopValue == nil || input.LimitValue == nil {
			return fmt.Errorf("stop_value and limit_value are required for stop_limit orders")
		}
	default:
		return fmt.Errorf("order type must be one of: market, limit, stop, stop_limit")
	}

	return nil
}

// orderPricePerUnit determines the price per unit to record on the order.
//
// Market: buy fills at ask, sell fills at bid.
//
// Limit: use the better of the limit price and the current market price if the
// condition is already satisfied at placement time, otherwise fall back to
// limit_value (worst-case committed price used for commission + limit checks).
//   - Buy limit:  condition = ask <= limit_value  → fill price = ask
//   - Sell limit: condition = bid >= limit_value  → fill price = bid
//
// Stop / stop-limit: the stop condition is evaluated by the cron executor; store
// stop_value as the committed reference price for commission + limit checks.
func orderPricePerUnit(listing *models.MarketListingRecord, input CreateOrderInput) float64 {
	switch input.OrderType {
	case "limit":
		if input.Direction == "buy" && listing.Ask <= *input.LimitValue {
			return listing.Ask // already satisfiable — get the better price
		}
		if input.Direction == "sell" && listing.Bid >= *input.LimitValue {
			return listing.Bid // already satisfiable — get the better price
		}
		return *input.LimitValue // conditions not met yet; use committed limit price

	default: // "market"
		if input.Direction == "buy" {
			return listing.Ask
		}
		return listing.Bid
	}
}

// calcCommission computes the commission for an order.
// Market: min(14% * price, $7)
// Limit/Stop/Stop-Limit: min(24% * price, $12)
func calcCommission(orderType string, totalPrice float64) float64 {
	if orderType == "market" {
		return math.Min(marketCommissionRate*totalPrice, marketCommissionCap)
	}
	return math.Min(limitCommissionRate*totalPrice, limitCommissionCap)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
