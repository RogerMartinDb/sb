package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/sportsbook/sb/internal/odds"
	"github.com/sportsbook/sb/internal/oddsmanagement"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	dbURL := envOr("ODDS_DB_URL", "postgres://sb:sb_secret@localhost:5434/db_odds")
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer db.Close()

	redisOdds := redis.NewClient(&redis.Options{Addr: envOr("REDIS_ODDS_ADDR", "localhost:6380")})
	kafkaBrokers := []string{envOr("KAFKA_BROKERS", "localhost:9092")}

	// ── Odds Management main consumer ─────────────────────────────────────────
	svc, err := oddsmanagement.NewService(db, kafkaBrokers, logger)
	if err != nil {
		return fmt.Errorf("odds management service: %w", err)
	}
	defer svc.Close()

	// ── Redis cache updater goroutine ─────────────────────────────────────────
	// Dedicated consumer group (odds-cache-updater-cg) — independent of odds-management-cg.
	cacheUpdater, err := odds.NewCacheUpdater(kafkaBrokers, redisOdds, logger)
	if err != nil {
		return fmt.Errorf("cache updater: %w", err)
	}
	defer cacheUpdater.Close()

	errCh := make(chan error, 2)

	go func() {
		errCh <- svc.RunConsumer(ctx, kafkaBrokers)
	}()
	go func() {
		errCh <- cacheUpdater.Run(ctx)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
