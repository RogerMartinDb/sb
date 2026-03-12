package betacceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
)

// ── Domain errors ─────────────────────────────────────────────────────────────

var (
	ErrMarketNotOpen     = errors.New("market is not OPEN")
	ErrOddsMoved         = errors.New("odds have moved beyond accepted tolerance")
	ErrLimitExceeded     = errors.New("stake exceeds user limit")
	ErrOddsNotSettled    = errors.New("ODDS_NOT_SETTLED: lag detected on odds-management consumer group")
	ErrInsufficientFunds = errors.New("insufficient available balance")
)

// ── Configuration ─────────────────────────────────────────────────────────────

// Config holds tunable parameters for the bet flow.
type Config struct {
	// OddsTolerance is the maximum fractional movement in odds before we reject.
	// e.g. 0.05 = 5% movement allowed.
	OddsTolerance float64

	// LargeStakeThreshold: bets at or above this amount trigger the lag check.
	LargeStakeThreshold int64 // minor units

	// KafkaNumPartitions must match the partition count of the bet.placed topic.
	KafkaNumPartitions int32

	// MarketStatusTTL is the Redis TTL for market status cache entries.
	MarketStatusTTL time.Duration

	// OddsCacheTTL is the Redis TTL for odds cache entries.
	OddsCacheTTL time.Duration
}

// DefaultConfig returns sane defaults aligned with the architecture spec.
func DefaultConfig() Config {
	return Config{
		OddsTolerance:       0.05,
		LargeStakeThreshold: 10_000, // $100.00
		KafkaNumPartitions:  24,
		MarketStatusTTL:     5 * time.Second,
		OddsCacheTTL:        30 * time.Second,
	}
}

// ── Request / Response types ──────────────────────────────────────────────────

// PlaceBetRequest is the parsed, validated HTTP request body.
type PlaceBetRequest struct {
	IdempotencyKey string
	UserID         string
	MarketID       string
	MarketName     string
	SelectionID    string
	SelectionName  string
	// RequestedOdds is the odds the client saw when placing the bet.
	RequestedOddsDecimal  float64
	RequestedOddsAmerican int
	StakeMinor            int64  // stake in minor units (e.g. pence)
	Currency              string
}

// PlaceBetResponse is returned synchronously.
type PlaceBetResponse struct {
	BetID        string
	Status       string // "ACCEPTED"
	OddsDecimal  float64
	OddsAmerican int
	Stake        int64
	Currency     string
	PlacedAt     time.Time
}

// ── Cached odds entry ─────────────────────────────────────────────────────────

// CachedOdds is stored as JSON in Redis odds cache.
// source_event_offset is the Kafka offset of the odds.updated event that
// produced this entry — used as a secondary consistency signal.
type CachedOdds struct {
	Decimal           float64 `json:"decimal"`
	American          int     `json:"american"`
	SourceEventOffset int64   `json:"src_offset"`
}

// ── BetFlow ───────────────────────────────────────────────────────────────────

// BetFlow orchestrates the synchronous bet placement steps 1–9 as defined in
// the architecture spec.
type BetFlow struct {
	cfg           Config
	db            *pgxpool.Pool
	redisSession  *redis.Client
	redisOdds     *redis.Client
	redisMarket   *redis.Client
	redisRateLimit *redis.Client
	walletClient  sbv1.WalletServiceClient
	lagChecker    *LagChecker
	logger        *slog.Logger
}

// NewBetFlow constructs a BetFlow with all dependencies injected.
func NewBetFlow(
	cfg Config,
	db *pgxpool.Pool,
	redisSession, redisOdds, redisMarket, redisRateLimit *redis.Client,
	walletClient sbv1.WalletServiceClient,
	lagChecker *LagChecker,
	logger *slog.Logger,
) *BetFlow {
	return &BetFlow{
		cfg:            cfg,
		db:             db,
		redisSession:   redisSession,
		redisOdds:      redisOdds,
		redisMarket:    redisMarket,
		redisRateLimit: redisRateLimit,
		walletClient:   walletClient,
		lagChecker:     lagChecker,
		logger:         logger,
	}
}

// PlaceBet executes the full synchronous bet flow and returns before any Kafka
// publish occurs. The Kafka publish happens async via the outbox relay.
//
// Steps (mirroring architecture spec):
//  1. Validate session (Redis) → user_id, kyc_status
//  2. Check market status (Redis, 5s TTL) → must be OPEN
//  3. Read current odds (Redis, 30s TTL) → verify within tolerance
//  4. Check user limits (gRPC → Account & Wallet)
//  5. Lag check (if large stake)
//  6. Write outbox row PENDING
//  7. gRPC DeductBalance → Account & Wallet
//  8. Mark outbox row READY_TO_PUBLISH
//  9. Return 200 {bet_id, status: ACCEPTED, odds, stake}
func (f *BetFlow) PlaceBet(ctx context.Context, req PlaceBetRequest) (*PlaceBetResponse, error) {
	// ── Step 1: Idempotency check ─────────────────────────────────────────────
	idemKey := fmt.Sprintf("idem:%s", req.IdempotencyKey)
	existingBetID, err := f.redisRateLimit.Get(ctx, idemKey).Result()
	if err == nil {
		// Already processed — return idempotent response.
		f.logger.Info("bet_flow: idempotent replay", "bet_id", existingBetID)
		return &PlaceBetResponse{
			BetID:        existingBetID,
			Status:       "ACCEPTED",
			OddsDecimal:  req.RequestedOddsDecimal,
			OddsAmerican: req.RequestedOddsAmerican,
			Stake:        req.StakeMinor,
			Currency:     req.Currency,
			PlacedAt:     time.Now(),
		}, nil
	}

	// ── Step 2: Check market status (Redis, 5s TTL) ───────────────────────────
	marketKey := fmt.Sprintf("market:status:%s", req.MarketID)
	marketStatus, err := f.redisMarket.Get(ctx, marketKey).Result()
	if err != nil {
		return nil, fmt.Errorf("bet_flow: market status cache miss for %s: %w", req.MarketID, err)
	}
	if marketStatus != "OPEN" {
		return nil, fmt.Errorf("%w: market %s status=%s", ErrMarketNotOpen, req.MarketID, marketStatus)
	}

	// ── Step 3: Read and validate current odds ────────────────────────────────
	oddsKey := fmt.Sprintf("odds:%s:%s", req.MarketID, req.SelectionID)
	oddsRaw, err := f.redisOdds.Get(ctx, oddsKey).Bytes()
	if err != nil {
		return nil, fmt.Errorf("bet_flow: odds cache miss for %s/%s: %w", req.MarketID, req.SelectionID, err)
	}
	var currentOdds CachedOdds
	if err := json.Unmarshal(oddsRaw, &currentOdds); err != nil {
		return nil, fmt.Errorf("bet_flow: corrupt odds cache entry: %w", err)
	}
	if err := validateOdds(req, currentOdds, f.cfg.OddsTolerance); err != nil {
		return nil, err
	}

	// ── Step 4: Check user limits (gRPC → Account & Wallet) ──────────────────
	limitsResp, err := f.walletClient.GetUserLimits(ctx, &sbv1.GetUserLimitsRequest{
		UserId: req.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("bet_flow: GetUserLimits: %w", err)
	}
	if err := checkLimits(req, limitsResp); err != nil {
		return nil, err
	}

	// ── Step 5: Lag check (only for large stakes) ─────────────────────────────
	if req.StakeMinor >= f.cfg.LargeStakeThreshold {
		partition := PartitionForMarket(req.MarketID, f.cfg.KafkaNumPartitions)
		lagging, lagErr := f.lagChecker.IsLagging(ctx, partition)
		if lagErr != nil {
			f.logger.Error("bet_flow: lag check failed (fail-closed)", "partition", partition, "err", lagErr)
			return nil, fmt.Errorf("%w: lag check error: %v", ErrOddsNotSettled, lagErr)
		}
		if lagging {
			return nil, ErrOddsNotSettled
		}
	}

	// ── Step 6: Write outbox row (status=PENDING) ─────────────────────────────
	betID := uuid.New().String()
	transactionID := uuid.New().String()
	payload, err := buildBetPlacedPayload(betID, req, currentOdds)
	if err != nil {
		return nil, fmt.Errorf("bet_flow: build payload: %w", err)
	}

	if err := f.writeOutboxPending(ctx, betID, req.MarketID, payload); err != nil {
		return nil, fmt.Errorf("bet_flow: write outbox pending: %w", err)
	}

	// ── Step 7: gRPC DeductBalance → Account & Wallet ─────────────────────────
	deductResp, err := f.walletClient.DeductBalance(ctx, &sbv1.DeductBalanceRequest{
		TransactionId: transactionID,
		UserId:        req.UserID,
		Stake: &sbv1.Money{
			AmountMinor: req.StakeMinor,
			Currency:    req.Currency,
		},
		BetId: betID,
	})
	if err != nil {
		// Cancel the outbox row and surface the error to the caller.
		_ = f.cancelOutbox(ctx, betID)
		if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition {
			return nil, ErrInsufficientFunds
		}
		return nil, fmt.Errorf("bet_flow: DeductBalance: %w", err)
	}
	_ = deductResp

	// ── Step 8: Mark outbox row READY_TO_PUBLISH ──────────────────────────────
	if err := f.markOutboxReady(ctx, betID); err != nil {
		// Deduction succeeded but outbox update failed. Log loudly — the
		// reconciliation job will catch PENDING_CONFIRMATION ledger rows.
		f.logger.Error("bet_flow: CRITICAL — DeductBalance succeeded but outbox update failed",
			"bet_id", betID, "transaction_id", transactionID, "err", err)
		// Still return success to the client — the bet is accepted and funds reserved.
	}

	// ── Step 9: Store idempotency key and return ──────────────────────────────
	_ = f.redisRateLimit.Set(ctx, idemKey, betID, 24*time.Hour).Err()

	f.logger.Info("bet_flow: bet accepted", "bet_id", betID, "user_id", req.UserID,
		"market_id", req.MarketID, "stake_minor", req.StakeMinor)

	return &PlaceBetResponse{
		BetID:        betID,
		Status:       "ACCEPTED",
		OddsDecimal:  currentOdds.Decimal,
		OddsAmerican: currentOdds.American,
		Stake:        req.StakeMinor,
		Currency:     req.Currency,
		PlacedAt:     time.Now(),
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func validateOdds(req PlaceBetRequest, current CachedOdds, tolerance float64) error {
	if current.Decimal <= 0 {
		return fmt.Errorf("bet_flow: invalid cached decimal odds: %.4f", current.Decimal)
	}
	movement := abs64(req.RequestedOddsDecimal-current.Decimal) / current.Decimal
	if movement > tolerance {
		return fmt.Errorf("%w: requested %.4f, current %.4f, movement %.2f%%",
			ErrOddsMoved, req.RequestedOddsDecimal, current.Decimal, movement*100)
	}
	return nil
}

func checkLimits(req PlaceBetRequest, limits *sbv1.GetUserLimitsResponse) error {
	if limits.MaxSingleStake != nil && req.StakeMinor > limits.MaxSingleStake.AmountMinor {
		return fmt.Errorf("%w: stake %d exceeds max single stake %d",
			ErrLimitExceeded, req.StakeMinor, limits.MaxSingleStake.AmountMinor)
	}
	if limits.DailyLimit != nil && limits.DailyStakedSoFar != nil {
		remaining := limits.DailyLimit.AmountMinor - limits.DailyStakedSoFar.AmountMinor
		if req.StakeMinor > remaining {
			return fmt.Errorf("%w: stake %d exceeds daily limit remaining %d",
				ErrLimitExceeded, req.StakeMinor, remaining)
		}
	}
	return nil
}

func buildBetPlacedPayload(betID string, req PlaceBetRequest, odds CachedOdds) (json.RawMessage, error) {
	event := map[string]any{
		"event_type":     "bet.placed",
		"bet_id":         betID,
		"user_id":        req.UserID,
		"market_id":      req.MarketID,
		"market_name":    req.MarketName,
		"selection_id":   req.SelectionID,
		"selection_name": req.SelectionName,
		"odds_decimal":   odds.Decimal,
		"odds_american":  odds.American,
		"stake_minor":    req.StakeMinor,
		"currency":       req.Currency,
		"placed_at":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	return json.Marshal(event)
}

func (f *BetFlow) writeOutboxPending(ctx context.Context, betID, marketID string, payload json.RawMessage) error {
	_, err := f.db.Exec(ctx, `
		INSERT INTO outbox (id, bet_id, topic, partition_key, payload, status, created_at)
		VALUES             ($1,  $2,   $3,    $4,            $5,      $6,    NOW())`,
		uuid.New().String(), betID, "bet.placed", marketID, payload, OutboxStatusPending,
	)
	return err
}

func (f *BetFlow) markOutboxReady(ctx context.Context, betID string) error {
	_, err := f.db.Exec(ctx,
		`UPDATE outbox SET status = $1 WHERE bet_id = $2 AND status = $3`,
		OutboxStatusReadyToPublish, betID, OutboxStatusPending,
	)
	return err
}

func (f *BetFlow) cancelOutbox(ctx context.Context, betID string) error {
	_, err := f.db.Exec(ctx,
		`UPDATE outbox SET status = $1 WHERE bet_id = $2`,
		OutboxStatusCancelled, betID,
	)
	return err
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
