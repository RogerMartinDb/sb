package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	statusCacheInterval = 3 * time.Second
	statusCacheTTL      = 5 * time.Second
)

// StatusCacheWarmer periodically writes market:status:{id} keys to Redis
// for all OPEN and SUSPENDED markets. This ensures bet acceptance can validate
// market status from cache even for freshly synced markets.
type StatusCacheWarmer struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	logger *slog.Logger
}

func NewStatusCacheWarmer(db *pgxpool.Pool, rdb *redis.Client, logger *slog.Logger) *StatusCacheWarmer {
	return &StatusCacheWarmer{db: db, rdb: rdb, logger: logger}
}

func (w *StatusCacheWarmer) Run(ctx context.Context) {
	w.logger.Info("status cache warmer starting")
	w.warm(ctx)

	ticker := time.NewTicker(statusCacheInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.warm(ctx)
		}
	}
}

func (w *StatusCacheWarmer) warm(ctx context.Context) {
	rows, err := w.db.Query(ctx,
		`SELECT market_id, status FROM markets WHERE status IN ('OPEN', 'SUSPENDED')`)
	if err != nil {
		w.logger.Error("status cache warmer: query failed", "err", err)
		return
	}
	defer rows.Close()

	pipe := w.rdb.Pipeline()
	count := 0
	for rows.Next() {
		var marketID, status string
		if err := rows.Scan(&marketID, &status); err != nil {
			w.logger.Error("status cache warmer: scan failed", "err", err)
			continue
		}
		pipe.Set(ctx, fmt.Sprintf("market:status:%s", marketID), status, statusCacheTTL)
		count++
	}
	if err := rows.Err(); err != nil {
		w.logger.Error("status cache warmer: rows error", "err", err)
		return
	}

	if count > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			w.logger.Error("status cache warmer: redis pipeline failed", "err", err)
		}
	}
}
