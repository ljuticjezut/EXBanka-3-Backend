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
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/config"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/interbank"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/util"
)

// InterbankOtcHTTPHandler is the local-frontend entry point for the
// cross-bank OTC negotiation flow. It sits in front of interbank.Client
// (outbound /negotiations + /public-stock calls to partner banks) and
// repository.InterbankOtcRepository (our local copy of every
// negotiation we're a party to). The partner-facing wire surface lives
// in package interbank; this handler is the JWT-authenticated face of
// it for our own UI.
//
// The buyer-side option-exercise routes (option-contracts list/get/
// exercise) live on this handler too — they share the
// /api/v1/interbank-otc/* prefix and the same client + JWT context.
// The exercise implementation lives in interbank_exercise_http_handler.go.
type InterbankOtcHTTPHandler struct {
	cfg              *config.Config
	registry         *interbank.Registry
	client           *interbank.Client
	negRepo          *repository.InterbankOtcRepository
	negsHandler      *interbank.NegotiationsHandler
	stockCacheRepo   *repository.RemotePublicStockRepository
	contractRepo     *repository.InterbankOptionContractRepository
	exerciseRepo     *repository.InterbankExerciseRepository
	walletRepo       *repository.InterbankWalletRepository
	portfolioRepo    *repository.PortfolioRepository
	marketRepo       *repository.MarketRepository
	db               *gorm.DB
}

func NewInterbankOtcHTTPHandler(
	cfg *config.Config,
	registry *interbank.Registry,
	client *interbank.Client,
	negRepo *repository.InterbankOtcRepository,
	negsHandler *interbank.NegotiationsHandler,
	stockCacheRepo *repository.RemotePublicStockRepository,
	contractRepo *repository.InterbankOptionContractRepository,
	exerciseRepo *repository.InterbankExerciseRepository,
	walletRepo *repository.InterbankWalletRepository,
	portfolioRepo *repository.PortfolioRepository,
	marketRepo *repository.MarketRepository,
	db *gorm.DB,
) *InterbankOtcHTTPHandler {
	return &InterbankOtcHTTPHandler{
		cfg:            cfg,
		registry:       registry,
		client:         client,
		negRepo:        negRepo,
		negsHandler:    negsHandler,
		stockCacheRepo: stockCacheRepo,
		contractRepo:   contractRepo,
		exerciseRepo:   exerciseRepo,
		walletRepo:     walletRepo,
		portfolioRepo:  portfolioRepo,
		marketRepo:     marketRepo,
		db:             db,
	}
}

// publicStockStaleness is the cutoff beyond which a cached snapshot is
// marked `stale=true` in the response. The cron refreshes @every 5m, so
// 12m gives a comfortable two-tick window before stale flags appear.
const publicStockStaleness = 12 * time.Minute

// partnerStockResult is the per-partner intermediate for listPublicStocks.
// Used by both fanOutLive (live fan-out) and readCached (cache reads) so
// the merging loop in listPublicStocks doesn't care which source produced
// the data.
type partnerStockResult struct {
	code   interbank.RoutingNumber
	stocks interbank.PublicStocksResponse
	err    error
	stale  bool
}

// Routes dispatches /api/v1/interbank-otc/* to the right method.
func (h *InterbankOtcHTTPHandler) Routes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/interbank-otc/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")

	switch {
	case len(parts) == 1 && parts[0] == "public-stocks":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listPublicStocks(w, r)
	case len(parts) == 1 && parts[0] == "negotiations":
		switch r.Method {
		case http.MethodGet:
			h.listNegotiations(w, r)
		case http.MethodPost:
			h.createNegotiation(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 3 && parts[0] == "negotiations":
		routing, id, ok := parseRoutingAndID(parts[1], parts[2])
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "expected /negotiations/{routingNumber}/{id}"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			h.getNegotiation(w, r, routing, id)
		case http.MethodPut:
			h.updateNegotiation(w, r, routing, id)
		case http.MethodDelete:
			h.closeNegotiation(w, r, routing, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 4 && parts[0] == "negotiations" && parts[3] == "accept":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		routing, id, ok := parseRoutingAndID(parts[1], parts[2])
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "expected /negotiations/{routingNumber}/{id}/accept"})
			return
		}
		h.acceptNegotiation(w, r, routing, id)
	case len(parts) == 1 && parts[0] == "option-contracts":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listOptionContracts(w, r)
	case len(parts) == 2 && parts[0] == "option-contracts":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "contract id must be numeric"})
			return
		}
		h.getOptionContract(w, r, uint(id))
	case len(parts) == 3 && parts[0] == "option-contracts" && parts[2] == "exercise":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "contract id must be numeric"})
			return
		}
		h.exerciseOptionContract(w, r, uint(id))
	default:
		http.NotFound(w, r)
	}
}

// listPublicStocks aggregates partner /public-stock data into a single
// payload the local frontend can render. By default the data comes
// from the remote_public_stock_snapshots cache (refreshed by the
// PublicStockCacheRunner cron). Passing ?live=true bypasses the cache
// and does a parallel fan-out for callers that need fresh data right
// now (e.g. an explicit "refresh" button).
//
// Per-partner errors are reported per-bank rather than failing the
// whole call — one slow or down partner shouldn't blank the catalogue.
// Cached responses include a `stale=true` flag when the snapshot is
// older than publicStockStaleness.
func (h *InterbankOtcHTTPHandler) listPublicStocks(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireTradingAccessHTTP(w, claims) {
		return
	}

	live := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("live")), "true")

	partners := h.registry.All()
	bankNames := map[interbank.RoutingNumber]string{}
	for _, p := range partners {
		bankNames[p.Code] = p.DisplayName
	}

	var results []partnerStockResult
	if live || h.stockCacheRepo == nil {
		results = h.fanOutLive(r.Context(), partners)
	} else {
		liveResults, fellBack := h.readCached(partners)
		if fellBack {
			// No cache row exists at all — first run, no cron has
			// fired yet. Fan out live so the caller doesn't see an
			// empty list on cold start.
			results = h.fanOutLive(r.Context(), partners)
		} else {
			results = liveResults
		}
	}

	type stockResp struct {
		Ticker  string `json:"ticker"`
		Sellers []struct {
			BankRoutingNumber int     `json:"bankRoutingNumber"`
			BankDisplayName   string  `json:"bankDisplayName"`
			SellerID          string  `json:"sellerId"`
			Amount            float64 `json:"amount"`
		} `json:"sellers"`
	}
	out := map[string]*stockResp{}
	partnerErrors := map[string]string{}
	partnerStale := map[string]bool{}
	for _, res := range results {
		key := strconv.Itoa(int(res.code))
		if res.err != nil {
			partnerErrors[key] = res.err.Error()
			slog.Warn("interbank-otc: partner /public-stock failed",
				"partner", res.code, "error", res.err)
			continue
		}
		if res.stale {
			partnerStale[key] = true
		}
		for _, ps := range res.stocks {
			entry, ok := out[ps.Stock.Ticker]
			if !ok {
				entry = &stockResp{Ticker: ps.Stock.Ticker}
				out[ps.Stock.Ticker] = entry
			}
			for _, seller := range ps.Sellers {
				entry.Sellers = append(entry.Sellers, struct {
					BankRoutingNumber int     `json:"bankRoutingNumber"`
					BankDisplayName   string  `json:"bankDisplayName"`
					SellerID          string  `json:"sellerId"`
					Amount            float64 `json:"amount"`
				}{
					BankRoutingNumber: int(seller.Seller.RoutingNumber),
					BankDisplayName:   bankNames[seller.Seller.RoutingNumber],
					SellerID:          seller.Seller.ID,
					Amount:            seller.Amount,
				})
			}
		}
	}

	stocks := make([]stockResp, 0, len(out))
	for _, e := range out {
		stocks = append(stocks, *e)
	}

	source := "cache"
	if live || h.stockCacheRepo == nil {
		source = "live"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stocks":        stocks,
		"count":         len(stocks),
		"partnerErrors": partnerErrors,
		"partnerStale":  partnerStale,
		"source":        source,
	})
}

// fanOutLive runs the original parallel /public-stock fan-out. Used
// for ?live=true and as a cold-start fallback when the cache is empty.
func (h *InterbankOtcHTTPHandler) fanOutLive(reqCtx context.Context, partners []interbank.PartnerBank) []partnerStockResult {
	ctx, cancel := context.WithTimeout(reqCtx, 15*time.Second)
	defer cancel()

	results := make([]partnerStockResult, len(partners))
	var wg sync.WaitGroup
	for i := range partners {
		i := i
		p := partners[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			stocks, err := h.client.FetchPublicStock(ctx, p.Code)
			results[i] = partnerStockResult{code: p.Code, stocks: stocks, err: err}
		}()
	}
	wg.Wait()
	return results
}

// readCached returns per-partner cached snapshots. Marks a result
// stale when the snapshot is older than publicStockStaleness. Returns
// fellBack=true when zero snapshots exist at all (cold start) — the
// caller falls back to a live fan-out in that case so the first user
// after process start doesn't see an empty catalogue.
func (h *InterbankOtcHTTPHandler) readCached(partners []interbank.PartnerBank) ([]partnerStockResult, bool) {
	rows, err := h.stockCacheRepo.List()
	if err != nil {
		slog.Error("interbank-otc: reading public-stock cache failed", "err", err)
		return nil, true
	}
	if len(rows) == 0 {
		return nil, true
	}
	byPartner := map[int]*models.RemotePublicStockSnapshot{}
	for i := range rows {
		byPartner[rows[i].PartnerRoutingNumber] = &rows[i]
	}
	now := time.Now().UTC()
	results := make([]partnerStockResult, 0, len(partners))
	for _, p := range partners {
		row := byPartner[int(p.Code)]
		if row == nil {
			results = append(results, partnerStockResult{
				code: p.Code,
				err:  fmt.Errorf("no cached snapshot yet"),
			})
			continue
		}
		stale := now.Sub(row.FetchedAt) > publicStockStaleness
		if row.LastError != "" && row.PayloadJSON == "" {
			results = append(results, partnerStockResult{
				code:  p.Code,
				err:   fmt.Errorf("partner cache error: %s", row.LastError),
				stale: stale,
			})
			continue
		}
		var stocks interbank.PublicStocksResponse
		if row.PayloadJSON != "" {
			if err := json.Unmarshal([]byte(row.PayloadJSON), &stocks); err != nil {
				results = append(results, partnerStockResult{
					code:  p.Code,
					err:   fmt.Errorf("decoding cached snapshot: %w", err),
					stale: stale,
				})
				continue
			}
		}
		results = append(results, partnerStockResult{
			code:   p.Code,
			stocks: stocks,
			stale:  stale,
		})
	}
	return results, false
}

// listNegotiations returns every cross-bank negotiation the caller is a
// party to. `?role=buyer|seller` filters to one side; `?includeClosed=true`
// includes negotiations whose isOngoing flag has flipped off.
func (h *InterbankOtcHTTPHandler) listNegotiations(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireTradingAccessHTTP(w, claims) {
		return
	}

	localID, ok := localParticipantIDFromClaims(claims)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can list interbank negotiations"})
		return
	}

	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
	switch role {
	case "buyer":
		role = models.InterbankNegotiationRoleBuyer
	case "seller":
		role = models.InterbankNegotiationRoleSeller
	case "":
		// keep blank — repo treats blank as both sides
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "role must be buyer|seller"})
		return
	}
	includeClosed := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("includeClosed")), "true")

	rows, err := h.negRepo.ListByLocalParticipant(localID, role, includeClosed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("listing negotiations: %v", err)})
		return
	}

	items := make([]map[string]interface{}, 0, len(rows))
	for i := range rows {
		items = append(items, negotiationRowToResponse(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"negotiations": items,
		"count":        len(items),
	})
}

// createNegotiation forwards a new OtcOffer to the seller's bank and
// persists the local copy with LocalRole=buyer. The caller's claims
// resolve to the buyer identity; the request body only carries the
// counterparty + terms.
func (h *InterbankOtcHTTPHandler) createNegotiation(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireTradingAccessHTTP(w, claims) {
		return
	}
	localID, ok := localParticipantIDFromClaims(claims)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can start interbank negotiations"})
		return
	}

	var body struct {
		SellerID struct {
			RoutingNumber int    `json:"routingNumber"`
			ID            string `json:"id"`
		} `json:"sellerId"`
		Stock struct {
			Ticker string `json:"ticker"`
		} `json:"stock"`
		SettlementDate string `json:"settlementDate"`
		PricePerUnit   struct {
			Currency string  `json:"currency"`
			Amount   float64 `json:"amount"`
		} `json:"pricePerUnit"`
		Premium struct {
			Currency string  `json:"currency"`
			Amount   float64 `json:"amount"`
		} `json:"premium"`
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})
		return
	}

	if body.SellerID.RoutingNumber == 0 || strings.TrimSpace(body.SellerID.ID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "sellerId.routingNumber and sellerId.id are required"})
		return
	}
	if body.SellerID.RoutingNumber == int(h.registry.OwnRoutingNumber()) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "sellerId.routingNumber must be a partner bank, not this bank — use /api/v1/otc for local negotiations"})
		return
	}
	if h.registry.Lookup(interbank.RoutingNumber(body.SellerID.RoutingNumber)) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": fmt.Sprintf("no partner bank registered for routing number %d", body.SellerID.RoutingNumber)})
		return
	}
	if strings.TrimSpace(body.Stock.Ticker) == "" || body.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "stock.ticker and a positive amount are required"})
		return
	}

	own := h.registry.OwnRoutingNumber()
	buyer := interbank.ForeignBankId{RoutingNumber: own, ID: localID}
	seller := interbank.ForeignBankId{
		RoutingNumber: interbank.RoutingNumber(body.SellerID.RoutingNumber),
		ID:            body.SellerID.ID,
	}

	offer := interbank.OtcOffer{
		Stock:          interbank.StockDescription{Ticker: body.Stock.Ticker},
		SettlementDate: body.SettlementDate,
		PricePerUnit: interbank.MonetaryValue{
			Currency: interbank.CurrencyCode(body.PricePerUnit.Currency),
			Amount:   body.PricePerUnit.Amount,
		},
		Premium: interbank.MonetaryValue{
			Currency: interbank.CurrencyCode(body.Premium.Currency),
			Amount:   body.Premium.Amount,
		},
		BuyerID:        buyer,
		SellerID:       seller,
		Amount:         body.Amount,
		LastModifiedBy: buyer,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	negID, err := h.client.CreateNegotiation(ctx, seller.RoutingNumber, offer)
	if err != nil {
		writeJSON(w, statusFromRemoteError(err), map[string]string{"message": fmt.Sprintf("partner POST /negotiations failed: %v", err)})
		return
	}

	row := &models.InterbankOtcNegotiation{
		NegotiationRoutingNumber:    int(negID.RoutingNumber),
		NegotiationID:               negID.ID,
		LocalRole:                   models.InterbankNegotiationRoleBuyer,
		CounterpartyRoutingNumber:   int(seller.RoutingNumber),
		BuyerRoutingNumber:          int(buyer.RoutingNumber),
		BuyerID:                     buyer.ID,
		SellerRoutingNumber:         int(seller.RoutingNumber),
		SellerID:                    seller.ID,
		StockTicker:                 body.Stock.Ticker,
		Amount:                      body.Amount,
		PricePerUnitCurrency:        body.PricePerUnit.Currency,
		PricePerUnitAmount:          body.PricePerUnit.Amount,
		PremiumCurrency:             body.Premium.Currency,
		PremiumAmount:               body.Premium.Amount,
		SettlementDate:              body.SettlementDate,
		LastModifiedByRoutingNumber: int(buyer.RoutingNumber),
		LastModifiedByID:            buyer.ID,
		IsOngoing:                   true,
	}
	if err := h.negRepo.Create(row); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("persisting local negotiation: %v", err)})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"negotiation": negotiationRowToResponse(row),
	})
}

// getNegotiation reads our local copy. We don't fan out to the partner
// here — if the local row is stale the partner's inbound PUT/DELETE
// will refresh it, and the frontend can poll.
func (h *InterbankOtcHTTPHandler) getNegotiation(w http.ResponseWriter, r *http.Request, routing interbank.RoutingNumber, id string) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireTradingAccessHTTP(w, claims) {
		return
	}
	localID, ok := localParticipantIDFromClaims(claims)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can read interbank negotiations"})
		return
	}

	row, err := h.negRepo.Get(int(routing), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("loading negotiation: %v", err)})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such negotiation"})
		return
	}
	if !localUserIsParty(row, localID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "you are not a party to that negotiation"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"negotiation": negotiationRowToResponse(row),
	})
}

// updateNegotiation forwards a counter-offer to the partner, then
// mirrors the new terms into the local row.
func (h *InterbankOtcHTTPHandler) updateNegotiation(w http.ResponseWriter, r *http.Request, routing interbank.RoutingNumber, id string) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireTradingAccessHTTP(w, claims) {
		return
	}
	localID, ok := localParticipantIDFromClaims(claims)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can update interbank negotiations"})
		return
	}

	row, err := h.negRepo.Get(int(routing), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("loading negotiation: %v", err)})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such negotiation"})
		return
	}
	if !localUserIsParty(row, localID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "you are not a party to that negotiation"})
		return
	}
	if !row.IsOngoing {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "negotiation is no longer ongoing"})
		return
	}

	var body struct {
		SettlementDate string `json:"settlementDate"`
		PricePerUnit   struct {
			Currency string  `json:"currency"`
			Amount   float64 `json:"amount"`
		} `json:"pricePerUnit"`
		Premium struct {
			Currency string  `json:"currency"`
			Amount   float64 `json:"amount"`
		} `json:"premium"`
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})
		return
	}
	if body.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "amount must be positive"})
		return
	}

	own := h.registry.OwnRoutingNumber()
	caller := interbank.ForeignBankId{RoutingNumber: own, ID: localID}

	offer := interbank.OtcOffer{
		Stock:          interbank.StockDescription{Ticker: row.StockTicker},
		SettlementDate: body.SettlementDate,
		PricePerUnit: interbank.MonetaryValue{
			Currency: interbank.CurrencyCode(body.PricePerUnit.Currency),
			Amount:   body.PricePerUnit.Amount,
		},
		Premium: interbank.MonetaryValue{
			Currency: interbank.CurrencyCode(body.Premium.Currency),
			Amount:   body.Premium.Amount,
		},
		BuyerID:        interbank.ForeignBankId{RoutingNumber: interbank.RoutingNumber(row.BuyerRoutingNumber), ID: row.BuyerID},
		SellerID:       interbank.ForeignBankId{RoutingNumber: interbank.RoutingNumber(row.SellerRoutingNumber), ID: row.SellerID},
		Amount:         body.Amount,
		LastModifiedBy: caller,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	partnerCode := interbank.RoutingNumber(row.CounterpartyRoutingNumber)
	if _, err := h.client.UpdateNegotiation(ctx, partnerCode, routing, id, offer); err != nil {
		writeJSON(w, statusFromRemoteError(err), map[string]string{"message": fmt.Sprintf("partner PUT /negotiations failed: %v", err)})
		return
	}

	if err := h.negRepo.UpdateTerms(
		int(routing), id,
		body.Amount,
		body.PricePerUnit.Currency, body.PricePerUnit.Amount,
		body.Premium.Currency, body.Premium.Amount,
		body.SettlementDate,
		int(caller.RoutingNumber), caller.ID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("updating local negotiation: %v", err)})
		return
	}

	row, err = h.negRepo.Get(int(routing), id)
	if err != nil || row == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "local negotiation disappeared after update"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"negotiation": negotiationRowToResponse(row),
	})
}

// closeNegotiation tells the partner to close the negotiation, then
// flips our local row to is_ongoing=false. Idempotent on already-closed
// rows.
func (h *InterbankOtcHTTPHandler) closeNegotiation(w http.ResponseWriter, r *http.Request, routing interbank.RoutingNumber, id string) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireTradingAccessHTTP(w, claims) {
		return
	}
	localID, ok := localParticipantIDFromClaims(claims)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can close interbank negotiations"})
		return
	}

	row, err := h.negRepo.Get(int(routing), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("loading negotiation: %v", err)})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such negotiation"})
		return
	}
	if !localUserIsParty(row, localID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "you are not a party to that negotiation"})
		return
	}

	if row.IsOngoing {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		partnerCode := interbank.RoutingNumber(row.CounterpartyRoutingNumber)
		if err := h.client.CloseNegotiation(ctx, partnerCode, routing, id); err != nil {
			writeJSON(w, statusFromRemoteError(err), map[string]string{"message": fmt.Sprintf("partner DELETE /negotiations failed: %v", err)})
			return
		}
		if err := h.negRepo.MarkClosed(int(routing), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("closing local negotiation: %v", err)})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// acceptNegotiation is the local-seller analogue of the partner-triggered
// GET /negotiations/{r}/{id}/accept in package interbank. It validates
// that the caller's local id matches the seller on the negotiation,
// then asks NegotiationsHandler to run the same dispatch (close
// locally, send NEW_TX, follow with COMMIT_TX on YES). The HTTP
// response carries the buyer-bank's vote on success or a structured
// error on dispatch / commit failure.
func (h *InterbankOtcHTTPHandler) acceptNegotiation(w http.ResponseWriter, r *http.Request, routing interbank.RoutingNumber, id string) {
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireTradingAccessHTTP(w, claims) {
		return
	}
	localID, ok := localParticipantIDFromClaims(claims)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "only client tokens can accept interbank negotiations"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	outcome, statusCode, errMsg := h.negsHandler.AcceptForLocalSeller(ctx, routing, id, localID)
	if statusCode != 0 {
		writeJSON(w, statusCode, map[string]string{"message": errMsg})
		return
	}

	switch {
	case outcome.DispatchErr != nil:
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"message": fmt.Sprintf("dispatching NEW_TX to buyer's bank failed: %v", outcome.DispatchErr),
		})
	case outcome.Vote != nil && outcome.Vote.Vote != interbank.VoteYes:
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"message": "buyer's bank refused the acceptance — negotiation has been reopened",
			"vote":    outcome.Vote,
		})
	case outcome.CommitErr != nil:
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"message": fmt.Sprintf("buyer voted YES but COMMIT_TX failed; operator action required: %v", outcome.CommitErr),
			"vote":    outcome.Vote,
		})
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"vote": outcome.Vote,
		})
	}
}

func negotiationRowToResponse(row *models.InterbankOtcNegotiation) map[string]interface{} {
	return map[string]interface{}{
		"negotiationRoutingNumber": row.NegotiationRoutingNumber,
		"negotiationId":            row.NegotiationID,
		"localRole":                row.LocalRole,
		"counterpartyRoutingNumber": row.CounterpartyRoutingNumber,
		"buyerId":                  map[string]interface{}{"routingNumber": row.BuyerRoutingNumber, "id": row.BuyerID},
		"sellerId":                 map[string]interface{}{"routingNumber": row.SellerRoutingNumber, "id": row.SellerID},
		"stock":                    map[string]string{"ticker": row.StockTicker},
		"amount":                   row.Amount,
		"pricePerUnit":             map[string]interface{}{"currency": row.PricePerUnitCurrency, "amount": row.PricePerUnitAmount},
		"premium":                  map[string]interface{}{"currency": row.PremiumCurrency, "amount": row.PremiumAmount},
		"settlementDate":           row.SettlementDate,
		"lastModifiedBy":           map[string]interface{}{"routingNumber": row.LastModifiedByRoutingNumber, "id": row.LastModifiedByID},
		"isOngoing":                row.IsOngoing,
		"createdAt":                row.CreatedAt,
		"updatedAt":                row.UpdatedAt,
	}
}

// localParticipantIDFromClaims returns the local participant id we
// encode into ForeignBankId.ID for a JWT-authenticated client caller.
// Employee/bank tokens are rejected at this entry point — interbank
// OTC is a client-initiated flow.
func localParticipantIDFromClaims(claims *util.Claims) (string, bool) {
	if claims.TokenSource != "client" || claims.ClientID == 0 {
		return "", false
	}
	return interbank.EncodeLocalParticipantID(interbank.LocalParticipantClient, claims.ClientID), true
}

// localUserIsParty returns true if the encoded local id is the buyer
// or seller on the local side of the negotiation row. The remote side
// would carry the partner's opaque id and won't match.
func localUserIsParty(row *models.InterbankOtcNegotiation, localID string) bool {
	switch row.LocalRole {
	case models.InterbankNegotiationRoleBuyer:
		return row.BuyerID == localID
	case models.InterbankNegotiationRoleSeller:
		return row.SellerID == localID
	default:
		return false
	}
}

func parseRoutingAndID(routingStr, id string) (interbank.RoutingNumber, string, bool) {
	if strings.TrimSpace(routingStr) == "" || strings.TrimSpace(id) == "" {
		return 0, "", false
	}
	n, err := strconv.Atoi(routingStr)
	if err != nil {
		return 0, "", false
	}
	return interbank.RoutingNumber(n), id, true
}

// statusFromRemoteError maps an interbank.RemoteError onto the
// appropriate HTTP status code to bubble out to our frontend. We
// preserve 4xx codes (partner is telling us our request was malformed
// or unauthorized in their view) and collapse 5xx to 502 Bad Gateway.
// Any non-RemoteError error becomes 502 too — that covers timeouts,
// connection failures, etc.
func statusFromRemoteError(err error) int {
	var rerr *interbank.RemoteError
	if errors.As(err, &rerr) {
		if rerr.StatusCode >= 400 && rerr.StatusCode < 500 {
			return rerr.StatusCode
		}
	}
	return http.StatusBadGateway
}
