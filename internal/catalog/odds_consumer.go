package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/IBM/sarama"
)

// oddsUpdateMsg is the WebSocket message sent for odds changes.
type oddsUpdateMsg struct {
	Type       string          `json:"type"`
	MarketID   string          `json:"market_id"`
	Selections []oddsSelection `json:"selections"`
}

type oddsSelection struct {
	SelectionID  string  `json:"selection_id"`
	OddsDecimal  float64 `json:"odds_decimal"`
	OddsAmerican int     `json:"odds_american"`
}

// ConsumeOddsUpdates listens to the odds.updated Kafka topic and broadcasts
// odds changes to WebSocket clients via the Broadcaster.
func ConsumeOddsUpdates(ctx context.Context, brokers []string, broadcaster *Broadcaster, logger *slog.Logger) error {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Consumer.Offsets.AutoCommit.Enable = true
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest // only care about live updates

	cg, err := sarama.NewConsumerGroup(brokers, "catalog-odds-ws-cg", cfg)
	if err != nil {
		return fmt.Errorf("odds ws consumer: consumer group: %w", err)
	}
	defer cg.Close()

	handler := &oddsWSHandler{broadcaster: broadcaster, logger: logger}
	for {
		if err := cg.Consume(ctx, []string{"odds.updated"}, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Error("odds ws consumer: consume error", "err", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type oddsWSHandler struct {
	broadcaster *Broadcaster
	logger      *slog.Logger
}

func (h *oddsWSHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *oddsWSHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *oddsWSHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// Buffer odds updates per market to coalesce rapid updates for the same market.
	for msg := range claim.Messages() {
		var event struct {
			MarketID    string  `json:"market_id"`
			SelectionID string  `json:"selection_id"`
			Decimal     float64 `json:"decimal"`
			American    int     `json:"american"`
		}
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			h.logger.Warn("odds ws consumer: unmarshal failed", "err", err)
			session.MarkMessage(msg, "")
			continue
		}

		// Skip broadcasting if no clients are connected.
		if h.broadcaster.ClientCount() == 0 {
			session.MarkMessage(msg, "")
			continue
		}

		wsMsg := oddsUpdateMsg{
			Type:     "odds_update",
			MarketID: event.MarketID,
			Selections: []oddsSelection{
				{
					SelectionID:  event.SelectionID,
					OddsDecimal:  event.Decimal,
					OddsAmerican: event.American,
				},
			},
		}

		data, err := json.Marshal(wsMsg)
		if err != nil {
			h.logger.Error("odds ws consumer: marshal failed", "err", err)
			session.MarkMessage(msg, "")
			continue
		}

		h.broadcaster.Broadcast(data)
		session.MarkMessage(msg, "")
	}
	return nil
}
