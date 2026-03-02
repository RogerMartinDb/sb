// Package bethistory implements the Bet History service: an append-only read
// model of placed and settled bets, consumed asynchronously from Kafka.
package bethistory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
)

const (
	consumerGroupBetHistory = "bet-history-cg"
)

// BetRecord is the read model stored in db_bet_history.
type BetRecord struct {
	BetID       string
	UserID      string
	MarketID    string
	SelectionID string
	OddsNum     int64
	OddsDen     int64
	StakeMinor  int64
	Currency    string
	Status      string // PLACED, SETTLED_WIN, SETTLED_LOSS, VOID
	PlacedAt    time.Time
	SettledAt   *time.Time
	PayoutMinor *int64
}

// Consumer processes bet.placed and bet.settled events from Kafka.
type Consumer struct {
	db           *pgxpool.Pool
	walletClient sbv1.WalletServiceClient
	logger       *slog.Logger
}

// NewConsumer creates a Bet History consumer.
// walletConn is used to call ConfirmTransaction after recording bet.placed.
func NewConsumer(db *pgxpool.Pool, walletConn *grpc.ClientConn, logger *slog.Logger) *Consumer {
	return &Consumer{
		db:           db,
		walletClient: sbv1.NewWalletServiceClient(walletConn),
		logger:       logger,
	}
}

// Run starts consuming from Kafka until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context, brokers []string) error {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Consumer.Offsets.AutoCommit.Enable = false

	cg, err := sarama.NewConsumerGroup(brokers, consumerGroupBetHistory, cfg)
	if err != nil {
		return fmt.Errorf("bet_history: consumer group: %w", err)
	}
	defer cg.Close()

	topics := []string{"bet.placed", "bet.settled", "bet.voided"}
	handler := &betHistoryHandler{consumer: c}

	for {
		if err := cg.Consume(ctx, topics, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.Error("bet_history: consume error", "err", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type betHistoryHandler struct{ consumer *Consumer }

func (h *betHistoryHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *betHistoryHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *betHistoryHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	c := h.consumer
	for msg := range claim.Messages() {
		var err error
		switch msg.Topic {
		case "bet.placed":
			err = c.handleBetPlaced(session.Context(), msg)
		case "bet.settled":
			err = c.handleBetSettled(session.Context(), msg)
		case "bet.voided":
			err = c.handleBetVoided(session.Context(), msg)
		}
		if err != nil {
			c.logger.Error("bet_history: handle message failed",
				"topic", msg.Topic, "offset", msg.Offset, "err", err)
			continue // skip bad message; do not commit offset
		}
		session.MarkMessage(msg, "")
		session.Commit()
	}
	return nil
}

// handleBetPlaced inserts the bet record and confirms the wallet transaction.
func (c *Consumer) handleBetPlaced(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event struct {
		BetID       string `json:"bet_id"`
		UserID      string `json:"user_id"`
		MarketID    string `json:"market_id"`
		SelectionID string `json:"selection_id"`
		OddsNum     int64  `json:"odds_num"`
		OddsDen     int64  `json:"odds_den"`
		StakeMinor  int64  `json:"stake_minor"`
		Currency    string `json:"currency"`
		PlacedAt    string `json:"placed_at"`
		// TransactionID comes from a Kafka header set by Bet Acceptance.
	}
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal bet.placed: %w", err)
	}

	placedAt, err := time.Parse(time.RFC3339Nano, event.PlacedAt)
	if err != nil {
		placedAt = time.Now()
	}

	// Extract transaction_id from Kafka headers.
	var transactionID string
	for _, h := range msg.Headers {
		if string(h.Key) == "transaction_id" {
			transactionID = string(h.Value)
			break
		}
	}

	// Insert into bet_history (idempotent via ON CONFLICT DO NOTHING).
	_, err = c.db.Exec(ctx, `
		INSERT INTO bets (bet_id, user_id, market_id, selection_id, odds_num, odds_den,
		                  stake_minor, currency, status, placed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'PLACED', $9)
		ON CONFLICT (bet_id) DO NOTHING`,
		event.BetID, event.UserID, event.MarketID, event.SelectionID,
		event.OddsNum, event.OddsDen, event.StakeMinor, event.Currency, placedAt,
	)
	if err != nil {
		return fmt.Errorf("insert bet record: %w", err)
	}

	// Confirm wallet transaction if we have the ID.
	if transactionID != "" {
		if _, err := c.walletClient.ConfirmTransaction(ctx, &sbv1.ConfirmTransactionRequest{
			TransactionId: transactionID,
		}); err != nil {
			c.logger.Warn("bet_history: ConfirmTransaction failed (will retry on offset replay)",
				"transaction_id", transactionID, "err", err)
			return fmt.Errorf("ConfirmTransaction: %w", err)
		}
	}

	c.logger.Info("bet_history: bet recorded", "bet_id", event.BetID)
	return nil
}

func (c *Consumer) handleBetSettled(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event struct {
		BetID       string `json:"bet_id"`
		Outcome     string `json:"outcome"` // WIN, LOSS
		PayoutMinor int64  `json:"payout_minor"`
		SettledAt   string `json:"settled_at"`
	}
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal bet.settled: %w", err)
	}

	newStatus := "SETTLED_LOSS"
	if event.Outcome == "WIN" {
		newStatus = "SETTLED_WIN"
	}

	settledAt, _ := time.Parse(time.RFC3339Nano, event.SettledAt)

	_, err := c.db.Exec(ctx, `
		UPDATE bets SET status = $1, settled_at = $2, payout_minor = $3
		WHERE bet_id = $4`,
		newStatus, settledAt, event.PayoutMinor, event.BetID,
	)
	return err
}

func (c *Consumer) handleBetVoided(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event struct {
		BetID    string `json:"bet_id"`
		VoidedAt string `json:"voided_at"`
	}
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal bet.voided: %w", err)
	}
	voidedAt, _ := time.Parse(time.RFC3339Nano, event.VoidedAt)
	_, err := c.db.Exec(ctx, `
		UPDATE bets SET status = 'VOID', settled_at = $1 WHERE bet_id = $2`,
		voidedAt, event.BetID,
	)
	return err
}
