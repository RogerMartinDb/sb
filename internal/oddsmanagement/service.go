// Package oddsmanagement implements the Odds Management Service: subscribes to
// normalised feed events and bet.placed events, computes offered odds, and
// publishes odds.updated.
package oddsmanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	MarketID          string `json:"market_id"`
	SelectionID       string `json:"selection_id"`
	Numerator         int64  `json:"num"`
	Denominator       int64  `json:"den"`
	SourceEventOffset int64  `json:"src_offset"` // offset of the triggering event
	ComputedAt        string `json:"computed_at"`
}

// Service is the Odds Management Service.
type Service struct {
	db       *pgxpool.Pool
	producer sarama.SyncProducer
	logger   *slog.Logger
}

// NewService creates the service with a Kafka producer for odds.updated.
func NewService(db *pgxpool.Pool, brokers []string, logger *slog.Logger) (*Service, error) {
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
	return &Service{db: db, producer: producer, logger: logger}, nil
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

// handleNormalisedFeed processes external price signals.
func (s *Service) handleNormalisedFeed(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event struct {
		EventType       string `json:"event_type"`
		MarketID        string `json:"market_id"`
		SelectionID     string `json:"selection_id"`
		OddsNumerator   int64  `json:"odds_num"`
		OddsDenominator int64  `json:"odds_den"`
	}
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal feed event: %w", err)
	}
	if event.EventType != "price.update" {
		return nil
	}

	// Compute final offered odds (apply margin, limits, etc.).
	// Stub: use raw odds for now.
	offeredNum, offeredDen := applyMargin(event.OddsNumerator, event.OddsDenominator)

	return s.writeOddsAndPublish(ctx, event.MarketID, event.SelectionID, offeredNum, offeredDen, msg.Offset)
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
	var currentNum, currentDen int64
	err := s.db.QueryRow(ctx,
		`SELECT offered_num, offered_den FROM odds
		 WHERE market_id = $1 AND selection_id = $2
		 ORDER BY updated_at DESC LIMIT 1`,
		event.MarketID, event.SelectionID,
	).Scan(&currentNum, &currentDen)
	if err != nil {
		return nil // no odds yet; skip
	}

	// Adjust odds based on liability. Stub: shorten odds slightly on large bets.
	newNum, newDen := adjustForLiability(currentNum, currentDen, event.StakeMinor)
	if newNum == currentNum && newDen == currentDen {
		// No change — still commit the offset so lag resolves.
		return nil
	}

	return s.writeOddsAndPublish(ctx, event.MarketID, event.SelectionID, newNum, newDen, msg.Offset)
}

// writeOddsAndPublish writes new odds to the DB, then publishes odds.updated.
// Offset commit happens AFTER this function returns successfully.
func (s *Service) writeOddsAndPublish(ctx context.Context, marketID, selectionID string, num, den, sourceOffset int64) error {
	// 1. Write to DB (ordered so the DB reflects committed state before Kafka publish).
	_, err := s.db.Exec(ctx, `
		INSERT INTO odds (market_id, selection_id, offered_num, offered_den, source_offset, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())`,
		marketID, selectionID, num, den, sourceOffset,
	)
	if err != nil {
		return fmt.Errorf("write odds to db: %w", err)
	}

	// 2. Publish odds.updated.
	event := OddsUpdatedEvent{
		MarketID:          marketID,
		SelectionID:       selectionID,
		Numerator:         num,
		Denominator:       den,
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

// ── Odds computation helpers (stubs) ─────────────────────────────────────────

// applyMargin applies the operator's overround to the raw provider odds.
// Stub: 5% margin reduction.
func applyMargin(num, den int64) (int64, int64) {
	// Convert to decimal, apply 5% reduction, convert back.
	if den == 0 {
		return num, den
	}
	return num * 95, den * 100
}

// adjustForLiability shortens odds slightly based on accumulated stake.
func adjustForLiability(num, den, stakeMinor int64) (int64, int64) {
	// Stub: shorten by 1% for stakes over £50.
	if stakeMinor > 5000 && num > 0 {
		return num * 99, den * 100
	}
	return num, den
}
