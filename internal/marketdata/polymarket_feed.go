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

// PolymarketFeed polls the Polymarket Gamma API for a given sport competition.
// providerID is used by CompositeNormaliser to select the correct normaliser.
type PolymarketFeed struct {
	client        *polymarket.Client
	eventMatcher  *EventMatcher  // optional; used by NBA score feed for team→event mapping
	tokenRegistry *TokenRegistry // optional; populated with CLOB token IDs for WS price feed
	logger        *slog.Logger
	providerID    string
	slugPrefix    string
	tagID         int
	binaryMode    bool // when true, accept events without sportsMarketType (yes/no binary markets)
}

// NewPolymarketFeed creates a feed for NBA events (tag 745, slug prefix "nba-").
func NewPolymarketFeed(eventMatcher *EventMatcher, tokenRegistry *TokenRegistry, logger *slog.Logger) *PolymarketFeed {
	return &PolymarketFeed{
		client:        polymarket.NewClient(logger),
		eventMatcher:  eventMatcher,
		tokenRegistry: tokenRegistry,
		logger:        logger,
		providerID:    "polymarket-nba",
		slugPrefix:    "nba-",
		tagID:         polymarket.NBATagID,
	}
}

// NewIranFeed creates a feed for Iran political events (tag 78, binary mode).
func NewIranFeed(logger *slog.Logger) *PolymarketFeed {
	return &PolymarketFeed{
		client:     polymarket.NewClient(logger),
		logger:     logger,
		providerID: "polymarket-iran",
		tagID:      polymarket.IranTagID,
		binaryMode: true,
	}
}

// NewNCAABFeed creates a feed for NCAAB events (tag 28, slug prefix "cbb-").
func NewNCAABFeed(eventMatcher *EventMatcher, tokenRegistry *TokenRegistry, logger *slog.Logger) *PolymarketFeed {
	return &PolymarketFeed{
		client:        polymarket.NewClient(logger),
		eventMatcher:  eventMatcher,
		tokenRegistry: tokenRegistry,
		logger:        logger,
		providerID:    "polymarket-ncaab",
		slugPrefix:    "cbb-",
		tagID:         polymarket.NCAABTagID,
	}
}

func (f *PolymarketFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent, 64)
	go func() {
		defer close(ch)
		f.logger.Info("polymarket feed: starting", "provider", f.providerID)
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
	events, err := f.client.FetchEvents(ctx, f.tagID)
	if err != nil {
		f.logger.Error("polymarket feed: fetch failed", "provider", f.providerID, "err", err)
		return
	}

	now := time.Now().UTC()
	sent := 0
	for _, ev := range events {
		if ev.Closed || !ev.Active {
			continue
		}
		if f.slugPrefix != "" && !strings.HasPrefix(ev.Slug, f.slugPrefix) {
			continue
		}
		if f.binaryMode {
			if !hasBinaryMarkets(ev.Markets) {
				continue
			}
		} else if !hasSportsMarkets(ev.Markets) {
			continue
		}

		data, err := json.Marshal(ev)
		if err != nil {
			f.logger.Warn("polymarket feed: marshal event failed", "slug", ev.Slug, "err", err)
			continue
		}

		// Register in event matcher so score feeds can correlate team names to event IDs.
		if f.eventMatcher != nil {
			eventName := strings.ReplaceAll(ev.Title, " vs. ", " @ ")
			f.eventMatcher.Register(ev.ID, eventName)
		}

		// Register CLOB token IDs for the WS price feed.
		if f.tokenRegistry != nil {
			for _, m := range ev.Markets {
				tokenIDs, err := m.ParseClobTokenIDs()
				if err != nil || len(tokenIDs) == 0 {
					continue
				}
				var entries []TokenEntry
				for i, tid := range tokenIDs {
					entries = append(entries, TokenEntry{
						TokenID:     tid,
						ConditionID: m.ConditionID,
						SelIndex:    i,
						ProviderID:  f.providerID,
					})
				}
				f.tokenRegistry.Register(entries)
			}
		}

		select {
		case ch <- RawProviderEvent{
			ProviderID: f.providerID,
			Data:       data,
			ReceivedAt: now,
		}:
			sent++
		case <-ctx.Done():
			return
		}
	}
	f.logger.Info("polymarket feed: poll complete", "provider", f.providerID, "events_sent", sent, "events_total", len(events))
}

func hasSportsMarkets(markets []polymarket.Market) bool {
	for _, m := range markets {
		if _, ok := allowedTypes[m.SportsMarketType]; ok {
			return true
		}
	}
	return false
}

func hasBinaryMarkets(markets []polymarket.Market) bool {
	for _, m := range markets {
		if !m.Closed && m.Active && m.SportsMarketType == "" {
			return true
		}
	}
	return false
}
