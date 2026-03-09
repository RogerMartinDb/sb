package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sportsbook/sb/internal/marketdata"
)

func main() {
	logLevel := slog.LevelInfo
	if envOr("LOG_LEVEL", "") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	kafkaBrokers := []string{envOr("KAFKA_BROKERS", "localhost:9092")}
	sportradarURL := envOr("SPORTRADAR_API_URL", "https://api.sportradar.com")
	sportradarKey := envOr("SPORTRADAR_API_KEY", "")

	// Shared event matcher: populated by Polymarket feed, consumed by NBA score feed.
	eventMatcher := marketdata.NewEventMatcher()

	feeds := []marketdata.ProviderFeed{
		marketdata.NewSportradarFeed(sportradarURL, sportradarKey, logger),
		marketdata.NewPolymarketFeed(eventMatcher, logger),
		marketdata.NewNBAScoreFeed(eventMatcher, logger),
	}

	normaliser := marketdata.NewCompositeNormaliser(map[string]marketdata.Normaliser{
		"sportradar": &marketdata.SportradarNormaliser{},
		"polymarket": &marketdata.PolymarketNormaliser{},
		"nba-scores": &marketdata.NBAScoreNormaliser{},
	})

	svc, err := marketdata.NewIngestionService(feeds, normaliser, kafkaBrokers, logger)
	if err != nil {
		return fmt.Errorf("ingestion service: %w", err)
	}
	defer svc.Close()

	logger.Info("market data ingestion service starting")
	return svc.Run(ctx)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
