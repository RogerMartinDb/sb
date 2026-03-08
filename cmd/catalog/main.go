package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
	"github.com/sportsbook/sb/internal/catalog"
	"github.com/sportsbook/sb/internal/polymarket"
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
	dbURL := envOr("CATALOG_DB_URL", "postgres://sb:sb_secret@localhost:5435/db_catalog")
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer db.Close()

	redisMarket := redis.NewClient(&redis.Options{Addr: envOr("REDIS_MARKET_ADDR", "localhost:6381")})
	redisOdds := redis.NewClient(&redis.Options{Addr: envOr("REDIS_ODDS_ADDR", "localhost:6380")})
	kafkaBrokers := []string{envOr("KAFKA_BROKERS", "localhost:9092")}

	svc := catalog.NewService(db, redisMarket, logger)

	grpcServer := grpc.NewServer()
	sbv1.RegisterCatalogServiceServer(grpcServer, svc)

	// Create Kafka producer for Polymarket syncer to publish price.update events.
	kafkaCfg := sarama.NewConfig()
	kafkaCfg.Version = sarama.V3_6_0_0
	kafkaCfg.Producer.RequiredAcks = sarama.WaitForAll
	kafkaCfg.Producer.Return.Successes = true
	kafkaCfg.Producer.Return.Errors = true
	producer, err := sarama.NewSyncProducer(kafkaBrokers, kafkaCfg)
	if err != nil {
		return fmt.Errorf("kafka producer: %w", err)
	}
	defer producer.Close()

	// Run Kafka consumer in background.
	go func() {
		if err := catalog.ConsumeNormalisedFeed(ctx, kafkaBrokers, db, logger); err != nil {
			logger.Error("catalog feed consumer error", "err", err)
		}
	}()

	// Run Polymarket syncer in background (with Kafka producer).
	go polymarket.NewSyncer(db, producer, logger).Run(ctx)

	// Run status cache warmer in background.
	go catalog.NewStatusCacheWarmer(db, redisMarket, logger).Run(ctx)

	// Start HTTP server for /events endpoint.
	httpHandler := catalog.NewHTTPHandler(db, redisOdds, logger)
	httpAddr := envOr("HTTP_ADDR", ":8086")
	httpServer := &http.Server{Addr: httpAddr, Handler: httpHandler.Mux()}
	go func() {
		logger.Info("catalog HTTP server starting", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("catalog HTTP server error", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		httpServer.Close()
	}()

	// Start gRPC server.
	addr := envOr("GRPC_ADDR", ":50052")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("catalog service starting", "grpc_addr", addr, "http_addr", httpAddr)
	return grpcServer.Serve(lis)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
