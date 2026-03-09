package marketdata

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/sportsbook/sb/internal/polymarket"
)

const polymarketSyncInterval = 5 * time.Minute

// PolymarketFeed implements ProviderFeed by polling the Polymarket Gamma API
// for NBA game events. Each RawProviderEvent contains one polymarket.Event
// (a full NBA game with all its markets).
type PolymarketFeed struct {
	client       *polymarket.Client
	eventMatcher *EventMatcher // shared mapping for NBA score matching
	logger       *slog.Logger
}

func NewPolymarketFeed(eventMatcher *EventMatcher, logger *slog.Logger) *PolymarketFeed {
	return &PolymarketFeed{
		client:       polymarket.NewClient(logger),
		eventMatcher: eventMatcher,
		logger:       logger,
	}
}

func (f *PolymarketFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent, 64)
	go func() {
		defer close(ch)
		f.logger.Info("polymarket feed: starting")
		f.poll(ctx, ch)

		ticker := time.NewTicker(polymarketSyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f.poll(ctx, ch)
			}
		}
	}()
	return ch, nil
}

func (f *PolymarketFeed) poll(ctx context.Context, ch chan<- RawProviderEvent) {
	events, err := f.client.FetchNBAEvents(ctx)
	if err != nil {
		f.logger.Error("polymarket feed: fetch failed", "err", err)
		return
	}

	now := time.Now().UTC()
	sent := 0
	for _, ev := range events {
		if !strings.HasPrefix(ev.Slug, "nba-") || ev.Closed || !ev.Active {
			continue
		}
		if !hasSportsMarkets(ev.Markets) {
			continue
		}

		data, err := json.Marshal(ev)
		if err != nil {
			f.logger.Warn("polymarket feed: marshal event failed", "slug", ev.Slug, "err", err)
			continue
		}

		// Register event in the shared matcher so the NBA score feed can
		// map team names to event IDs.
		eventName := strings.ReplaceAll(ev.Title, " vs. ", " @ ")
		f.eventMatcher.Register(ev.ID, eventName)

		select {
		case ch <- RawProviderEvent{
			ProviderID: "polymarket",
			Data:       data,
			ReceivedAt: now,
		}:
			sent++
		case <-ctx.Done():
			return
		}
	}
	f.logger.Info("polymarket feed: poll complete", "events_sent", sent, "events_total", len(events))
}

func hasSportsMarkets(markets []polymarket.Market) bool {
	for _, m := range markets {
		if _, ok := allowedTypes[m.SportsMarketType]; ok {
			return true
		}
	}
	return false
}
