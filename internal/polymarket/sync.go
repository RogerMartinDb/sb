package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sportID         = "basketball"
	sportName       = "Basketball"
	competitionID   = "nba"
	competitionName = "NBA"
	syncInterval    = 5 * time.Minute
	lookAheadDays   = 14
)

// allowedTypes maps Polymarket sportsMarketType → our market_type enum.
// Only full-game markets are included; first-half variants are excluded.
var allowedTypes = map[string]string{
	"moneyline": "ML",
	"spreads":   "SPREAD",
	"totals":    "TOTAL",
}

// Syncer polls Polymarket for upcoming NBA games and upserts them into the
// catalog database (sports / competitions / events / markets / selections).
// When a Kafka producer is set, it publishes price.update events to
// market-data.normalised after upserting each market's selections, triggering
// the odds pipeline.
type Syncer struct {
	client   *Client
	db       *pgxpool.Pool
	producer sarama.SyncProducer // nil = no Kafka publishing
	logger   *slog.Logger
}

func NewSyncer(db *pgxpool.Pool, producer sarama.SyncProducer, logger *slog.Logger) *Syncer {
	return &Syncer{
		client:   NewClient(logger),
		db:       db,
		producer: producer,
		logger:   logger,
	}
}

// Run starts the polling loop, syncing immediately then every syncInterval.
func (s *Syncer) Run(ctx context.Context) {
	s.logger.Info("polymarket syncer starting")
	s.sync(ctx)

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sync(ctx)
		}
	}
}

func (s *Syncer) sync(ctx context.Context) {
	events, err := s.client.FetchNBAEvents(ctx)
	if err != nil {
		s.logger.Error("polymarket: fetch events failed", "err", err)
		return
	}

	now := time.Now().UTC()
	cutoff := now.Add(lookAheadDays * 24 * time.Hour)

	if err := s.ensureNBA(ctx); err != nil {
		s.logger.Error("polymarket: ensure sport/competition failed", "err", err)
		return
	}

	synced := 0
	skipped := 0
	for _, ev := range events {
		if !strings.HasPrefix(ev.Slug, "nba-") || ev.Closed || !ev.Active {
			s.logger.Debug("polymarket: skipping event",
				"slug", ev.Slug, "active", ev.Active, "closed", ev.Closed)
			skipped++
			continue
		}
		// Only sync events that have sports markets (skip futures/props events).
		if !hasSportsMarkets(ev.Markets) {
			s.logger.Debug("polymarket: skipping non-game event", "slug", ev.Slug)
			skipped++
			continue
		}
		if err := s.syncEvent(ctx, ev, now, cutoff); err != nil {
			s.logger.Error("polymarket: sync event failed", "slug", ev.Slug, "err", err)
			continue
		}
		synced++
	}
	s.logger.Info("polymarket: sync complete", "events_synced", synced, "events_skipped", skipped)
}

func hasSportsMarkets(markets []Market) bool {
	for _, m := range markets {
		if _, ok := allowedTypes[m.SportsMarketType]; ok {
			return true
		}
	}
	return false
}

// ensureNBA creates the Basketball sport and NBA competition rows if absent.
func (s *Syncer) ensureNBA(ctx context.Context) error {
	if _, err := s.db.Exec(ctx,
		`INSERT INTO sports (sport_id, name) VALUES ($1, $2) ON CONFLICT (sport_id) DO NOTHING`,
		sportID, sportName,
	); err != nil {
		return fmt.Errorf("upsert sport: %w", err)
	}
	if _, err := s.db.Exec(ctx,
		`INSERT INTO competitions (competition_id, sport_id, name, country)
		 VALUES ($1, $2, $3, 'US')
		 ON CONFLICT (competition_id) DO NOTHING`,
		competitionID, sportID, competitionName,
	); err != nil {
		return fmt.Errorf("upsert competition: %w", err)
	}
	return nil
}

func (s *Syncer) syncEvent(ctx context.Context, ev Event, now, cutoff time.Time) error {
	// Derive the game start time from any market that carries it.
	startTime, ok := gameStartTime(ev.Markets)
	if !ok {
		s.logger.Warn("polymarket: no game start time found", "slug", ev.Slug)
		return nil
	}
	// Allow live games (started up to 6 hours ago) and future games within the look-ahead window.
	liveWindow := now.Add(-6 * time.Hour)
	if startTime.Before(liveWindow) || startTime.After(cutoff) {
		s.logger.Debug("polymarket: event outside time window",
			"slug", ev.Slug, "starts_at", startTime,
			"now", now, "cutoff", cutoff)
		return nil
	}

	// Determine event status: LIVE if game has started, SCHEDULED otherwise.
	eventStatus := "SCHEDULED"
	if !startTime.After(now) {
		eventStatus = "LIVE"
	}

	s.logger.Info("polymarket: syncing event",
		"slug", ev.Slug, "title", ev.Title, "starts_at", startTime,
		"status", eventStatus, "total_markets", len(ev.Markets))

	eventName := strings.ReplaceAll(ev.Title, " vs. ", " @ ")
	if _, err := s.db.Exec(ctx, `
		INSERT INTO events (event_id, competition_id, name, starts_at, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_id) DO UPDATE
		    SET name       = EXCLUDED.name,
		        starts_at  = EXCLUDED.starts_at,
		        status     = EXCLUDED.status,
		        updated_at = NOW()`,
		ev.ID, competitionID, eventName, startTime, eventStatus,
	); err != nil {
		return fmt.Errorf("upsert event: %w", err)
	}

	// Filter to allowed market types only.
	var allowed []Market
	for _, m := range ev.Markets {
		if _, ok := allowedTypes[m.SportsMarketType]; ok && !m.Closed && m.Active {
			allowed = append(allowed, m)
		}
	}

	// Compute is_main flags.
	isMainMap := computeIsMain(allowed)

	marketsSynced := 0
	for _, m := range allowed {
		ourType := allowedTypes[m.SportsMarketType]
		isMain := isMainMap[m.ConditionID]
		if err := s.syncMarket(ctx, ev.ID, m, ourType, startTime, isMain); err != nil {
			s.logger.Warn("polymarket: sync market failed",
				"condition_id", m.ConditionID, "type", ourType, "err", err)
			continue
		}
		marketsSynced++
	}

	s.logger.Info("polymarket: event synced",
		"slug", ev.Slug, "markets_synced", marketsSynced,
		"markets_allowed", len(allowed))

	return nil
}

func (s *Syncer) syncMarket(ctx context.Context, eventID string, m Market, marketType string, closesAt time.Time, isMain bool) error {
	outcomes, err := m.ParseOutcomes()
	if err != nil {
		return fmt.Errorf("parse outcomes: %w", err)
	}
	prices, err := m.ParseOutcomePrices()
	if err != nil {
		return fmt.Errorf("parse prices: %w", err)
	}

	if len(outcomes) != 2 || len(prices) != 2 {
		return fmt.Errorf("unexpected market shape: outcomes=%d prices=%d",
			len(outcomes), len(prices))
	}

	targetValue := absLine(m.Line)
	marketName := m.Name()

	s.logger.Debug("polymarket: upserting market",
		"condition_id", m.ConditionID, "type", marketType,
		"name", marketName, "target", targetValue,
		"is_main", isMain, "outcomes", outcomes, "prices", prices)

	if _, err := s.db.Exec(ctx, `
		INSERT INTO markets
		    (market_id, event_id, name, status, market_type, target_value, is_main, closes_at)
		VALUES ($1, $2, $3, 'OPEN', $4, $5, $6, $7)
		ON CONFLICT (market_id) DO UPDATE
		    SET name         = EXCLUDED.name,
		        market_type  = EXCLUDED.market_type,
		        target_value = EXCLUDED.target_value,
		        is_main      = EXCLUDED.is_main,
		        updated_at   = NOW()`,
		m.ConditionID, eventID, marketName, marketType, targetValue, isMain, closesAt,
	); err != nil {
		return fmt.Errorf("upsert market: %w", err)
	}

	sels := buildSelections(marketType, outcomes, prices, m.Line)
	for i, sel := range sels {
		selID := fmt.Sprintf("%s-%d", m.ConditionID, i)
		if _, err := s.db.Exec(ctx, `
			INSERT INTO selections
			    (selection_id, market_id, name, active, target_value, feed_probability)
			VALUES ($1, $2, $3, true, $4, $5)
			ON CONFLICT (selection_id) DO UPDATE
			    SET name             = EXCLUDED.name,
			        target_value     = EXCLUDED.target_value,
			        feed_probability = EXCLUDED.feed_probability`,
			selID, m.ConditionID, sel.name, sel.targetValue, sel.prob,
		); err != nil {
			return fmt.Errorf("upsert selection %d: %w", i, err)
		}
	}

	// Publish price.update to market-data.normalised so odds management
	// computes vigged odds and populates the Redis cache.
	if s.producer != nil {
		event := struct {
			EventType   string `json:"event_type"`
			MarketID    string `json:"market_id"`
			PublishedAt string `json:"published_at"`
		}{
			EventType:   "price.update",
			MarketID:    m.ConditionID,
			PublishedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal price.update: %w", err)
		}
		if _, _, err := s.producer.SendMessage(&sarama.ProducerMessage{
			Topic: "market-data.normalised",
			Key:   sarama.StringEncoder(m.ConditionID),
			Value: sarama.ByteEncoder(data),
		}); err != nil {
			s.logger.Warn("polymarket: failed to publish price.update",
				"market_id", m.ConditionID, "err", err)
		}
	}

	return nil
}

type selRow struct {
	name        string
	targetValue float64
	prob        float64
}

func buildSelections(marketType string, outcomes, prices []string, line *float64) []selRow {
	p0, _ := strconv.ParseFloat(prices[0], 64)
	p1, _ := strconv.ParseFloat(prices[1], 64)

	switch marketType {
	case "ML":
		return []selRow{
			{name: outcomes[0], targetValue: 0, prob: p0},
			{name: outcomes[1], targetValue: 0, prob: p1},
		}

	case "SPREAD":
		abs := absLine(line)
		if derefLine(line) <= 0 {
			return []selRow{
				{name: outcomes[1], targetValue: abs, prob: p1},
				{name: outcomes[0], targetValue: -abs, prob: p0},
			}
		}
		return []selRow{
			{name: outcomes[0], targetValue: abs, prob: p0},
			{name: outcomes[1], targetValue: -abs, prob: p1},
		}

	case "TOTAL":
		l := absLine(line)
		return []selRow{
			{name: outcomes[0], targetValue: l, prob: p0},
			{name: outcomes[1], targetValue: -l, prob: p1},
		}

	default:
		return []selRow{
			{name: outcomes[0], targetValue: 0, prob: p0},
			{name: outcomes[1], targetValue: 0, prob: p1},
		}
	}
}

// computeIsMain determines which markets in an event are "main" lines.
// ML → always main.
// SPREAD/TOTAL → the line whose max outcome probability is closest to 0.5.
func computeIsMain(markets []Market) map[string]bool {
	result := make(map[string]bool)

	byType := make(map[string][]Market)
	for _, m := range markets {
		ourType := allowedTypes[m.SportsMarketType]
		byType[ourType] = append(byType[ourType], m)
	}

	for _, m := range byType["ML"] {
		result[m.ConditionID] = true
	}

	for _, mtype := range []string{"SPREAD", "TOTAL"} {
		group := byType[mtype]
		if len(group) == 0 {
			continue
		}

		bestID := ""
		bestDist := math.MaxFloat64

		for _, m := range group {
			prices, err := m.ParseOutcomePrices()
			if err != nil || len(prices) < 2 {
				continue
			}
			maxProb := 0.0
			for _, ps := range prices {
				p, _ := strconv.ParseFloat(ps, 64)
				if p > maxProb {
					maxProb = p
				}
			}
			dist := math.Abs(maxProb - 0.5)
			if dist < bestDist {
				bestDist = dist
				bestID = m.ConditionID
			}
		}

		if bestID != "" {
			result[bestID] = true
		}
	}

	return result
}

// gameStartTime finds the first parseable GameStartTime from a slice of markets.
func gameStartTime(markets []Market) (time.Time, bool) {
	for _, m := range markets {
		if m.GameStartTime == "" {
			continue
		}
		for _, layout := range []string{
			"2006-01-02 15:04:05-07",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05-07:00",
		} {
			if t, err := time.Parse(layout, m.GameStartTime); err == nil {
				return t.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

func absLine(line *float64) float64 {
	if line == nil {
		return 0
	}
	return math.Abs(*line)
}

func derefLine(line *float64) float64 {
	if line == nil {
		return 0
	}
	return *line
}
