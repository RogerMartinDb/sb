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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

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

	feeds := []marketdata.ProviderFeed{
		marketdata.NewSportradarFeed(sportradarURL, sportradarKey, logger),
	}
	normaliser := &marketdata.SportradarNormaliser{}

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
