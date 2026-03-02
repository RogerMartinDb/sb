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
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
	"github.com/sportsbook/sb/internal/betacceptance"
	"github.com/sportsbook/sb/internal/identity"
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
	cfg := betacceptance.DefaultConfig()

	// ── DB (PgBouncer transaction mode) ──────────────────────────────────────
	dbURL := envOr("BET_ACCEPTANCE_DB_URL", "postgres://sb:sb_secret@localhost:6432/db_bet_acceptance?sslmode=disable&default_query_exec_mode=simple_protocol")
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer db.Close()

	// ── Redis instances ───────────────────────────────────────────────────────
	redisSession := redis.NewClient(&redis.Options{Addr: envOr("REDIS_SESSION_ADDR", "localhost:6379")})
	redisOdds := redis.NewClient(&redis.Options{Addr: envOr("REDIS_ODDS_ADDR", "localhost:6380")})
	redisMarket := redis.NewClient(&redis.Options{Addr: envOr("REDIS_MARKET_ADDR", "localhost:6381")})
	redisRL := redis.NewClient(&redis.Options{Addr: envOr("REDIS_RATELIMIT_ADDR", "localhost:6382")})

	// ── Kafka lag checker ─────────────────────────────────────────────────────
	kafkaBrokers := []string{envOr("KAFKA_BROKERS", "localhost:9092")}
	lagChecker, err := betacceptance.NewLagChecker(kafkaBrokers, redisRL)
	if err != nil {
		return fmt.Errorf("lag checker: %w", err)
	}
	defer lagChecker.Close()

	// ── gRPC client for Account & Wallet ──────────────────────────────────────
	walletAddr := envOr("WALLET_GRPC_ADDR", "localhost:50051")
	walletConn, err := grpc.NewClient(walletAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("wallet grpc: %w", err)
	}
	defer walletConn.Close()
	walletClient := sbv1.NewWalletServiceClient(walletConn)

	// ── Bet flow ──────────────────────────────────────────────────────────────
	betFlow := betacceptance.NewBetFlow(
		cfg, db,
		redisSession, redisOdds, redisMarket, redisRL,
		walletClient, lagChecker, logger,
	)

	// ── Outbox relay ──────────────────────────────────────────────────────────
	hostname, _ := os.Hostname()
	relay, err := betacceptance.NewOutboxRelay(db, kafkaBrokers, "bet-acceptance-"+hostname, logger)
	if err != nil {
		return fmt.Errorf("outbox relay: %w", err)
	}
	defer relay.Close()
	go relay.Run(ctx)

	// ── HTTP server ───────────────────────────────────────────────────────────
	jwtSecret := []byte(envOr("JWT_SECRET", "dev-secret-change-in-production"))
	identitySvc := identity.NewService(nil, nil, jwtSecret, logger)

	mux := http.NewServeMux()
	mux.Handle("POST /bets", jwtMiddleware(identitySvc, makePlaceBetHandler(betFlow, logger)))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         envOr("HTTP_ADDR", ":8080"),
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

	logger.Info("bet acceptance service starting", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http serve: %w", err)
	}
	return nil
}

// placeBetHTTPRequest is the JSON body expected from the API Gateway.
type placeBetHTTPRequest struct {
	MarketID         string `json:"market_id"`
	SelectionID      string `json:"selection_id"`
	RequestedOddsNum int64  `json:"odds_num"`
	RequestedOddsDen int64  `json:"odds_den"`
	StakeMinor       int64  `json:"stake_minor"`
	Currency         string `json:"currency"`
}

func makePlaceBetHandler(flow *betacceptance.BetFlow, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// API Gateway sets X-User-ID and X-Idempotency-Key after JWT validation.
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			http.Error(w, `{"error":"missing X-User-ID"}`, http.StatusUnauthorized)
			return
		}
		idemKey := r.Header.Get("Idempotency-Key")
		if idemKey == "" {
			http.Error(w, `{"error":"Idempotency-Key header required"}`, http.StatusBadRequest)
			return
		}

		var body placeBetHTTPRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		resp, err := flow.PlaceBet(r.Context(), betacceptance.PlaceBetRequest{
			IdempotencyKey:   idemKey,
			UserID:           userID,
			MarketID:         body.MarketID,
			SelectionID:      body.SelectionID,
			RequestedOddsNum: body.RequestedOddsNum,
			RequestedOddsDen: body.RequestedOddsDen,
			StakeMinor:       body.StakeMinor,
			Currency:         body.Currency,
		})
		if err != nil {
			httpErr := mapBetFlowError(err)
			logger.Warn("place bet rejected", "user_id", userID, "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(httpErr.code)
			_ = json.NewEncoder(w).Encode(httpErr)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bet_id":    resp.BetID,
			"status":    resp.Status,
			"odds_num":  resp.OddsNum,
			"odds_den":  resp.OddsDen,
			"stake":     resp.Stake,
			"currency":  resp.Currency,
			"placed_at": resp.PlacedAt.Format(time.RFC3339Nano),
		})
	}
}

type httpError struct {
	code    int
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func mapBetFlowError(err error) httpError {
	switch {
	case errors.Is(err, betacceptance.ErrMarketNotOpen):
		return httpError{code: http.StatusConflict, Error: "MARKET_NOT_OPEN"}
	case errors.Is(err, betacceptance.ErrOddsMoved):
		return httpError{code: http.StatusConflict, Error: "ODDS_CHANGED", Message: err.Error()}
	case errors.Is(err, betacceptance.ErrLimitExceeded):
		return httpError{code: http.StatusUnprocessableEntity, Error: "LIMIT_EXCEEDED", Message: err.Error()}
	case errors.Is(err, betacceptance.ErrOddsNotSettled):
		return httpError{code: http.StatusConflict, Error: "ODDS_NOT_SETTLED"}
	case errors.Is(err, betacceptance.ErrInsufficientFunds):
		return httpError{code: http.StatusPaymentRequired, Error: "INSUFFICIENT_FUNDS"}
	default:
		return httpError{code: http.StatusInternalServerError, Error: "INTERNAL_ERROR"}
	}
}

func jwtMiddleware(svc *identity.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
			return
		}
		claims, err := svc.ValidateToken(strings.TrimPrefix(auth, "Bearer "))
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		r.Header.Set("X-User-ID", claims.UserID)
		next.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
