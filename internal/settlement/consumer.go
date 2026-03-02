// Package settlement implements the Settlement Service: consumes result events
// from Market Data Ingestion and instructs Account & Wallet to credit winnings.
package settlement

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
)

const consumerGroupSettlement = "settlement-cg"

// Consumer processes result events and settles bets.
type Consumer struct {
	db           *pgxpool.Pool
	walletClient sbv1.WalletServiceClient
	producer     sarama.SyncProducer
	logger       *slog.Logger
}

// NewConsumer creates a Settlement consumer.
func NewConsumer(db *pgxpool.Pool, walletConn *grpc.ClientConn, producer sarama.SyncProducer, logger *slog.Logger) *Consumer {
	return &Consumer{
		db:           db,
		walletClient: sbv1.NewWalletServiceClient(walletConn),
		producer:     producer,
		logger:       logger,
	}
}

// Run starts consuming from Kafka until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context, brokers []string) error {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Consumer.Offsets.AutoCommit.Enable = false

	cg, err := sarama.NewConsumerGroup(brokers, consumerGroupSettlement, cfg)
	if err != nil {
		return fmt.Errorf("settlement: consumer group: %w", err)
	}
	defer cg.Close()

	handler := &settlementHandler{consumer: c}
	for {
		if err := cg.Consume(ctx, []string{"market-data.normalised"}, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.Error("settlement: consume error", "err", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type settlementHandler struct{ consumer *Consumer }

func (h *settlementHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *settlementHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *settlementHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.consumer.handleResult(session.Context(), msg); err != nil {
			h.consumer.logger.Error("settlement: handle result failed", "offset", msg.Offset, "err", err)
			continue
		}
		session.MarkMessage(msg, "")
		session.Commit()
	}
	return nil
}

// ResultEvent is the normalised event type for market results.
type ResultEvent struct {
	EventType   string `json:"event_type"` // "market.resulted"
	MarketID    string `json:"market_id"`
	SelectionID string `json:"winning_selection_id"`
	ResultedAt  string `json:"resulted_at"`
}

func (c *Consumer) handleResult(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event ResultEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal result event: %w", err)
	}
	if event.EventType != "market.resulted" {
		return nil // not a result event; skip
	}

	// Find all unsettled PLACED bets on this market.
	rows, err := c.db.Query(ctx, `
		SELECT bet_id, user_id, selection_id, stake_minor, odds_num, odds_den, currency
		FROM pending_bets
		WHERE market_id = $1 AND status = 'PLACED'`,
		event.MarketID,
	)
	if err != nil {
		return fmt.Errorf("query pending bets: %w", err)
	}
	defer rows.Close()

	type pendingBet struct {
		BetID       string
		UserID      string
		SelectionID string
		StakeMinor  int64
		OddsNum     int64
		OddsDen     int64
		Currency    string
	}

	var bets []pendingBet
	for rows.Next() {
		var b pendingBet
		if err := rows.Scan(&b.BetID, &b.UserID, &b.SelectionID, &b.StakeMinor, &b.OddsNum, &b.OddsDen, &b.Currency); err != nil {
			return fmt.Errorf("scan pending bet: %w", err)
		}
		bets = append(bets, b)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	resultsAt, _ := time.Parse(time.RFC3339Nano, event.ResultedAt)
	if resultsAt.IsZero() {
		resultsAt = time.Now()
	}

	for _, bet := range bets {
		isWin := bet.SelectionID == event.SelectionID
		if err := c.settleBet(ctx, bet.BetID, bet.UserID, isWin, bet.StakeMinor, bet.OddsNum, bet.OddsDen, bet.Currency, resultsAt); err != nil {
			c.logger.Error("settlement: settle bet failed", "bet_id", bet.BetID, "err", err)
		}
	}
	return nil
}

func (c *Consumer) settleBet(
	ctx context.Context,
	betID, userID string,
	isWin bool,
	stakeMinor, oddsNum, oddsDen int64,
	currency string,
	settledAt time.Time,
) error {
	// Compute payout (only if win).
	var payoutMinor int64
	if isWin && oddsDen > 0 {
		// Decimal odds = num/den; payout = stake * decimal_odds
		payoutMinor = stakeMinor * oddsNum / oddsDen
	}

	outcome := "LOSS"
	if isWin {
		outcome = "WIN"
	}

	// Record settlement in local DB (idempotent via ON CONFLICT DO NOTHING).
	_, err := c.db.Exec(ctx, `
		INSERT INTO settlements (settlement_id, bet_id, outcome, payout_minor, settled_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (bet_id) DO NOTHING`,
		uuid.New().String(), betID, outcome, payoutMinor, settledAt,
	)
	if err != nil {
		return fmt.Errorf("insert settlement: %w", err)
	}

	// Credit the wallet.
	reason := sbv1.CreditReason_CREDIT_REASON_WIN
	creditAmount := payoutMinor
	if !isWin {
		// On loss, release bets_in_flight (stake was already deducted; return 0 winnings
		// but we still need to debit bets_in_flight column). We send 0 credit.
		reason = sbv1.CreditReason_CREDIT_REASON_WIN // wallet handles loss path by amount=0
		creditAmount = 0
	}

	if _, err := c.walletClient.CreditBalance(ctx, &sbv1.CreditBalanceRequest{
		TransactionId:     uuid.New().String(),
		UserId:            userID,
		Amount:            &sbv1.Money{AmountMinor: creditAmount, Currency: currency},
		Reason:            reason,
		OriginatingBetId: betID,
	}); err != nil {
		return fmt.Errorf("CreditBalance: %w", err)
	}

	// Publish bet.settled event.
	payload, _ := json.Marshal(map[string]any{
		"event_type":   "bet.settled",
		"bet_id":       betID,
		"outcome":      outcome,
		"payout_minor": payoutMinor,
		"settled_at":   settledAt.UTC().Format(time.RFC3339Nano),
	})
	_, _, err = c.producer.SendMessage(&sarama.ProducerMessage{
		Topic: "bet.settled",
		Key:   sarama.StringEncoder(userID),
		Value: sarama.ByteEncoder(payload),
	})
	return err
}
