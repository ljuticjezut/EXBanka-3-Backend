package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/config"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/models"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/service"
)

type MarketHTTPHandler struct {
	cfg *config.Config
	svc *service.MarketService
}

func NewMarketHTTPHandler(cfg *config.Config, svc *service.MarketService) *MarketHTTPHandler {
	return &MarketHTTPHandler{cfg: cfg, svc: svc}
}

func (h *MarketHTTPHandler) ListExchanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireMarketReadAccessHTTP(w, claims) {
		return
	}

	exchanges, err := h.svc.ListExchanges()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to load exchanges"})
		return
	}
	items := make([]exchangeResponse, 0, len(exchanges))
	for _, exchange := range exchanges {
		items = append(items, exchangeToResponse(exchange))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exchanges": items,
		"count":     len(items),
	})
}

func (h *MarketHTTPHandler) ListListings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireMarketReadAccessHTTP(w, claims) {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	listings, err := h.svc.ListListings(query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to load listings"})
		return
	}
	items := make([]listingResponse, 0, len(listings))
	for _, listing := range listings {
		items = append(items, listingToResponse(listing))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"listings": items,
		"count":    len(items),
		"query":    query,
	})
}

func (h *MarketHTTPHandler) ListingRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireMarketReadAccessHTTP(w, claims) {
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/listings/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(path, "/")
	ticker := parts[0]

	switch {
	case len(parts) == 1:
		listing, err := h.svc.GetListing(ticker)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"listing": listingToResponse(*listing),
		})
	case len(parts) == 2 && parts[1] == "history":
		history, err := h.svc.GetListingHistory(ticker)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": err.Error()})
			return
		}
		items := make([]historyResponse, 0, len(history))
		for _, item := range history {
			items = append(items, historyToResponse(item))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ticker":  strings.ToUpper(ticker),
			"history": items,
		})
	default:
		http.NotFound(w, r)
	}
}

func (h *MarketHTTPHandler) GetPortfolio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims, ok := requireAuthenticatedHTTP(w, r, h.cfg)
	if !ok {
		return
	}
	if !requireMarketReadAccessHTTP(w, claims) {
		return
	}

	ownerID := claims.ClientID
	ownerType := models.PortfolioOwnerTypeClient
	if claims.TokenSource == "employee" {
		ownerID = claims.EmployeeID
		ownerType = models.PortfolioOwnerTypeEmployee
	}

	portfolio, err := h.svc.GetPortfolio(ownerID, ownerType)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"portfolio": portfolioToResponse(*portfolio),
	})
}

type exchangeResponse struct {
	Name         string `json:"name"`
	Acronym      string `json:"acronym"`
	MICCode      string `json:"micCode"`
	Polity       string `json:"polity"`
	Currency     string `json:"currency"`
	Timezone     string `json:"timezone"`
	WorkingHours string `json:"workingHours"`
	Enabled      bool   `json:"enabled"`
}

type exchangeSummaryResponse struct {
	Name     string `json:"name"`
	Acronym  string `json:"acronym"`
	MICCode  string `json:"micCode"`
	Currency string `json:"currency"`
}

type listingResponse struct {
	Ticker      string                  `json:"ticker"`
	Name        string                  `json:"name"`
	Exchange    exchangeSummaryResponse `json:"exchange"`
	LastRefresh string                  `json:"lastRefresh"`
	Price       float64                 `json:"price"`
	Ask         float64                 `json:"ask"`
	Bid         float64                 `json:"bid"`
	Volume      int64                   `json:"volume"`
	Type        string                  `json:"type"`
}

type historyResponse struct {
	Date   string  `json:"date"`
	Price  float64 `json:"price"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Change float64 `json:"change"`
	Volume int64   `json:"volume"`
}

type portfolioItemResponse struct {
	Ticker       string  `json:"ticker"`
	Name         string  `json:"name"`
	Exchange     string  `json:"exchange"`
	Currency     string  `json:"currency"`
	Quantity     float64 `json:"quantity"`
	AveragePrice float64 `json:"averagePrice"`
	CurrentPrice float64 `json:"currentPrice"`
	MarketValue  float64 `json:"marketValue"`
	PnL          float64 `json:"pnl"`
	PnLPercent   float64 `json:"pnlPercent"`
}

type portfolioResponse struct {
	OwnerID           string                  `json:"ownerId"`
	OwnerType         string                  `json:"ownerType"`
	GeneratedAt       string                  `json:"generatedAt"`
	ValuationAsOf     string                  `json:"valuationAsOf"`
	ValuationCurrency string                  `json:"valuationCurrency"`
	EstimatedValue    float64                 `json:"estimatedValue"`
	UnrealizedPnL     float64                 `json:"unrealizedPnL"`
	PositionCount     int                     `json:"positionCount"`
	ReadOnly          bool                    `json:"readOnly"`
	ModelType         string                  `json:"modelType"`
	PositionSource    string                  `json:"positionSource"`
	PricingSource     string                  `json:"pricingSource"`
	Items             []portfolioItemResponse `json:"items"`
}

func exchangeToResponse(exchange models.Exchange) exchangeResponse {
	return exchangeResponse{
		Name:         exchange.Name,
		Acronym:      exchange.Acronym,
		MICCode:      exchange.MICCode,
		Polity:       exchange.Polity,
		Currency:     exchange.Currency,
		Timezone:     exchange.Timezone,
		WorkingHours: exchange.WorkingHours,
		Enabled:      exchange.Enabled,
	}
}

func exchangeSummaryToResponse(exchange models.ExchangeSummary) exchangeSummaryResponse {
	return exchangeSummaryResponse{
		Name:     exchange.Name,
		Acronym:  exchange.Acronym,
		MICCode:  exchange.MICCode,
		Currency: exchange.Currency,
	}
}

func listingToResponse(listing models.Listing) listingResponse {
	return listingResponse{
		Ticker:      listing.Ticker,
		Name:        listing.Name,
		Exchange:    exchangeSummaryToResponse(listing.Exchange),
		LastRefresh: listing.LastRefresh.UTC().Format(time.RFC3339),
		Price:       listing.Price,
		Ask:         listing.Ask,
		Bid:         listing.Bid,
		Volume:      listing.Volume,
		Type:        string(listing.Type),
	}
}

func historyToResponse(item models.ListingDailyPriceInfo) historyResponse {
	return historyResponse{
		Date:   item.Date.Format("2006-01-02"),
		Price:  item.Price,
		High:   item.High,
		Low:    item.Low,
		Change: item.Change,
		Volume: item.Volume,
	}
}

func portfolioToResponse(portfolio models.Portfolio) portfolioResponse {
	items := make([]portfolioItemResponse, 0, len(portfolio.Items))
	for _, item := range portfolio.Items {
		items = append(items, portfolioItemResponse{
			Ticker:       item.Ticker,
			Name:         item.Name,
			Exchange:     item.Exchange,
			Currency:     item.Currency,
			Quantity:     item.Quantity,
			AveragePrice: item.AveragePrice,
			CurrentPrice: item.CurrentPrice,
			MarketValue:  item.MarketValue,
			PnL:          item.PnL,
			PnLPercent:   item.PnLPercent,
		})
	}

	return portfolioResponse{
		OwnerID:           strconv.FormatUint(uint64(portfolio.OwnerID), 10),
		OwnerType:         string(portfolio.OwnerType),
		GeneratedAt:       portfolio.GeneratedAt.UTC().Format(time.RFC3339),
		ValuationAsOf:     portfolio.ValuationAsOf.UTC().Format(time.RFC3339),
		ValuationCurrency: portfolio.ValuationCurrency,
		EstimatedValue:    portfolio.EstimatedValue,
		UnrealizedPnL:     portfolio.UnrealizedPnL,
		PositionCount:     portfolio.PositionCount,
		ReadOnly:          portfolio.ReadOnly,
		ModelType:         string(portfolio.ModelType),
		PositionSource:    portfolio.PositionSource,
		PricingSource:     portfolio.PricingSource,
		Items:             items,
	}
}
