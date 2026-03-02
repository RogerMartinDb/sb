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
	"github.com/redis/go-redis/v9"

	"github.com/sportsbook/sb/internal/identity"
)

const minPasswordLen = 8

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
	dbURL := envOr("IDENTITY_DB_URL", "postgres://sb:sb_secret@localhost:5438/db_identity")
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer db.Close()

	redisSession := redis.NewClient(&redis.Options{Addr: envOr("REDIS_SESSION_ADDR", "localhost:6379")})
	jwtSecret := []byte(envOr("JWT_SECRET", "dev-secret-change-in-production"))

	svc := identity.NewService(db, redisSession, jwtSecret, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		resp, err := svc.Login(r.Context(), identity.LoginRequest{
			Email:    req.Email,
			Password: req.Password,
		})
		if err != nil {
			http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": resp.AccessToken,
			"expires_at":   resp.ExpiresAt.Format(time.RFC3339),
		})
	})

	mux.HandleFunc("POST /auth/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if req.Email == "" || len(req.Password) < minPasswordLen {
			http.Error(w, `{"error":"email required and password must be at least 8 characters"}`, http.StatusBadRequest)
			return
		}
		resp, err := svc.Register(r.Context(), identity.RegisterRequest{
			Email:    req.Email,
			Password: req.Password,
		})
		if errors.Is(err, identity.ErrEmailTaken) {
			http.Error(w, `{"error":"email already registered"}`, http.StatusConflict)
			return
		}
		if err != nil {
			logger.Error("register failed", "err", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": resp.AccessToken,
			"expires_at":   resp.ExpiresAt.Format(time.RFC3339),
		})
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         envOr("HTTP_ADDR", ":8084"),
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

	logger.Info("identity service starting", "addr", srv.Addr)
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
