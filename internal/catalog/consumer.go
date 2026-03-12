package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sportsbook/sb/internal/marketdata"
)

// ConsumeNormalisedFeed consumes market-data.normalised and upserts catalog data.
// When a Broadcaster is provided (non-nil), game.state updates are broadcast to
// WebSocket clients after being persisted.
func ConsumeNormalisedFeed(ctx context.Context, brokers []string, db *pgxpool.Pool, broadcaster *Broadcaster, logger *slog.Logger) error {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Consumer.Offsets.AutoCommit.Enable = true
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	cg, err := sarama.NewConsumerGroup(brokers, "market-catalog-cg", cfg)
	if err != nil {
		return fmt.Errorf("catalog consumer: consumer group: %w", err)
	}
	defer cg.Close()

	handler := &catalogFeedHandler{db: db, broadcaster: broadcaster, logger: logger}
	for {
		if err := cg.Consume(ctx, []string{"market-data-normalised"}, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Error("catalog consumer: consume error", "err", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type catalogFeedHandler struct {
	db          *pgxpool.Pool
	broadcaster *Broadcaster
	logger      *slog.Logger
}

func (h *catalogFeedHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *catalogFeedHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *catalogFeedHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var event marketdata.NormalisedMarketEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			h.logger.Warn("catalog consumer: unmarshal failed", "err", err)
			session.MarkMessage(msg, "")
			continue
		}

		ctx := session.Context()
		switch event.EventType {
		case "catalog.upsert":
			if err := h.handleCatalogUpsert(ctx, &event); err != nil {
				h.logger.Error("catalog consumer: upsert failed",
					"event_type", event.EventType, "market_id", event.MarketID, "err", err)
			}
		case "game.state":
			if err := h.handleGameState(ctx, &event); err != nil {
				h.logger.Error("catalog consumer: game state update failed",
					"event_id", event.EventID, "err", err)
			} else if h.broadcaster != nil {
				h.broadcastScoreUpdate(&event)
			}
		case "price.update":
			// Handled by odds management consumer group, not us.
		default:
			h.logger.Debug("catalog consumer: ignoring event", "event_type", event.EventType)
		}

		session.MarkMessage(msg, "")
	}
	return nil
}

func (h *catalogFeedHandler) handleCatalogUpsert(ctx context.Context, e *marketdata.NormalisedMarketEvent) error {
	// Ensure sport exists.
	if e.SportID != "" && e.SportName != "" {
		if _, err := h.db.Exec(ctx,
			`INSERT INTO sports (sport_id, name) VALUES ($1, $2) ON CONFLICT (sport_id) DO NOTHING`,
			e.SportID, e.SportName,
		); err != nil {
			return fmt.Errorf("upsert sport: %w", err)
		}
	}

	// Ensure competition exists.
	if e.CompetitionID != "" && e.CompetitionName != "" {
		if _, err := h.db.Exec(ctx, `
			INSERT INTO competitions (competition_id, sport_id, name, country)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (competition_id) DO NOTHING`,
			e.CompetitionID, e.SportID, e.CompetitionName, e.Country,
		); err != nil {
			return fmt.Errorf("upsert competition: %w", err)
		}
	}

	// Upsert event.
	if e.EventID != "" && e.EventName != "" {
		if _, err := h.db.Exec(ctx, `
			INSERT INTO events (event_id, competition_id, name, starts_at, status)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (event_id) DO UPDATE
			    SET name       = EXCLUDED.name,
			        starts_at  = EXCLUDED.starts_at,
			        status     = EXCLUDED.status,
			        updated_at = NOW()`,
			e.EventID, e.CompetitionID, e.EventName, e.StartsAt, e.EventStatus,
		); err != nil {
			return fmt.Errorf("upsert event: %w", err)
		}
	}

	// Upsert market.
	if e.MarketID != "" && e.MarketName != "" {
		if _, err := h.db.Exec(ctx, `
			INSERT INTO markets
			    (market_id, event_id, name, status, market_type, target_value, is_main, closes_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (market_id) DO UPDATE
			    SET name         = EXCLUDED.name,
			        market_type  = EXCLUDED.market_type,
			        target_value = EXCLUDED.target_value,
			        is_main      = EXCLUDED.is_main,
			        updated_at   = NOW()`,
			e.MarketID, e.EventID, e.MarketName, e.MarketStatus,
			e.MarketType, e.TargetValue, e.IsMain, e.ClosesAt,
		); err != nil {
			return fmt.Errorf("upsert market: %w", err)
		}
	}

	// Upsert selection.
	if e.SelectionID != "" && e.SelectionName != "" {
		if _, err := h.db.Exec(ctx, `
			INSERT INTO selections
			    (selection_id, market_id, name, active, target_value, feed_probability)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (selection_id) DO UPDATE
			    SET name             = EXCLUDED.name,
			        target_value     = EXCLUDED.target_value,
			        feed_probability = EXCLUDED.feed_probability`,
			e.SelectionID, e.MarketID, e.SelectionName,
			e.SelActive, e.SelTargetValue, e.FeedProbability,
		); err != nil {
			return fmt.Errorf("upsert selection: %w", err)
		}
	}

	return nil
}

func (h *catalogFeedHandler) handleGameState(ctx context.Context, e *marketdata.NormalisedMarketEvent) error {
	_, err := h.db.Exec(ctx, `
		UPDATE events
		SET home_score  = $2,
		    away_score  = $3,
		    game_period = $4,
		    game_clock  = $5,
		    status      = $6,
		    updated_at  = NOW()
		WHERE event_id = $1`,
		e.EventID,
		e.HomeScore,
		e.AwayScore,
		e.GamePeriod,
		e.GameClock,
		e.EventStatus,
	)
	return err
}

// broadcastScoreUpdate sends a score_update message to all WebSocket clients.
func (h *catalogFeedHandler) broadcastScoreUpdate(e *marketdata.NormalisedMarketEvent) {
	msg := struct {
		Type       string `json:"type"`
		EventID    string `json:"event_id"`
		HomeScore  int    `json:"home_score"`
		AwayScore  int    `json:"away_score"`
		GamePeriod string `json:"game_period"`
		GameClock  string `json:"game_clock"`
		Status     string `json:"status"`
	}{
		Type:       "score_update",
		EventID:    e.EventID,
		HomeScore:  e.HomeScore,
		AwayScore:  e.AwayScore,
		GamePeriod: e.GamePeriod,
		GameClock:  e.GameClock,
		Status:     e.EventStatus,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("catalog consumer: marshal score update failed", "err", err)
		return
	}
	h.broadcaster.Broadcast(data)
}
