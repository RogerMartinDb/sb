package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
	"github.com/sportsbook/sb/internal/identity"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type claimsKey struct{}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	jwtSecret := []byte(envOr("JWT_SECRET", "dev-secret-change-in-production"))
	walletAddr := envOr("WALLET_GRPC_ADDR", "localhost:50051")
	listenAddr := envOr("CASHIER_ADDR", ":8085")

	conn, err := grpc.NewClient(walletAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("connect wallet", "err", err)
		os.Exit(1)
	}
	defer conn.Close()
	walletClient := sbv1.NewWalletServiceClient(conn)

	identitySvc := identity.NewService(nil, nil, jwtSecret, logger)

	jwtMw := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, `{"error":"UNAUTHORIZED"}`, http.StatusUnauthorized)
				return
			}
			claims, err := identitySvc.ValidateToken(strings.TrimPrefix(auth, "Bearer "))
			if err != nil {
				http.Error(w, `{"error":"UNAUTHORIZED"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next(w, r.WithContext(ctx))
		}
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /cashier/balance", jwtMw(func(w http.ResponseWriter, r *http.Request) {
		claims := r.Context().Value(claimsKey{}).(*identity.Claims)
		resp, err := walletClient.GetBalance(r.Context(), &sbv1.GetBalanceRequest{UserId: claims.UserID})
		if err != nil {
			http.Error(w, `{"error":"INTERNAL"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"available_minor": resp.Available.AmountMinor,
			"currency":        resp.Available.Currency,
		})
	}))

	mux.HandleFunc("POST /cashier/deposit", jwtMw(func(w http.ResponseWriter, r *http.Request) {
		claims := r.Context().Value(claimsKey{}).(*identity.Claims)
		var body struct {
			AmountDollars float64 `json:"amount_dollars"`
			PaymentMethod string  `json:"payment_method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AmountDollars <= 0 {
			http.Error(w, `{"error":"INVALID_REQUEST"}`, http.StatusBadRequest)
			return
		}
		amountMinor := int64(math.Round(body.AmountDollars * 100))
		txID := uuid.New().String()
		resp, err := walletClient.Deposit(r.Context(), &sbv1.DepositRequest{
			TransactionId: txID,
			UserId:        claims.UserID,
			Amount:        &sbv1.Money{AmountMinor: amountMinor, Currency: "USD"},
			PaymentMethod: body.PaymentMethod,
		})
		if err != nil {
			logger.Error("deposit", "err", err)
			http.Error(w, `{"error":"INTERNAL"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"transaction_id":  resp.TransactionId,
			"status":          resp.Status.String(),
			"available_after": resp.AvailableAfter.AmountMinor,
			"currency":        resp.AvailableAfter.Currency,
		})
	}))

	mux.HandleFunc("POST /cashier/withdraw", jwtMw(func(w http.ResponseWriter, r *http.Request) {
		claims := r.Context().Value(claimsKey{}).(*identity.Claims)
		var body struct {
			AmountDollars float64 `json:"amount_dollars"`
			PaymentMethod string  `json:"payment_method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AmountDollars <= 0 {
			http.Error(w, `{"error":"INVALID_REQUEST"}`, http.StatusBadRequest)
			return
		}
		amountMinor := int64(math.Round(body.AmountDollars * 100))
		txID := uuid.New().String()
		resp, err := walletClient.Withdraw(r.Context(), &sbv1.WithdrawRequest{
			TransactionId: txID,
			UserId:        claims.UserID,
			Amount:        &sbv1.Money{AmountMinor: amountMinor, Currency: "USD"},
			PaymentMethod: body.PaymentMethod,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				json.NewEncoder(w).Encode(map[string]any{"error": "INSUFFICIENT_FUNDS"}) //nolint:errcheck
				return
			}
			logger.Error("withdraw", "err", err)
			http.Error(w, `{"error":"INTERNAL"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"transaction_id":  resp.TransactionId,
			"status":          resp.Status.String(),
			"available_after": resp.AvailableAfter.AmountMinor,
			"currency":        resp.AvailableAfter.Currency,
		})
	}))

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("cashier listening", "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	logger.Info("cashier stopped")
	_ = fmt.Sprintf("") // keep fmt import
}
