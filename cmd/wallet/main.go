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
	"google.golang.org/grpc"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
	"github.com/sportsbook/sb/internal/wallet"
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
	// Session pooling required (advisory locks incompatible with transaction pooling).
	dbURL := envOr("WALLET_DB_URL", "postgres://sb:sb_secret@localhost:5433/db_wallet")
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer db.Close()

	svc := wallet.NewService(db, logger)

	grpcServer := grpc.NewServer()
	sbv1.RegisterWalletServiceServer(grpcServer, svc)

	addr := envOr("GRPC_ADDR", ":50051")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("wallet service starting", "addr", addr)
	return grpcServer.Serve(lis)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
