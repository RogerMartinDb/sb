// Package betacceptance implements the synchronous bet placement flow.
package betacceptance

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
)

const (
	// lagCacheTTL is the Redis cache duration for lag results per partition.
	// 200ms prevents hammering the Kafka broker admin API on high-throughput paths.
	lagCacheTTL = 200 * time.Millisecond

	// TopicBetPlaced is the topic whose lag we check to ensure Odds Management
	// has processed all prior bet.placed events before we accept a large new bet.
	TopicBetPlaced = "bet-placed"

	// OddsManagementConsumerGroup is the group whose committed offsets we compare
	// against the log-end offset to compute lag.
	OddsManagementConsumerGroup = "odds-management-cg"
)

// LagChecker wraps the Kafka client + ClusterAdmin APIs and a Redis cache to answer:
//
//	"Has the odds-management consumer group processed all messages on partition P
//	 of the bet.placed topic?"
//
// It is called on the critical bet-acceptance path, so it caches results for
// lagCacheTTL per partition to avoid overwhelming the broker admin endpoint.
//
// Fail-CLOSED contract: if the broker admin API is unavailable, IsLagging
// returns (true, err) — callers must reject the bet.
type LagChecker struct {
	client sarama.Client       // used for GetOffset (log-end offset)
	admin  sarama.ClusterAdmin // used for ListConsumerGroupOffsets (committed offset)
	rdb    *redis.Client       // redis-ratelimit instance (4th Redis logical instance)
	mu     sync.Mutex
}

// NewLagChecker constructs a LagChecker.
// brokers must include at least one reachable Kafka broker address.
// rdb should be the rate-limit/idempotency Redis instance (configured allkeys-lru).
func NewLagChecker(brokers []string, rdb *redis.Client) (*LagChecker, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Net.DialTimeout = 3 * time.Second
	cfg.Net.ReadTimeout = 3 * time.Second

	client, err := sarama.NewClient(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("lag_checker: create client: %w", err)
	}

	admin, err := sarama.NewClusterAdminFromClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("lag_checker: create cluster admin: %w", err)
	}

	return &LagChecker{client: client, admin: admin, rdb: rdb}, nil
}

// IsLagging returns true if the Odds Management consumer group has unprocessed
// messages on the partition that owns marketID.
//
// partition is the Kafka partition number (derived by the caller using the same
// hash function used when producing to bet.placed — market_id % num_partitions).
//
// Fail-CLOSED: on any admin error the function returns (true, err).
func (lc *LagChecker) IsLagging(ctx context.Context, partition int32) (bool, error) {
	cacheKey := fmt.Sprintf("lagcheck:%s:%d", TopicBetPlaced, partition)

	// ── Cache read ────────────────────────────────────────────────────────────
	cached, err := lc.rdb.Get(ctx, cacheKey).Int64()
	if err == nil {
		return cached > 0, nil
	}
	if err != redis.Nil {
		// Redis unavailable — skip cache and hit Kafka directly (still fail-closed
		// on Kafka failure below).
	}

	// ── Fetch from Kafka ──────────────────────────────────────────────────────
	lag, err := lc.fetchLag(ctx, partition)
	if err != nil {
		// Fail CLOSED: treat Kafka admin failure as lagging.
		return true, fmt.Errorf("lag_checker: fetch lag partition %d: %w", partition, err)
	}

	// ── Cache result ──────────────────────────────────────────────────────────
	// Ignore Redis write errors — result still valid for this call.
	_ = lc.rdb.Set(ctx, cacheKey, lag, lagCacheTTL).Err()

	return lag > 0, nil
}

// fetchLag computes lag using two Sarama APIs:
//
//	step 1 — client.GetOffset  → log-end offset  (sarama.OffsetNewest returns next offset to be written)
//	step 2 — admin.ListConsumerGroupOffsets → committed offset
//
//	lag = latestOffset − committedOffset
func (lc *LagChecker) fetchLag(_ context.Context, partition int32) (int64, error) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Step 1: log-end offset via Client.GetOffset.
	// sarama.OffsetNewest returns the offset of the next message to be written,
	// i.e. one past the last committed message — exactly what we need.
	latestOffset, err := lc.client.GetOffset(TopicBetPlaced, partition, sarama.OffsetNewest)
	if err != nil {
		return 0, fmt.Errorf("GetOffset: %w", err)
	}

	// Step 2: committed offset for the Odds Management consumer group.
	offsets, err := lc.admin.ListConsumerGroupOffsets(
		OddsManagementConsumerGroup,
		map[string][]int32{TopicBetPlaced: {partition}},
	)
	if err != nil {
		return 0, fmt.Errorf("ListConsumerGroupOffsets: %w", err)
	}
	block := offsets.GetBlock(TopicBetPlaced, partition)
	if block == nil {
		// Group has never committed — treat as maximally lagging.
		return latestOffset, nil
	}
	if block.Err != sarama.ErrNoError {
		return 0, fmt.Errorf("committed offset block error: %v", block.Err)
	}

	committedOffset := block.Offset // next offset the group will fetch
	lag := latestOffset - committedOffset
	if lag < 0 {
		lag = 0
	}
	return lag, nil
}

// Close releases the underlying Kafka connections.
// ClusterAdminFromClient shares the client connection, so closing admin is sufficient;
// closing the client separately would double-close — admin.Close handles both.
func (lc *LagChecker) Close() error {
	return lc.admin.Close()
}

// PartitionForMarket maps a market ID to its Kafka partition number using the
// same modulo hash applied by producers. numPartitions must match the
// configured partition count for the bet.placed topic.
func PartitionForMarket(marketID string, numPartitions int32) int32 {
	// Simple FNV-1a hash — must match producer partitioner config.
	var h uint32 = 2166136261
	for i := 0; i < len(marketID); i++ {
		h ^= uint32(marketID[i])
		h *= 16777619
	}
	return int32(h) % numPartitions
}
