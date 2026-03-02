package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/sportsbook/sb/internal/settlement"
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
	dbURL := envOr("SETTLEMENT_DB_URL", "postgres://sb:sb_secret@localhost:5437/db_settlement")
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer db.Close()

	walletAddr := envOr("WALLET_GRPC_ADDR", "localhost:50051")
	walletConn, err := grpc.NewClient(walletAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("wallet grpc: %w", err)
	}
	defer walletConn.Close()

	kafkaBrokers := []string{envOr("KAFKA_BROKERS", "localhost:9092")}

	// Producer for bet.settled and bet.voided events.
	producerCfg := sarama.NewConfig()
	producerCfg.Version = sarama.V3_6_0_0
	producerCfg.Producer.RequiredAcks = sarama.WaitForAll
	producerCfg.Producer.Return.Successes = true
	producerCfg.Producer.Return.Errors = true
	producer, err := sarama.NewSyncProducer(kafkaBrokers, producerCfg)
	if err != nil {
		return fmt.Errorf("kafka producer: %w", err)
	}
	defer producer.Close()

	consumer := settlement.NewConsumer(db, walletConn, producer, logger)

	logger.Info("settlement service starting")
	return consumer.Run(ctx, kafkaBrokers)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
