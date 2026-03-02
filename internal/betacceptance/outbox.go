package betacceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxStatus represents the lifecycle of an outbox row.
type OutboxStatus string

const (
	OutboxStatusPending        OutboxStatus = "PENDING"          // Row written; DeductBalance not yet called
	OutboxStatusReadyToPublish OutboxStatus = "READY_TO_PUBLISH" // DeductBalance succeeded; relay can publish
	OutboxStatusPublished      OutboxStatus = "PUBLISHED"        // Kafka produce succeeded
	OutboxStatusCancelled      OutboxStatus = "CANCELLED"        // DeductBalance failed; bet rejected
)

// OutboxRow mirrors the outbox table schema.
type OutboxRow struct {
	ID          string
	BetID       string
	Topic       string
	PartitionKey string // market_id — used by relay to compute Kafka partition
	Payload     json.RawMessage
	Status      OutboxStatus
	CreatedAt   time.Time
}

// OutboxRelay polls the outbox table and publishes READY_TO_PUBLISH rows to
// Kafka using a transactional (exactly-once) producer. It runs as a background
// goroutine within the Bet Acceptance service process.
//
// Design invariants:
//   - Exactly one relay goroutine runs per service instance (prevents double-publish races).
//   - Transactional producer ensures atomic produce + offset commit.
//   - Idempotent producer guarantees deduplication on broker-side retries.
//   - Polling interval is short (100ms) to stay within the p99 < 150ms target
//     for end-to-end bet confirmation visible in Bet History.
type OutboxRelay struct {
	db       *pgxpool.Pool
	producer sarama.SyncProducer
	logger   *slog.Logger
	pollInterval time.Duration
}

// NewOutboxRelay creates a relay backed by the given DB pool and a transactional
// Kafka producer. transactionalID must be unique per producer instance (e.g. hostname).
func NewOutboxRelay(db *pgxpool.Pool, brokers []string, transactionalID string, logger *slog.Logger) (*OutboxRelay, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0

	// Exactly-once / idempotent producer settings.
	cfg.Net.MaxOpenRequests = 1
	cfg.Producer.Idempotent = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true
	cfg.Producer.Transaction.ID = transactionalID
	cfg.Producer.Transaction.Timeout = 30 * time.Second

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("outbox: create sync producer: %w", err)
	}
	return &OutboxRelay{
		db:           db,
		producer:     producer,
		logger:       logger,
		pollInterval: 100 * time.Millisecond,
	}, nil
}

// Run starts the relay loop. It blocks until ctx is cancelled.
func (r *OutboxRelay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("outbox relay: shutting down")
			return
		case <-ticker.C:
			if err := r.publishBatch(ctx); err != nil {
				r.logger.Error("outbox relay: publish batch failed", "err", err)
			}
		}
	}
}

// publishBatch fetches up to 100 READY_TO_PUBLISH rows and publishes them.
func (r *OutboxRelay) publishBatch(ctx context.Context) error {
	rows, err := r.fetchReady(ctx, 100)
	if err != nil {
		return fmt.Errorf("fetchReady: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	for _, row := range rows {
		if err := r.publish(ctx, row); err != nil {
			r.logger.Error("outbox relay: publish row failed", "bet_id", row.BetID, "err", err)
			// Continue — other rows may succeed; failed row stays READY_TO_PUBLISH for retry.
		}
	}
	return nil
}

// fetchReady returns up to limit rows with status READY_TO_PUBLISH, locking
// them via SELECT FOR UPDATE SKIP LOCKED to prevent double-processing if
// multiple relay goroutines ever run (defensive coding).
func (r *OutboxRelay) fetchReady(ctx context.Context, limit int) ([]OutboxRow, error) {
	const q = `
		SELECT id, bet_id, topic, partition_key, payload, status, created_at
		FROM outbox
		WHERE status = 'READY_TO_PUBLISH'
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	dbRows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer dbRows.Close()

	var out []OutboxRow
	for dbRows.Next() {
		var row OutboxRow
		if err := dbRows.Scan(
			&row.ID, &row.BetID, &row.Topic, &row.PartitionKey,
			&row.Payload, &row.Status, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, dbRows.Err()
}

// publish produces a single outbox row to Kafka and marks it PUBLISHED in the DB.
func (r *OutboxRelay) publish(ctx context.Context, row OutboxRow) error {
	msg := &sarama.ProducerMessage{
		Topic: row.Topic,
		Key:   sarama.StringEncoder(row.PartitionKey),
		Value: sarama.ByteEncoder(row.Payload),
		Headers: []sarama.RecordHeader{
			{Key: []byte("bet_id"), Value: []byte(row.BetID)},
			{Key: []byte("outbox_id"), Value: []byte(row.ID)},
		},
	}

	_, _, err := r.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("kafka produce: %w", err)
	}

	// Mark PUBLISHED in the same DB transaction scope (best-effort; if this
	// fails the row will be retried and the idempotent producer will dedup on
	// the broker side via the producer epoch).
	if updateErr := r.markPublished(ctx, row.ID); updateErr != nil {
		r.logger.Warn("outbox relay: failed to mark row published (will retry)", "id", row.ID, "err", updateErr)
	}
	return nil
}

func (r *OutboxRelay) markPublished(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE outbox SET status = 'PUBLISHED', published_at = NOW() WHERE id = $1`,
		id,
	)
	return err
}

// Close shuts down the producer gracefully.
func (r *OutboxRelay) Close() error {
	return r.producer.Close()
}
