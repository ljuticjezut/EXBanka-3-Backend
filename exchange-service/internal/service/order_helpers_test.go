package service

import (
	"strings"
	"testing"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
)

func ptrFloat(v float64) *float64 { return &v }

func validInput() CreateOrderInput {
	return CreateOrderInput{
		UserID:      0,
		UserType:    "bank",
		AssetTicker: "AAPL",
		OrderType:   "market",
		Direction:   "buy",
		Quantity:    1,
		AccountID:   1,
	}
}

func TestValidateOrderInput_Valid(t *testing.T) {
	if err := validateOrderInput(validInput()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateOrderInput_BadUserType(t *testing.T) {
	in := validInput()
	in.UserType = "employee"
	err := validateOrderInput(in)
	if err == nil || !strings.Contains(err.Error(), "user type") {
		t.Fatalf("expected user-type error, got %v", err)
	}
}

func TestValidateOrderInput_ClientWithoutUserID(t *testing.T) {
	in := validInput()
	in.UserType = "client"
	in.UserID = 0
	err := validateOrderInput(in)
	if err == nil || !strings.Contains(err.Error(), "user ID") {
		t.Fatalf("expected user-id error, got %v", err)
	}
}

func TestValidateOrderInput_MissingTicker(t *testing.T) {
	in := validInput()
	in.AssetTicker = ""
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected ticker error")
	}
}

func TestValidateOrderInput_BadDirection(t *testing.T) {
	in := validInput()
	in.Direction = "hold"
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected direction error")
	}
}

func TestValidateOrderInput_NonPositiveQuantity(t *testing.T) {
	in := validInput()
	in.Quantity = 0
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected quantity error")
	}
}

func TestValidateOrderInput_MissingAccount(t *testing.T) {
	in := validInput()
	in.AccountID = 0
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected account error")
	}
}

func TestValidateOrderInput_LimitNeedsLimitValue(t *testing.T) {
	in := validInput()
	in.OrderType = "limit"
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected limit_value error")
	}
	in.LimitValue = ptrFloat(100)
	if err := validateOrderInput(in); err != nil {
		t.Fatalf("limit with limit_value should be valid, got %v", err)
	}
}

func TestValidateOrderInput_StopNeedsStopValue(t *testing.T) {
	in := validInput()
	in.OrderType = "stop"
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected stop_value error")
	}
	in.StopValue = ptrFloat(100)
	if err := validateOrderInput(in); err != nil {
		t.Fatalf("stop with stop_value should be valid, got %v", err)
	}
}

func TestValidateOrderInput_StopLimitNeedsBoth(t *testing.T) {
	in := validInput()
	in.OrderType = "stop_limit"
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected stop_limit error when both missing")
	}
	in.LimitValue = ptrFloat(100)
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected stop_limit error when only limit set")
	}
	in.StopValue = ptrFloat(95)
	if err := validateOrderInput(in); err != nil {
		t.Fatalf("stop_limit with both should be valid, got %v", err)
	}
}

func TestValidateOrderInput_UnknownOrderType(t *testing.T) {
	in := validInput()
	in.OrderType = "foobar"
	if err := validateOrderInput(in); err == nil {
		t.Fatal("expected unknown order type error")
	}
}

// --- orderPricePerUnit ---

func makeListing(ask, bid float64) *models.MarketListingRecord {
	return &models.MarketListingRecord{Ask: ask, Bid: bid}
}

func TestOrderPricePerUnit_MarketBuyUsesAsk(t *testing.T) {
	got := orderPricePerUnit(makeListing(101, 100), CreateOrderInput{OrderType: "market", Direction: "buy"})
	if got != 101 {
		t.Errorf("expected 101 (ask), got %v", got)
	}
}

func TestOrderPricePerUnit_MarketSellUsesBid(t *testing.T) {
	got := orderPricePerUnit(makeListing(101, 100), CreateOrderInput{OrderType: "market", Direction: "sell"})
	if got != 100 {
		t.Errorf("expected 100 (bid), got %v", got)
	}
}

func TestOrderPricePerUnit_LimitBuyConditionMet(t *testing.T) {
	got := orderPricePerUnit(makeListing(99, 98), CreateOrderInput{OrderType: "limit", Direction: "buy", LimitValue: ptrFloat(100)})
	if got != 99 {
		t.Errorf("expected ask=99 when limit triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_LimitBuyConditionNotMet(t *testing.T) {
	got := orderPricePerUnit(makeListing(105, 104), CreateOrderInput{OrderType: "limit", Direction: "buy", LimitValue: ptrFloat(100)})
	if got != 100 {
		t.Errorf("expected limit=100 when not triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_LimitSellConditionMet(t *testing.T) {
	got := orderPricePerUnit(makeListing(101, 100), CreateOrderInput{OrderType: "limit", Direction: "sell", LimitValue: ptrFloat(99)})
	if got != 100 {
		t.Errorf("expected bid=100 when sell-limit triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_LimitSellConditionNotMet(t *testing.T) {
	got := orderPricePerUnit(makeListing(95, 94), CreateOrderInput{OrderType: "limit", Direction: "sell", LimitValue: ptrFloat(100)})
	if got != 100 {
		t.Errorf("expected limit=100 when not triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_StopBuyTriggered(t *testing.T) {
	got := orderPricePerUnit(makeListing(110, 109), CreateOrderInput{OrderType: "stop", Direction: "buy", StopValue: ptrFloat(100)})
	if got != 110 {
		t.Errorf("expected ask=110 when stop triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_StopBuyNotTriggered(t *testing.T) {
	got := orderPricePerUnit(makeListing(99, 98), CreateOrderInput{OrderType: "stop", Direction: "buy", StopValue: ptrFloat(100)})
	if got != 100 {
		t.Errorf("expected stop=100 when not triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_StopSellTriggered(t *testing.T) {
	got := orderPricePerUnit(makeListing(91, 90), CreateOrderInput{OrderType: "stop", Direction: "sell", StopValue: ptrFloat(100)})
	if got != 90 {
		t.Errorf("expected bid=90 when sell-stop triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_StopSellNotTriggered(t *testing.T) {
	got := orderPricePerUnit(makeListing(110, 109), CreateOrderInput{OrderType: "stop", Direction: "sell", StopValue: ptrFloat(100)})
	if got != 100 {
		t.Errorf("expected stop=100 when not triggered, got %v", got)
	}
}

func TestOrderPricePerUnit_StopLimitUsesLimit(t *testing.T) {
	got := orderPricePerUnit(makeListing(110, 109), CreateOrderInput{OrderType: "stop_limit", Direction: "buy", StopValue: ptrFloat(100), LimitValue: ptrFloat(105)})
	if got != 105 {
		t.Errorf("expected limit=105 for stop_limit, got %v", got)
	}
}

// --- calcCommission ---

func almostEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func TestCalcCommission_MarketBelowCap(t *testing.T) {
	got := calcCommission("market", 10) // 14% of 10 = 1.4
	if !almostEqual(got, 1.4) {
		t.Errorf("expected 1.4, got %v", got)
	}
}

func TestCalcCommission_MarketCapped(t *testing.T) {
	got := calcCommission("market", 1000)
	if got != marketCommissionCap {
		t.Errorf("expected cap=%v, got %v", marketCommissionCap, got)
	}
}

func TestCalcCommission_StopUsesMarketRate(t *testing.T) {
	got := calcCommission("stop", 10)
	if !almostEqual(got, 1.4) {
		t.Errorf("expected 1.4 for stop, got %v", got)
	}
}

func TestCalcCommission_LimitBelowCap(t *testing.T) {
	got := calcCommission("limit", 10) // 24% of 10 = 2.4
	if !almostEqual(got, 2.4) {
		t.Errorf("expected 2.4, got %v", got)
	}
}

func TestCalcCommission_LimitCapped(t *testing.T) {
	got := calcCommission("limit", 1000)
	if got != limitCommissionCap {
		t.Errorf("expected cap=%v, got %v", limitCommissionCap, got)
	}
}

func TestCalcCommission_StopLimitUsesLimitRate(t *testing.T) {
	got := calcCommission("stop_limit", 10)
	if !almostEqual(got, 2.4) {
		t.Errorf("expected 2.4 for stop_limit, got %v", got)
	}
}

// --- computeInitialMarginCost (Initial Margin Cost = MaintenanceMargin × 1.1) ---

func TestComputeInitialMarginCost_Stock(t *testing.T) {
	svc := &OrderService{}
	listing := &models.MarketListingRecord{Type: "stock"}
	// 10 shares × $100 × 50% (stock rate) × 1.1 (IMC buffer) × 1.0 (same currency) = 550
	got, err := svc.computeInitialMarginCost(listing, 10, 1, 100, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 550) {
		t.Errorf("expected stock IMC=550, got %v", got)
	}
}

func TestComputeInitialMarginCost_StockWithContractSizeAndFx(t *testing.T) {
	svc := &OrderService{}
	listing := &models.MarketListingRecord{Type: "stock"}
	// 5 contracts × 100 shares × $50 × 50% × 1.1 × 1.2 (fx) = 16500
	got, err := svc.computeInitialMarginCost(listing, 5, 100, 50, 1.2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 16500) {
		t.Errorf("expected 16500, got %v", got)
	}
}

func TestComputeInitialMarginCost_Forex(t *testing.T) {
	svc := &OrderService{}
	listing := &models.MarketListingRecord{Type: "forex"}
	// 1000 units × 1 × $1.10 × 10% (forex rate) × 1.1 × 1.0 = 121
	got, err := svc.computeInitialMarginCost(listing, 1000, 1, 1.10, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 121) {
		t.Errorf("expected forex IMC=121, got %v", got)
	}
}

func TestComputeInitialMarginCost_Futures(t *testing.T) {
	svc := &OrderService{}
	listing := &models.MarketListingRecord{Type: "futures"}
	// 2 × 50 × $200 × 10% × 1.1 × 1.0 = 2200
	got, err := svc.computeInitialMarginCost(listing, 2, 50, 200, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 2200) {
		t.Errorf("expected futures IMC=2200, got %v", got)
	}
}

// --- round2 ---

func TestRound2(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{1.234, 1.23},
		{1.236, 1.24},
		{1.0, 1.0},
		{-1.236, -1.24},
	}
	for _, c := range cases {
		if got := round2(c.in); got != c.want {
			t.Errorf("round2(%v)=%v, want %v", c.in, got, c.want)
		}
	}
}
