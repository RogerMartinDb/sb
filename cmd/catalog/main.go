package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
	"github.com/sportsbook/sb/internal/catalog"
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
	dbURL := envOr("CATALOG_DB_URL", "postgres://sb:sb_secret@localhost:5435/db_catalog")
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer db.Close()

	redisMarket := redis.NewClient(&redis.Options{Addr: envOr("REDIS_MARKET_ADDR", "localhost:6381")})
	kafkaBrokers := []string{envOr("KAFKA_BROKERS", "localhost:9092")}

	svc := catalog.NewService(db, redisMarket, logger)

	grpcServer := grpc.NewServer()
	sbv1.RegisterCatalogServiceServer(grpcServer, svc)

	// Run Kafka consumer in background.
	go func() {
		if err := catalog.ConsumeNormalisedFeed(ctx, kafkaBrokers, db, logger); err != nil {
			logger.Error("catalog feed consumer error", "err", err)
		}
	}()

	addr := envOr("GRPC_ADDR", ":50052")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("catalog service starting", "addr", addr)
	return grpcServer.Serve(lis)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
