package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/sportsbook/sb/internal/bethistory"
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
	dbURL := envOr("BET_HISTORY_DB_URL", "postgres://sb:sb_secret@localhost:5436/db_bet_history")
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

	consumer := bethistory.NewConsumer(db, walletConn, logger)
	kafkaBrokers := []string{envOr("KAFKA_BROKERS", "localhost:9092")}

	// Run Kafka consumer.
	go func() {
		if err := consumer.Run(ctx, kafkaBrokers); err != nil {
			logger.Error("bet history consumer error", "err", err)
		}
	}()

	// HTTP API for "My Bets" UI.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /bets", func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			http.Error(w, `{"error":"missing X-User-ID"}`, http.StatusUnauthorized)
			return
		}
		rows, err := db.Query(r.Context(), `
			SELECT bet_id, market_id, selection_id, odds_decimal, odds_american,
			       stake_minor, currency, status, placed_at, settled_at, payout_minor
			FROM bets WHERE user_id = $1 ORDER BY placed_at DESC LIMIT 50`,
			userID,
		)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var bets []map[string]any
		for rows.Next() {
			var (
				betID, marketID, selectionID, currency, status string
				oddsDecimal                                    float64
				oddsAmerican                                   int
				stakeMinor                                     int64
				placedAt                                       time.Time
				settledAt                                      *time.Time
				payoutMinor                                    *int64
			)
			if err := rows.Scan(&betID, &marketID, &selectionID, &oddsDecimal, &oddsAmerican,
				&stakeMinor, &currency, &status, &placedAt, &settledAt, &payoutMinor); err != nil {
				continue
			}
			b := map[string]any{
				"bet_id":        betID,
				"market_id":     marketID,
				"selection_id":  selectionID,
				"odds_decimal":  oddsDecimal,
				"odds_american": oddsAmerican,
				"stake_minor":   stakeMinor,
				"currency":      currency,
				"status":        status,
				"placed_at":     placedAt.Format(time.RFC3339),
			}
			if settledAt != nil {
				b["settled_at"] = settledAt.Format(time.RFC3339)
			}
			if payoutMinor != nil {
				b["payout_minor"] = *payoutMinor
			}
			bets = append(bets, b)
		}
		if bets == nil {
			bets = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"bets": bets})
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         envOr("HTTP_ADDR", ":8082"),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("bet history service starting", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http serve: %w", err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
