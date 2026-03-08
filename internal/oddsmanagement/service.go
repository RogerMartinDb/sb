// Package oddsmanagement implements the Odds Management Service: subscribes to
// normalised feed events and bet.placed events, computes offered odds, and
// publishes odds.updated.
package oddsmanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OddsManagementConsumerGroup is the group ID used when consuming bet.placed.
// This is the group whose lag the Bet Acceptance Service monitors.
// The group MUST commit offsets only AFTER writing new odds to the DB AND
// publishing odds.updated — pre-committing defeats the lag check.
const OddsManagementConsumerGroup = "odds-management-cg"

// OddsUpdatedEvent is the payload published to odds.updated.
type OddsUpdatedEvent struct {
	MarketID          string  `json:"market_id"`
	SelectionID       string  `json:"selection_id"`
	Decimal           float64 `json:"decimal"`  // e.g. 2.50
	American          int     `json:"american"` // e.g. +150 or -200
	SourceEventOffset int64   `json:"src_offset"` // offset of the triggering event
	ComputedAt        string  `json:"computed_at"`
}

// Service is the Odds Management Service.
type Service struct {
	db        *pgxpool.Pool // odds DB
	catalogDB *pgxpool.Pool // catalog DB (read-only, for feed probabilities)
	producer  sarama.SyncProducer
	logger    *slog.Logger
}

// NewService creates the service with a Kafka producer for odds.updated.
// catalogDB is a read-only connection to the catalog database for fetching
// selection feed probabilities.
func NewService(db *pgxpool.Pool, catalogDB *pgxpool.Pool, brokers []string, logger *slog.Logger) (*Service, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Idempotent = true
	cfg.Net.MaxOpenRequests = 1
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("odds: create producer: %w", err)
	}
	logger.Info("odds management: producer connected", "brokers", brokers)
	return &Service{db: db, catalogDB: catalogDB, producer: producer, logger: logger}, nil
}

// RunConsumer starts consuming market-data.normalised and bet.placed topics.
// Offsets are committed ONLY after the odds are written to the DB AND
// odds.updated has been published.
func (s *Service) RunConsumer(ctx context.Context, brokers []string) error {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	// Manual commit — see contract above.
	cfg.Consumer.Offsets.AutoCommit.Enable = false

	cg, err := sarama.NewConsumerGroup(brokers, OddsManagementConsumerGroup, cfg)
	if err != nil {
		return fmt.Errorf("odds: consumer group: %w", err)
	}
	defer cg.Close()

	topics := []string{"market-data.normalised", "bet.placed"}
	handler := &oddsHandler{svc: s}

	s.logger.Info("odds management: consumer started", "group", OddsManagementConsumerGroup, "topics", topics)

	for {
		if err := cg.Consume(ctx, topics, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.logger.Error("odds: consume error", "err", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type oddsHandler struct{ svc *Service }

func (h *oddsHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *oddsHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *oddsHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.svc.handleEvent(session.Context(), msg); err != nil {
			h.svc.logger.Error("odds: handle event failed",
				"topic", msg.Topic, "offset", msg.Offset, "err", err)
			// Do NOT commit on error — the lag-check contract requires offsets to
			// reflect actual processing completion.
			continue
		}
		// Commit only after DB write + odds.updated publish have both succeeded.
		session.MarkMessage(msg, "")
		session.Commit()
	}
	return nil
}

func (s *Service) handleEvent(ctx context.Context, msg *sarama.ConsumerMessage) error {
	switch msg.Topic {
	case "market-data.normalised":
		return s.handleNormalisedFeed(ctx, msg)
	case "bet.placed":
		return s.handleBetPlaced(ctx, msg)
	default:
		return nil
	}
}

// handleNormalisedFeed processes external price signals. It fetches feed
// probabilities for all selections in the market from the catalog DB,
// computes a 20-cent line using logit vig, and writes odds for each selection.
func (s *Service) handleNormalisedFeed(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event struct {
		EventType string `json:"event_type"`
		MarketID  string `json:"market_id"`
	}
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal feed event: %w", err)
	}
	if event.EventType != "price.update" {
		return nil
	}

	// Fetch selections with feed probabilities from catalog DB.
	rows, err := s.catalogDB.Query(ctx,
		`SELECT selection_id, feed_probability FROM selections
		 WHERE market_id = $1 AND active = true AND feed_probability IS NOT NULL
		 ORDER BY selection_id`,
		event.MarketID)
	if err != nil {
		return fmt.Errorf("fetch selections: %w", err)
	}
	defer rows.Close()

	var sels []SelectionInput
	for rows.Next() {
		var si SelectionInput
		if err := rows.Scan(&si.ID, &si.FeedProbability); err != nil {
			return fmt.Errorf("scan selection: %w", err)
		}
		sels = append(sels, si)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate selections: %w", err)
	}

	if len(sels) != 2 {
		s.logger.Warn("odds: market does not have exactly 2 selections, skipping",
			"market_id", event.MarketID, "count", len(sels))
		return nil
	}

	results := ComputeMarketOdds(sels)
	for _, r := range results {
		if r.Decimal == 0 {
			s.logger.Warn("odds: vigged prob < feed prob, price zeroed",
				"market_id", event.MarketID, "selection_id", r.ID,
				"feed_prob", r.FeedProbability, "vigged_prob", r.ViggedProb)
			continue
		}
		if err := s.writeOddsAndPublish(ctx, event.MarketID, r.ID, r.Decimal, r.American, msg.Offset); err != nil {
			return err
		}
	}
	return nil
}

// handleBetPlaced adjusts odds in response to bet volume (risk management).
func (s *Service) handleBetPlaced(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event struct {
		MarketID    string `json:"market_id"`
		SelectionID string `json:"selection_id"`
		StakeMinor  int64  `json:"stake_minor"`
	}
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal bet.placed: %w", err)
	}

	// Read current odds from the DB.
	var currentDecimal float64
	err := s.db.QueryRow(ctx,
		`SELECT offered_decimal FROM odds
		 WHERE market_id = $1 AND selection_id = $2
		 ORDER BY updated_at DESC LIMIT 1`,
		event.MarketID, event.SelectionID,
	).Scan(&currentDecimal)
	if err != nil {
		return nil // no odds yet; skip
	}

	// Adjust odds based on liability. Stub: shorten odds slightly on large bets.
	newDecimal := adjustForLiability(currentDecimal, event.StakeMinor)
	if newDecimal == currentDecimal {
		// No change — still commit the offset so lag resolves.
		return nil
	}
	newAmerican := DecimalToAmerican(newDecimal)

	return s.writeOddsAndPublish(ctx, event.MarketID, event.SelectionID, newDecimal, newAmerican, msg.Offset)
}

// writeOddsAndPublish writes new odds to the DB, then publishes odds.updated.
// Offset commit happens AFTER this function returns successfully.
func (s *Service) writeOddsAndPublish(ctx context.Context, marketID, selectionID string, decimal float64, american int, sourceOffset int64) error {
	// 1. Write to DB (ordered so the DB reflects committed state before Kafka publish).
	_, err := s.db.Exec(ctx, `
		INSERT INTO odds (market_id, selection_id, offered_decimal, offered_american, source_offset, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())`,
		marketID, selectionID, decimal, american, sourceOffset,
	)
	if err != nil {
		return fmt.Errorf("write odds to db: %w", err)
	}

	// 2. Publish odds.updated.
	event := OddsUpdatedEvent{
		MarketID:          marketID,
		SelectionID:       selectionID,
		Decimal:           decimal,
		American:          american,
		SourceEventOffset: sourceOffset,
		ComputedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal odds.updated: %w", err)
	}
	_, _, err = s.producer.SendMessage(&sarama.ProducerMessage{
		Topic: "odds.updated",
		Key:   sarama.StringEncoder(marketID),
		Value: sarama.ByteEncoder(data),
	})
	if err != nil {
		return fmt.Errorf("publish odds.updated: %w", err)
	}

	return nil
}

// Close shuts down the producer.
func (s *Service) Close() error {
	return s.producer.Close()
}

// ── Odds computation ─────────────────────────────────────────────────────────

// SelectionInput is a selection with its feed probability, used as input to
// the odds computation.
type SelectionInput struct {
	ID              string
	FeedProbability float64
}

// SelectionResult holds computed odds for one selection.
type SelectionResult struct {
	ID              string
	FeedProbability float64
	ViggedProb      float64 // implied probability after vig
	Decimal         float64 // 0 if zeroed out
	American        int     // 0 if zeroed out
}

// ComputeMarketOdds computes vigged odds for a binary market using logit vig
// calibrated to a 20-cent line.
//
// Steps:
//  1. Average feed probabilities per selection (ready for multiple feeds).
//  2. Normalise to fair probabilities (sum to 1).
//  3. Binary-search for the logit shift δ that produces a 20-cent American
//     odds spread.
//  4. If a selection's vigged implied probability is lower than its original
//     feed probability, its price is set to zero.
func ComputeMarketOdds(sels []SelectionInput) []SelectionResult {
	if len(sels) != 2 {
		return nil
	}

	fp0, fp1 := sels[0].FeedProbability, sels[1].FeedProbability

	// Normalise to fair probabilities (sum to 1).
	total := fp0 + fp1
	if total <= 0 {
		return nil
	}
	p0 := fp0 / total
	p1 := fp1 / total

	// Clamp to avoid logit domain errors at extremes.
	const eps = 1e-6
	p0 = clamp(p0, eps, 1-eps)
	p1 = clamp(p1, eps, 1-eps)

	delta := find20CentDelta(p0, p1)

	results := make([]SelectionResult, 2)
	fairProbs := [2]float64{p0, p1}
	for i := range sels {
		q := sigmoid(logit(fairProbs[i]) + delta)
		dec := 1.0 / q
		am := DecimalToAmerican(dec)

		// If vigged implied probability is lower than the feed probability,
		// the line would give the bettor positive EV — zero out the price.
		if q < sels[i].FeedProbability {
			dec = 0
			am = 0
		}

		results[i] = SelectionResult{
			ID:              sels[i].ID,
			FeedProbability: sels[i].FeedProbability,
			ViggedProb:      q,
			Decimal:         dec,
			American:        am,
		}
	}
	return results
}

// ── Logit vig helpers ────────────────────────────────────────────────────────

func logit(p float64) float64 {
	return math.Log(p / (1 - p))
}

func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// americanFloat converts decimal odds to American odds as a float (no rounding),
// used internally by the binary search.
func americanFloat(decimal float64) float64 {
	if decimal >= 2.0 {
		return (decimal - 1.0) * 100.0
	}
	if decimal <= 1.0 {
		return 0
	}
	return -100.0 / (decimal - 1.0)
}

// centSpread computes the American-odds cent spread for a two-outcome market.
// qFav must be >= qDog. For example:
//
//	-130 / +120 → spread = 130 - 120 = 10
//	-105 / -105 → spread = 105 + 105 - 200 = 10
func centSpread(qFav, qDog float64) float64 {
	aFav := americanFloat(1.0 / qFav) // negative (favourite)
	aDog := americanFloat(1.0 / qDog) // positive or negative (underdog)

	if aFav < 0 && aDog >= 0 {
		return (-aFav) - aDog
	}
	// Both negative (near even money): total juice = |a1| + |a2| - 200.
	if aFav < 0 && aDog < 0 {
		return (-aFav) + (-aDog) - 200
	}
	return 0
}

// find20CentDelta binary-searches for the logit shift δ that produces a
// 20-cent American odds spread for a binary market with fair probabilities
// p0 and p1.
func find20CentDelta(p0, p1 float64) float64 {
	lo, hi := 0.0, 5.0
	for i := 0; i < 100; i++ {
		mid := (lo + hi) / 2
		q0 := sigmoid(logit(p0) + mid)
		q1 := sigmoid(logit(p1) + mid)

		qFav, qDog := q0, q1
		if q1 > q0 {
			qFav, qDog = q1, q0
		}

		if centSpread(qFav, qDog) < 20.0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// ── Liability adjustment ─────────────────────────────────────────────────────

// adjustForLiability shortens odds slightly based on accumulated stake.
func adjustForLiability(decimal float64, stakeMinor int64) float64 {
	// Stub: shorten by 1% for stakes over £50.
	if stakeMinor > 5000 && decimal > 1.0 {
		return 1.0 + (decimal-1.0)*0.99
	}
	return decimal
}

// ── American / Decimal conversion ────────────────────────────────────────────

// DecimalToAmerican converts decimal odds to American format.
// Decimal >= 2.0 → positive American (e.g. 2.50 → +150).
// Decimal < 2.0  → negative American (e.g. 1.50 → -200).
func DecimalToAmerican(decimal float64) int {
	if decimal >= 2.0 {
		return int(math.Round((decimal - 1.0) * 100))
	}
	if decimal <= 1.0 {
		return 0
	}
	return int(math.Round(-100.0 / (decimal - 1.0)))
}

// AmericanToDecimal converts American odds to decimal format.
func AmericanToDecimal(american int) float64 {
	if american > 0 {
		return float64(american)/100.0 + 1.0
	}
	if american < 0 {
		return 100.0/float64(-american) + 1.0
	}
	return 1.0 // even money edge case
}
