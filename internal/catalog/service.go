// Package catalog implements the Market Catalog service.
package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	sbv1 "github.com/sportsbook/sb/gen/sportsbook/v1"
)

const marketStatusTTL = 5 * time.Second

// Service implements sbv1.CatalogServiceServer and owns the market lifecycle.
type Service struct {
	sbv1.UnimplementedCatalogServiceServer
	db          *pgxpool.Pool
	redisMarket *redis.Client // market-status Redis instance
	logger      *slog.Logger
}

func NewService(db *pgxpool.Pool, redisMarket *redis.Client, logger *slog.Logger) *Service {
	return &Service{db: db, redisMarket: redisMarket, logger: logger}
}

// GetMarket returns market details by ID, using Redis for hot-path caching.
func (s *Service) GetMarket(ctx context.Context, req *sbv1.GetMarketRequest) (*sbv1.GetMarketResponse, error) {
	if req.MarketId == "" {
		return nil, status.Error(codes.InvalidArgument, "market_id required")
	}

	var (
		marketID  string
		eventID   string
		name      string
		mstatus   string
		opensAt   time.Time
		closesAt  time.Time
	)
	err := s.db.QueryRow(ctx, `
		SELECT market_id, event_id, name, status, opens_at, closes_at
		FROM markets WHERE market_id = $1`,
		req.MarketId,
	).Scan(&marketID, &eventID, &name, &mstatus, &opensAt, &closesAt)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "market %s not found", req.MarketId)
	}

	return &sbv1.GetMarketResponse{
		Market: &sbv1.Market{
			MarketId: marketID,
			EventId:  eventID,
			Name:     name,
			Status:   marketStatusFromString(mstatus),
			OpensAt:  timestamppb.New(opensAt),
			ClosesAt: timestamppb.New(closesAt),
		},
	}, nil
}

// GetSelection returns a selection by ID.
func (s *Service) GetSelection(ctx context.Context, req *sbv1.GetSelectionRequest) (*sbv1.GetSelectionResponse, error) {
	if req.SelectionId == "" {
		return nil, status.Error(codes.InvalidArgument, "selection_id required")
	}

	var (
		marketID    string
		selectionID string
		name        string
		active      bool
	)
	err := s.db.QueryRow(ctx, `
		SELECT market_id, selection_id, name, active
		FROM selections WHERE selection_id = $1`,
		req.SelectionId,
	).Scan(&marketID, &selectionID, &name, &active)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "selection %s not found", req.SelectionId)
	}

	return &sbv1.GetSelectionResponse{
		Selection: &sbv1.Selection{
			MarketId:    marketID,
			SelectionId: selectionID,
			Name:        name,
			Active:      active,
		},
	}, nil
}

// ListMarkets lists markets for an event.
func (s *Service) ListMarkets(ctx context.Context, req *sbv1.ListMarketsRequest) (*sbv1.ListMarketsResponse, error) {
	rows, err := s.db.Query(ctx,
		`SELECT market_id, event_id, name, status, opens_at, closes_at
		 FROM markets WHERE event_id = $1 ORDER BY name LIMIT $2`,
		req.EventId, req.PageSize,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query markets: %v", err)
	}
	defer rows.Close()

	var markets []*sbv1.Market
	for rows.Next() {
		var m sbv1.Market
		var opensAt, closesAt time.Time
		var mstatus string
		if err := rows.Scan(&m.MarketId, &m.EventId, &m.Name, &mstatus, &opensAt, &closesAt); err != nil {
			return nil, status.Errorf(codes.Internal, "scan market: %v", err)
		}
		m.Status = marketStatusFromString(mstatus)
		m.OpensAt = timestamppb.New(opensAt)
		m.ClosesAt = timestamppb.New(closesAt)
		markets = append(markets, &m)
	}
	return &sbv1.ListMarketsResponse{Markets: markets}, nil
}

// SuspendMarket updates the market status and publishes to Redis cache.
func (s *Service) SuspendMarket(ctx context.Context, marketID string) error {
	return s.setMarketStatus(ctx, marketID, "SUSPENDED")
}

// OpenMarket transitions a market to OPEN.
func (s *Service) OpenMarket(ctx context.Context, marketID string) error {
	return s.setMarketStatus(ctx, marketID, "OPEN")
}

// CloseMarket closes a market to new bets.
func (s *Service) CloseMarket(ctx context.Context, marketID string) error {
	return s.setMarketStatus(ctx, marketID, "CLOSED")
}

func (s *Service) setMarketStatus(ctx context.Context, marketID, newStatus string) error {
	if _, err := s.db.Exec(ctx,
		`UPDATE markets SET status = $1, updated_at = NOW() WHERE market_id = $2`,
		newStatus, marketID,
	); err != nil {
		return fmt.Errorf("update market status: %w", err)
	}
	// Write through to Redis market-status cache.
	key := fmt.Sprintf("market:status:%s", marketID)
	return s.redisMarket.Set(ctx, key, newStatus, marketStatusTTL).Err()
}

func marketStatusFromString(s string) sbv1.MarketStatus {
	switch s {
	case "OPEN":
		return sbv1.MarketStatus_MARKET_STATUS_OPEN
	case "SUSPENDED":
		return sbv1.MarketStatus_MARKET_STATUS_SUSPENDED
	case "CLOSED":
		return sbv1.MarketStatus_MARKET_STATUS_CLOSED
	case "SETTLED":
		return sbv1.MarketStatus_MARKET_STATUS_SETTLED
	default:
		return sbv1.MarketStatus_MARKET_STATUS_UNSPECIFIED
	}
}
