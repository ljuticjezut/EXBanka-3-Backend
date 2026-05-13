package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/interbank"
	"github.com/RAF-SI-2025/EXBanka-3-Backend/exchange-service/internal/repository"
)

// PublicStockCacheRunner is the cron half of BANKA-BE-5. Every tick it
// fans out across the registry, fetches each partner's /public-stock,
// and persists the response into remote_public_stock_snapshots. The
// local OTC discovery handler reads from that table — so a slow or
// down partner doesn't block a user opening the discovery page.
//
// On per-partner failure we record the error against the existing
// snapshot (keeping the previous payload around) so the handler can
// still show stale-but-good data with a `stale=true` flag.
type PublicStockCacheRunner struct {
	registry *interbank.Registry
	client   *interbank.Client
	repo     *repository.RemotePublicStockRepository

	// perPartnerTimeout caps each outbound /public-stock call. Total
	// run time is bounded by perPartnerTimeout * partner count, which
	// keeps the cron from overlapping itself even with several slow
	// partners.
	perPartnerTimeout time.Duration
}

func NewPublicStockCacheRunner(
	registry *interbank.Registry,
	client *interbank.Client,
	repo *repository.RemotePublicStockRepository,
) *PublicStockCacheRunner {
	return &PublicStockCacheRunner{
		registry:          registry,
		client:            client,
		repo:              repo,
		perPartnerTimeout: 8 * time.Second,
	}
}

// Run executes one cache refresh. Per-partner errors are logged + saved
// to the snapshot row; the run continues so one slow partner doesn't
// block fresh data for others. Fan-out is parallel because partners
// are independent.
func (r *PublicStockCacheRunner) Run() {
	partners := r.registry.All()
	if len(partners) == 0 {
		return
	}

	var wg sync.WaitGroup
	for i := range partners {
		p := partners[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.refreshOne(p.Code)
		}()
	}
	wg.Wait()
}

func (r *PublicStockCacheRunner) refreshOne(partner interbank.RoutingNumber) {
	ctx, cancel := context.WithTimeout(context.Background(), r.perPartnerTimeout)
	defer cancel()

	stocks, err := r.client.FetchPublicStock(ctx, partner)
	if err != nil {
		if upErr := r.repo.UpsertError(int(partner), err.Error()); upErr != nil {
			slog.Error("public-stock cache: recording partner error failed",
				"err", upErr, "partner", partner, "fetch_err", err)
			return
		}
		slog.Info("public-stock cache: partner refresh failed, kept stale snapshot",
			"partner", partner, "err", err)
		return
	}

	payload, mErr := json.Marshal(stocks)
	if mErr != nil {
		slog.Error("public-stock cache: marshalling response failed",
			"err", mErr, "partner", partner)
		return
	}

	if err := r.repo.UpsertPayload(int(partner), string(payload)); err != nil {
		slog.Error("public-stock cache: writing snapshot failed",
			"err", err, "partner", partner)
		return
	}
}
