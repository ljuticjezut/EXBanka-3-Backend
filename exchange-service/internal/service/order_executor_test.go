package service

import (
	"testing"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
)

func mkOrder(orderType, direction string) *models.OrderRecord {
	return &models.OrderRecord{OrderType: orderType, Direction: direction}
}

func TestEvaluateOrder_Market(t *testing.T) {
	if !evaluateOrder(mkOrder("market", "buy"), &models.MarketListingRecord{}).fillNow {
		t.Error("market should always fill")
	}
}

func TestEvaluateOrder_LimitBuy(t *testing.T) {
	o := mkOrder("limit", "buy")
	o.LimitValue = ptrFloat(100)
	if !evaluateOrder(o, &models.MarketListingRecord{Ask: 99}).fillNow {
		t.Error("buy limit should fill when ask<=limit")
	}
	if evaluateOrder(o, &models.MarketListingRecord{Ask: 101}).fillNow {
		t.Error("buy limit should NOT fill when ask>limit")
	}
}

func TestEvaluateOrder_LimitSell(t *testing.T) {
	o := mkOrder("limit", "sell")
	o.LimitValue = ptrFloat(100)
	if !evaluateOrder(o, &models.MarketListingRecord{Bid: 101}).fillNow {
		t.Error("sell limit should fill when bid>=limit")
	}
	if evaluateOrder(o, &models.MarketListingRecord{Bid: 99}).fillNow {
		t.Error("sell limit should NOT fill when bid<limit")
	}
}

func TestEvaluateOrder_StopBuy(t *testing.T) {
	o := mkOrder("stop", "buy")
	o.StopValue = ptrFloat(100)
	if !evaluateOrder(o, &models.MarketListingRecord{Ask: 101}).fillNow {
		t.Error("buy stop should fill when ask>stop")
	}
	if evaluateOrder(o, &models.MarketListingRecord{Ask: 99}).fillNow {
		t.Error("buy stop should NOT fill when ask<=stop")
	}
}

func TestEvaluateOrder_StopSell(t *testing.T) {
	o := mkOrder("stop", "sell")
	o.StopValue = ptrFloat(100)
	if !evaluateOrder(o, &models.MarketListingRecord{Bid: 99}).fillNow {
		t.Error("sell stop should fill when bid<stop")
	}
	if evaluateOrder(o, &models.MarketListingRecord{Bid: 101}).fillNow {
		t.Error("sell stop should NOT fill when bid>=stop")
	}
}

// stop_limit BUY: stop_value=100 arms the order (when ask >= 100), then it
// behaves as a limit order with limit_value=105 (fills while ask <= 105).
func TestEvaluateOrder_StopLimitBuy_ArmsAndFillsInsideWindow(t *testing.T) {
	o := mkOrder("stop_limit", "buy")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(105)

	// Ask=102 crosses stop and is inside limit window — should arm + fill on this tick.
	eval := evaluateOrder(o, &models.MarketListingRecord{Ask: 102})
	if !eval.fillNow || !eval.justTriggered {
		t.Errorf("expected fillNow=true justTriggered=true, got %+v", eval)
	}
}

func TestEvaluateOrder_StopLimitBuy_BelowStopDoesNothing(t *testing.T) {
	o := mkOrder("stop_limit", "buy")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(105)

	eval := evaluateOrder(o, &models.MarketListingRecord{Ask: 99})
	if eval.fillNow || eval.justTriggered {
		t.Errorf("ask below stop should neither arm nor fill, got %+v", eval)
	}
}

// Critical case: ask gaps above the limit. The order arms (stop crossed) but
// does not fill — it sits waiting for ask to drop back into the limit window.
func TestEvaluateOrder_StopLimitBuy_GapsAboveLimitArmsButDoesNotFill(t *testing.T) {
	o := mkOrder("stop_limit", "buy")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(105)

	eval := evaluateOrder(o, &models.MarketListingRecord{Ask: 110})
	if eval.fillNow {
		t.Errorf("ask above limit must not fill yet, got fillNow=true")
	}
	if !eval.justTriggered {
		t.Errorf("ask above stop should arm the latch even if outside limit window")
	}
}

// Once armed, the latch persists: on a later tick the order fills as a limit
// order EVEN IF the ask is now below the stop value (the latch is sticky).
func TestEvaluateOrder_StopLimitBuy_LatchSticksAfterPriceRetreats(t *testing.T) {
	o := mkOrder("stop_limit", "buy")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(105)
	o.StopTriggered = true // already armed from a prior tick

	// Ask=98 is below the stop, but since the latch is sticky, the order behaves
	// as a pure limit order and fills (ask <= limit_value).
	eval := evaluateOrder(o, &models.MarketListingRecord{Ask: 98})
	if !eval.fillNow {
		t.Errorf("latched order should fill while ask<=limit, got fillNow=false")
	}
	if eval.justTriggered {
		t.Errorf("already-armed order should not signal justTriggered, got true")
	}
}

// stop_limit SELL: stop_value=100 arms when bid <= 100, then limit_value=95
// gates the fill (sell while bid >= 95).
func TestEvaluateOrder_StopLimitSell_ArmsAndFillsInsideWindow(t *testing.T) {
	o := mkOrder("stop_limit", "sell")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(95)

	eval := evaluateOrder(o, &models.MarketListingRecord{Bid: 97})
	if !eval.fillNow || !eval.justTriggered {
		t.Errorf("expected fillNow=true justTriggered=true, got %+v", eval)
	}
}

func TestEvaluateOrder_StopLimitSell_AboveStopDoesNothing(t *testing.T) {
	o := mkOrder("stop_limit", "sell")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(95)

	eval := evaluateOrder(o, &models.MarketListingRecord{Bid: 105})
	if eval.fillNow || eval.justTriggered {
		t.Errorf("bid above stop should neither arm nor fill, got %+v", eval)
	}
}

func TestEvaluateOrder_StopLimitSell_GapsBelowLimitArmsButDoesNotFill(t *testing.T) {
	o := mkOrder("stop_limit", "sell")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(95)

	eval := evaluateOrder(o, &models.MarketListingRecord{Bid: 90})
	if eval.fillNow {
		t.Errorf("bid below limit must not fill yet, got fillNow=true")
	}
	if !eval.justTriggered {
		t.Errorf("bid below stop should arm the latch even if outside limit window")
	}
}

func TestEvaluateOrder_StopLimitSell_LatchSticksAfterPriceRecovers(t *testing.T) {
	o := mkOrder("stop_limit", "sell")
	o.StopValue = ptrFloat(100)
	o.LimitValue = ptrFloat(95)
	o.StopTriggered = true

	eval := evaluateOrder(o, &models.MarketListingRecord{Bid: 102})
	if !eval.fillNow {
		t.Errorf("latched sell order should fill while bid>=limit, got fillNow=false")
	}
	if eval.justTriggered {
		t.Errorf("already-armed order should not signal justTriggered, got true")
	}
}

func TestEvaluateOrder_UnknownType(t *testing.T) {
	eval := evaluateOrder(mkOrder("foo", "buy"), &models.MarketListingRecord{})
	if eval.fillNow || eval.justTriggered {
		t.Error("unknown order type should be inert")
	}
}

func TestFillQuantity_AON(t *testing.T) {
	o := &models.OrderRecord{IsAON: true, RemainingPortions: 5}
	if got := fillQuantity(o); got != 5 {
		t.Errorf("AON should return all 5, got %d", got)
	}
}

func TestFillQuantity_OneRemaining(t *testing.T) {
	o := &models.OrderRecord{RemainingPortions: 1}
	if got := fillQuantity(o); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestFillQuantity_RandomInRange(t *testing.T) {
	o := &models.OrderRecord{RemainingPortions: 10}
	for i := 0; i < 30; i++ {
		got := fillQuantity(o)
		if got < 1 || got > 10 {
			t.Fatalf("fillQuantity should be in [1,10], got %d", got)
		}
	}
}

func TestExecuteFillPrice_MarketBuy(t *testing.T) {
	o := mkOrder("market", "buy")
	if executeFillPrice(o, &models.MarketListingRecord{Ask: 50, Bid: 49}) != 50 {
		t.Error("market buy should fill at ask")
	}
}

func TestExecuteFillPrice_MarketSell(t *testing.T) {
	o := mkOrder("market", "sell")
	if executeFillPrice(o, &models.MarketListingRecord{Ask: 50, Bid: 49}) != 49 {
		t.Error("market sell should fill at bid")
	}
}

func TestExecuteFillPrice_LimitBuyUsesMin(t *testing.T) {
	o := mkOrder("limit", "buy")
	o.LimitValue = ptrFloat(100)
	if executeFillPrice(o, &models.MarketListingRecord{Ask: 99}) != 99 {
		t.Error("buy limit should fill at min(limit, ask)=99")
	}
	if executeFillPrice(o, &models.MarketListingRecord{Ask: 101}) != 100 {
		t.Error("buy limit should fill at min(limit, ask)=100")
	}
}

func TestExecuteFillPrice_LimitSellUsesMax(t *testing.T) {
	o := mkOrder("limit", "sell")
	o.LimitValue = ptrFloat(100)
	if executeFillPrice(o, &models.MarketListingRecord{Bid: 101}) != 101 {
		t.Error("sell limit should fill at max(limit, bid)=101")
	}
	if executeFillPrice(o, &models.MarketListingRecord{Bid: 99}) != 100 {
		t.Error("sell limit should fill at max(limit, bid)=100")
	}
}

func TestExecuteFillPrice_StopLimit(t *testing.T) {
	o := mkOrder("stop_limit", "buy")
	o.LimitValue = ptrFloat(100)
	if executeFillPrice(o, &models.MarketListingRecord{Ask: 95}) != 95 {
		t.Error("stop_limit buy should fill at min(limit, ask)=95")
	}
}
