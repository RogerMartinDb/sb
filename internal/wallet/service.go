// Package wallet implements the Account & Wallet gRPC service.
package wallet

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
)

// Service implements sbv1.WalletServiceServer.
// It owns the double-entry ledger in db_wallet.
//
// PgBouncer note: session pooling is used for this service because advisory
// locks are required for serialised balance updates (incompatible with
// transaction-mode pooling).
type Service struct {
	sbv1.UnimplementedWalletServiceServer
	db     *pgxpool.Pool
	logger *slog.Logger
}

func NewService(db *pgxpool.Pool, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger}
}

// GetUserLimits returns stake limits and KYC status for a user.
func (s *Service) GetUserLimits(ctx context.Context, req *sbv1.GetUserLimitsRequest) (*sbv1.GetUserLimitsResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id required")
	}

	var (
		maxSingleStakeMinor int64
		dailyLimitMinor     int64
		currency            string
		kycStatus           string
	)
	err := s.db.QueryRow(ctx, `
		SELECT u.max_single_stake_minor, u.daily_limit_minor, u.currency, u.kyc_status
		FROM user_limits u
		WHERE u.user_id = $1`, req.UserId,
	).Scan(&maxSingleStakeMinor, &dailyLimitMinor, &currency, &kycStatus)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "user not found: %v", err)
	}

	// Sum stakes placed in the rolling 24h window.
	var dailyStakedMinor int64
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(stake_minor), 0)
		FROM ledger_entries
		WHERE user_id = $1
		  AND entry_type = 'DEBIT'
		  AND created_at > NOW() - INTERVAL '24 hours'
		  AND status != 'CANCELLED'`,
		req.UserId,
	).Scan(&dailyStakedMinor)

	return &sbv1.GetUserLimitsResponse{
		MaxSingleStake: &sbv1.Money{AmountMinor: maxSingleStakeMinor, Currency: currency},
		DailyLimit:     &sbv1.Money{AmountMinor: dailyLimitMinor, Currency: currency},
		DailyStakedSoFar: &sbv1.Money{AmountMinor: dailyStakedMinor, Currency: currency},
		KycStatus:      kycStatusFromString(kycStatus),
	}, nil
}

// DeductBalance reserves stake for a pending bet using a double-entry ledger.
// Idempotent on transaction_id.
func (s *Service) DeductBalance(ctx context.Context, req *sbv1.DeductBalanceRequest) (*sbv1.DeductBalanceResponse, error) {
	if req.TransactionId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "transaction_id and user_id required")
	}

	// Idempotency: return existing result if transaction already processed.
	existing, err := s.getExistingTransaction(ctx, req.TransactionId)
	if err == nil {
		return existing, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Advisory lock on user_id to serialise concurrent balance deductions.
	// Compatible with session pooling only (see PgBouncer note on struct).
	var lockID int64
	if err := tx.QueryRow(ctx, `SELECT hashtext($1)`, req.UserId).Scan(&lockID); err != nil {
		return nil, status.Errorf(codes.Internal, "hash user_id: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		return nil, status.Errorf(codes.Internal, "advisory lock: %v", err)
	}

	// Read current balance.
	var availableMinor int64
	if err := tx.QueryRow(ctx,
		`SELECT available_minor FROM balances WHERE user_id = $1 FOR UPDATE`,
		req.UserId,
	).Scan(&availableMinor); err != nil {
		return nil, status.Errorf(codes.NotFound, "balance not found for user %s", req.UserId)
	}

	stake := req.Stake.GetAmountMinor()
	if availableMinor < stake {
		return nil, status.Errorf(codes.FailedPrecondition,
			"insufficient balance: available=%d stake=%d", availableMinor, stake)
	}

	// Double-entry: debit available_balance, credit bets_in_flight.
	if _, err := tx.Exec(ctx, `
		UPDATE balances
		SET available_minor   = available_minor   - $1,
		    bets_in_flight_minor = bets_in_flight_minor + $1,
		    updated_at        = NOW()
		WHERE user_id = $2`, stake, req.UserId,
	); err != nil {
		return nil, status.Errorf(codes.Internal, "update balances: %v", err)
	}

	// Record ledger entry.
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries
		    (transaction_id, user_id, entry_type, stake_minor, currency, status, bet_id, created_at)
		VALUES ($1, $2, 'DEBIT', $3, $4, 'PENDING_CONFIRMATION', $5, NOW())`,
		req.TransactionId, req.UserId, stake, req.Stake.GetCurrency(), req.BetId,
	); err != nil {
		return nil, status.Errorf(codes.Internal, "insert ledger entry: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, status.Errorf(codes.Internal, "commit: %v", err)
	}

	availableAfter := availableMinor - stake
	s.logger.Info("wallet: balance deducted",
		"user_id", req.UserId, "stake", stake, "available_after", availableAfter)

	return &sbv1.DeductBalanceResponse{
		TransactionId:  req.TransactionId,
		Status:         sbv1.LedgerEntryStatus_LEDGER_ENTRY_STATUS_PENDING_CONFIRMATION,
		AvailableAfter: &sbv1.Money{AmountMinor: availableAfter, Currency: req.Stake.GetCurrency()},
	}, nil
}

// CreditBalance credits winnings or refunds a voided stake.
func (s *Service) CreditBalance(ctx context.Context, req *sbv1.CreditBalanceRequest) (*sbv1.CreditBalanceResponse, error) {
	if req.TransactionId == "" || req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "transaction_id and user_id required")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	amount := req.Amount.GetAmountMinor()

	// Determine the column to debit on the bets_in_flight side.
	// On win/void, the stake was in bets_in_flight — release it.
	if _, err := tx.Exec(ctx, `
		UPDATE balances
		SET available_minor      = available_minor      + $1,
		    bets_in_flight_minor = bets_in_flight_minor - $2,
		    updated_at           = NOW()
		WHERE user_id = $3`,
		amount,                                   // credit amount (winnings or stake refund)
		req.Amount.GetAmountMinor(), // release the original stake portion
		req.UserId,
	); err != nil {
		return nil, status.Errorf(codes.Internal, "update balances: %v", err)
	}

	entryType := creditReasonToEntryType(req.Reason)
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries
		    (transaction_id, user_id, entry_type, stake_minor, currency, status, bet_id, created_at)
		VALUES ($1, $2, $3, $4, $5, 'CONFIRMED', $6, NOW())
		ON CONFLICT (transaction_id) DO NOTHING`,
		req.TransactionId, req.UserId, entryType,
		amount, req.Amount.GetCurrency(), req.OriginatingBetId,
	); err != nil {
		return nil, status.Errorf(codes.Internal, "insert ledger entry: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, status.Errorf(codes.Internal, "commit: %v", err)
	}

	var newAvailable int64
	_ = s.db.QueryRow(ctx, `SELECT available_minor FROM balances WHERE user_id = $1`, req.UserId).
		Scan(&newAvailable)

	return &sbv1.CreditBalanceResponse{
		TransactionId:  req.TransactionId,
		Status:         sbv1.LedgerEntryStatus_LEDGER_ENTRY_STATUS_CONFIRMED,
		AvailableAfter: &sbv1.Money{AmountMinor: newAvailable, Currency: req.Amount.GetCurrency()},
	}, nil
}

// ConfirmTransaction moves a ledger row from PENDING_CONFIRMATION → CONFIRMED.
// Called by Bet History Service after recording bet.placed.
func (s *Service) ConfirmTransaction(ctx context.Context, req *sbv1.ConfirmTransactionRequest) (*sbv1.ConfirmTransactionResponse, error) {
	if req.TransactionId == "" {
		return nil, status.Error(codes.InvalidArgument, "transaction_id required")
	}
	_, err := s.db.Exec(ctx, `
		UPDATE ledger_entries
		SET status = 'CONFIRMED', confirmed_at = NOW()
		WHERE transaction_id = $1 AND status = 'PENDING_CONFIRMATION'`,
		req.TransactionId,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "confirm transaction: %v", err)
	}
	return &sbv1.ConfirmTransactionResponse{
		TransactionId: req.TransactionId,
		Status:        sbv1.LedgerEntryStatus_LEDGER_ENTRY_STATUS_CONFIRMED,
	}, nil
}

// GetBalance returns the current balance for a user.
func (s *Service) GetBalance(ctx context.Context, req *sbv1.GetBalanceRequest) (*sbv1.GetBalanceResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id required")
	}
	var (
		availableMinor    int64
		betsInFlightMinor int64
		currency          string
	)
	err := s.db.QueryRow(ctx,
		`SELECT available_minor, bets_in_flight_minor, currency FROM balances WHERE user_id = $1`,
		req.UserId,
	).Scan(&availableMinor, &betsInFlightMinor, &currency)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "balance not found: %v", err)
	}
	return &sbv1.GetBalanceResponse{
		UserId:        req.UserId,
		Available:     &sbv1.Money{AmountMinor: availableMinor, Currency: currency},
		BetsInFlight:  &sbv1.Money{AmountMinor: betsInFlightMinor, Currency: currency},
		Total:         &sbv1.Money{AmountMinor: availableMinor + betsInFlightMinor, Currency: currency},
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (s *Service) getExistingTransaction(ctx context.Context, txID string) (*sbv1.DeductBalanceResponse, error) {
	var (
		st       string
		currency string
	)
	err := s.db.QueryRow(ctx,
		`SELECT status, currency FROM ledger_entries WHERE transaction_id = $1`, txID,
	).Scan(&st, &currency)
	if err != nil {
		return nil, err
	}
	return &sbv1.DeductBalanceResponse{
		TransactionId: txID,
		Status:        ledgerStatusFromString(st),
	}, nil
}

func kycStatusFromString(s string) sbv1.KYCStatus {
	switch s {
	case "PENDING":
		return sbv1.KYCStatus_KYC_STATUS_PENDING
	case "VERIFIED":
		return sbv1.KYCStatus_KYC_STATUS_VERIFIED
	case "REJECTED":
		return sbv1.KYCStatus_KYC_STATUS_REJECTED
	default:
		return sbv1.KYCStatus_KYC_STATUS_UNSPECIFIED
	}
}

func ledgerStatusFromString(s string) sbv1.LedgerEntryStatus {
	switch s {
	case "PENDING_CONFIRMATION":
		return sbv1.LedgerEntryStatus_LEDGER_ENTRY_STATUS_PENDING_CONFIRMATION
	case "CONFIRMED":
		return sbv1.LedgerEntryStatus_LEDGER_ENTRY_STATUS_CONFIRMED
	case "CANCELLED":
		return sbv1.LedgerEntryStatus_LEDGER_ENTRY_STATUS_CANCELLED
	default:
		return sbv1.LedgerEntryStatus_LEDGER_ENTRY_STATUS_UNSPECIFIED
	}
}

func creditReasonToEntryType(r sbv1.CreditReason) string {
	switch r {
	case sbv1.CreditReason_CREDIT_REASON_WIN:
		return "CREDIT_WIN"
	case sbv1.CreditReason_CREDIT_REASON_VOID:
		return "CREDIT_VOID"
	case sbv1.CreditReason_CREDIT_REASON_PUSH:
		return "CREDIT_PUSH"
	default:
		return "CREDIT"
	}
}

// ReconcileStaleTransactions is the backstop job: finds PENDING_CONFIRMATION
// ledger rows older than threshold and alerts ops. Should be called by a cron.
func ReconcileStaleTransactions(ctx context.Context, db *pgxpool.Pool, threshold time.Duration, alert func(txID string)) error {
	rows, err := db.Query(ctx, `
		SELECT transaction_id
		FROM ledger_entries
		WHERE status = 'PENDING_CONFIRMATION'
		  AND created_at < NOW() - $1::interval`,
		fmt.Sprintf("%d seconds", int(threshold.Seconds())),
	)
	if err != nil {
		return fmt.Errorf("reconcile query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var txID string
		if err := rows.Scan(&txID); err != nil {
			continue
		}
		alert(txID)
	}
	return rows.Err()
}
