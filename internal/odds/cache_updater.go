// Package odds implements the Odds Management Service internals.
package odds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
)

const (
	// TopicOddsUpdated is the topic this updater consumes.
	TopicOddsUpdated = "odds.updated"

	// ConsumerGroupOdds is the Kafka consumer group for the cache updater.
	// This is DISTINCT from odds-management-cg (which processes bet.placed).
	// The cache updater has its own consumer group so its offset is independent.
	ConsumerGroupOdds = "odds-cache-updater-cg"

	// OddsCacheTTL is the Redis TTL for odds cache entries (30s per spec).
	OddsCacheTTL = 30 * time.Second
)

// OddsUpdatedEvent is the canonical payload published to the odds.updated topic
// by the Odds Management Service after computing new odds.
type OddsUpdatedEvent struct {
	MarketID    string  `json:"market_id"`
	SelectionID string  `json:"selection_id"`
	Decimal     float64 `json:"decimal"`  // e.g. 2.50
	American    int     `json:"american"` // e.g. +150 or -200
	// SourceEventOffset is the offset of the market-data.normalised event that
	// triggered this odds computation. Written into Redis so Bet Acceptance can
	// use it as a secondary consistency signal alongside the lag check.
	SourceEventOffset int64  `json:"src_offset"`
	ComputedAt        string `json:"computed_at"` // RFC3339Nano
}

// CachedOddsEntry is the JSON blob written into Redis.
// Key: odds:{market_id}:{selection_id}
type CachedOddsEntry struct {
	Decimal           float64 `json:"decimal"`
	American          int     `json:"american"`
	SourceEventOffset int64   `json:"src_offset"`
	UpdatedAt         string  `json:"updated_at"`
}

// CacheUpdater consumes the odds.updated topic and writes entries into the
// Redis odds cache. It is the SOLE writer of the odds cache (single writer
// invariant from the architecture spec).
//
// Commit strategy: offsets are committed only AFTER the Redis write succeeds.
// This ensures that if the service crashes mid-write, the event is re-processed
// and the cache entry is refreshed on restart. Redis SET is idempotent so
// double-processing is safe.
type CacheUpdater struct {
	consumer sarama.ConsumerGroup
	rdb      *redis.Client
	logger   *slog.Logger
}

// NewCacheUpdater builds a CacheUpdater.
// brokers: Kafka broker addresses.
// rdb:     The Redis odds cache instance (volatile-ttl policy).
func NewCacheUpdater(brokers []string, rdb *redis.Client, logger *slog.Logger) (*CacheUpdater, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	// Disable auto-commit: we commit manually after the Redis write succeeds.
	cfg.Consumer.Offsets.AutoCommit.Enable = false

	cg, err := sarama.NewConsumerGroup(brokers, ConsumerGroupOdds, cfg)
	if err != nil {
		return nil, fmt.Errorf("cache_updater: create consumer group: %w", err)
	}
	return &CacheUpdater{consumer: cg, rdb: rdb, logger: logger}, nil
}

// Run starts consuming odds.updated events until ctx is cancelled.
func (u *CacheUpdater) Run(ctx context.Context) error {
	handler := &cacheUpdaterHandler{rdb: u.rdb, logger: u.logger}
	for {
		if err := u.consumer.Consume(ctx, []string{TopicOddsUpdated}, handler); err != nil {
			if ctx.Err() != nil {
				return nil // normal shutdown
			}
			u.logger.Error("cache_updater: consume error", "err", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

// Close shuts down the consumer group.
func (u *CacheUpdater) Close() error {
	return u.consumer.Close()
}

// ── sarama.ConsumerGroupHandler implementation ────────────────────────────────

type cacheUpdaterHandler struct {
	rdb    *redis.Client
	logger *slog.Logger
	session sarama.ConsumerGroupSession
}

func (h *cacheUpdaterHandler) Setup(session sarama.ConsumerGroupSession) error {
	h.session = session
	return nil
}

func (h *cacheUpdaterHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (h *cacheUpdaterHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.handleMessage(session.Context(), msg); err != nil {
			h.logger.Error("cache_updater: handle message failed",
				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "err", err)
			// Continue — log and move on. A persistent failure here should alert ops.
			// Do NOT commit this offset so we retry on restart.
			continue
		}
		// Commit offset only after successful Redis write.
		session.MarkMessage(msg, "")
		session.Commit()
	}
	return nil
}

func (h *cacheUpdaterHandler) handleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var event OddsUpdatedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("unmarshal OddsUpdatedEvent: %w", err)
	}

	if event.MarketID == "" || event.SelectionID == "" {
		return fmt.Errorf("missing market_id or selection_id in event at offset %d", msg.Offset)
	}

	entry := CachedOddsEntry{
		Decimal:           event.Decimal,
		American:          event.American,
		SourceEventOffset: event.SourceEventOffset,
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal cache entry: %w", err)
	}

	key := fmt.Sprintf("odds:%s:%s", event.MarketID, event.SelectionID)
	if err := h.rdb.Set(ctx, key, data, OddsCacheTTL).Err(); err != nil {
		return fmt.Errorf("redis SET %s: %w", key, err)
	}

	h.logger.Debug("cache_updater: odds cached",
		"market_id", event.MarketID,
		"selection_id", event.SelectionID,
		"decimal", event.Decimal,
		"american", event.American,
		"src_offset", event.SourceEventOffset,
	)
	return nil
}
