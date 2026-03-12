// Package marketdata implements the Market Data Ingestion Service: connects to
// external provider feeds (Sportradar, Polymarket, NBA, etc.), normalises to
// canonical format, and publishes to Kafka.
package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
)

// NormalisedMarketEvent is the canonical event schema published to
// market-data.normalised. All downstream services (Odds Management, Market
// Catalog, Settlement) consume this topic.
type NormalisedMarketEvent struct {
	EventType   string `json:"event_type"` // "catalog.upsert", "price.update", "game.state", "market.resulted"
	ProviderID  string `json:"provider_id"`
	SportID     string `json:"sport_id"`
	EventID     string `json:"event_id"`
	MarketID    string `json:"market_id"`
	SelectionID string `json:"selection_id,omitempty"`

	// Catalog fields (populated for catalog.upsert).
	SportName       string  `json:"sport_name,omitempty"`
	CompetitionID   string  `json:"competition_id,omitempty"`
	CompetitionName string  `json:"competition_name,omitempty"`
	Country         string  `json:"country,omitempty"`
	EventName       string  `json:"event_name,omitempty"`
	StartsAt        string  `json:"starts_at,omitempty"`
	EventStatus     string  `json:"event_status,omitempty"` // SCHEDULED, LIVE
	MarketName      string  `json:"market_name,omitempty"`
	MarketType      string  `json:"market_type,omitempty"` // ML, SPREAD, TOTAL
	MarketStatus    string  `json:"market_status,omitempty"`
	TargetValue     float64 `json:"target_value,omitempty"`
	IsMain          bool    `json:"is_main,omitempty"`
	ClosesAt        string  `json:"closes_at,omitempty"`
	SelectionName   string  `json:"selection_name,omitempty"`
	SelActive       bool    `json:"sel_active,omitempty"`
	SelTargetValue  float64 `json:"sel_target_value,omitempty"`
	FeedProbability float64 `json:"feed_probability,omitempty"`

	// Game state fields (populated for game.state).
	HomeScore  int    `json:"home_score,omitempty"`
	AwayScore  int    `json:"away_score,omitempty"`
	GamePeriod string `json:"game_period,omitempty"`
	GameClock  string `json:"game_clock,omitempty"`

	// Odds fields (populated for price updates).
	OddsDecimal  float64 `json:"odds_decimal,omitempty"`
	OddsAmerican int     `json:"odds_american,omitempty"`

	// Result fields (populated for market.resulted events).
	WinningSelectionID string `json:"winning_selection_id,omitempty"`

	PublishedAt string `json:"published_at"` // RFC3339Nano
	// SourceOffset is set by the ingestion service to the raw feed message ID,
	// providing an audit trail back to the provider.
	SourceOffset string `json:"source_offset,omitempty"`
}

// ProviderFeed is implemented by adapters for specific data providers
// (Sportradar, Polymarket, NBA scores, etc.).
type ProviderFeed interface {
	// Subscribe starts delivering raw events to the returned channel.
	// The channel is closed when ctx is cancelled or on unrecoverable error.
	Subscribe(ctx context.Context) (<-chan RawProviderEvent, error)
}

// RawProviderEvent is an opaque event from an upstream data provider.
type RawProviderEvent struct {
	ProviderID string
	Data       json.RawMessage
	ReceivedAt time.Time
}

// Normaliser transforms a RawProviderEvent into one or more NormalisedMarketEvents.
// Different providers require different normalisers.
type Normaliser interface {
	Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error)
}

// IngestionService connects to one or more provider feeds and publishes
// normalised events to Kafka.
type IngestionService struct {
	feeds      []ProviderFeed
	normaliser Normaliser
	producer   sarama.SyncProducer
	logger     *slog.Logger
}

// NewIngestionService constructs the service with a transactional producer for
// durability (acks=all, min.insync.replicas=2 in production).
func NewIngestionService(feeds []ProviderFeed, normaliser Normaliser, brokers []string, logger *slog.Logger) (*IngestionService, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Idempotent = true
	cfg.Net.MaxOpenRequests = 1
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("ingestion: create producer: %w", err)
	}
	return &IngestionService{
		feeds:      feeds,
		normaliser: normaliser,
		producer:   producer,
		logger:     logger,
	}, nil
}

// Run starts consuming from all registered feeds and publishing to Kafka.
// Blocks until ctx is cancelled.
func (s *IngestionService) Run(ctx context.Context) error {
	errCh := make(chan error, len(s.feeds))
	for _, feed := range s.feeds {
		go func(f ProviderFeed) {
			if err := s.consumeFeed(ctx, f); err != nil {
				errCh <- err
			}
		}(feed)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *IngestionService) consumeFeed(ctx context.Context, feed ProviderFeed) error {
	ch, err := feed.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("subscribe feed: %w", err)
	}
	for raw := range ch {
		events, err := s.normaliser.Normalise(raw)
		if err != nil {
			s.logger.Warn("ingestion: normalise failed", "provider", raw.ProviderID, "err", err)
			continue
		}
		for i := range events {
			if err := s.publish(&events[i]); err != nil {
				s.logger.Error("ingestion: publish failed", "event_type", events[i].EventType, "err", err)
			}
		}
	}
	return nil
}

func (s *IngestionService) publish(event *NormalisedMarketEvent) error {
	event.PublishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// Use market_id as partition key when available, else event_id.
	key := event.MarketID
	if key == "" {
		key = event.EventID
	}

	_, _, err = s.producer.SendMessage(&sarama.ProducerMessage{
		Topic: "market-data-normalised",
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(data),
	})
	return err
}

// Close shuts down the producer.
func (s *IngestionService) Close() error {
	return s.producer.Close()
}

// ── Sportradar adapter (stub) ─────────────────────────────────────────────────

// SportradarFeed is a stub implementation of ProviderFeed for Sportradar.
// Replace with real WebSocket / HTTP streaming integration.
type SportradarFeed struct {
	APIURL string
	APIKey string
	logger *slog.Logger
}

func NewSportradarFeed(apiURL, apiKey string, logger *slog.Logger) *SportradarFeed {
	return &SportradarFeed{APIURL: apiURL, APIKey: apiKey, logger: logger}
}

func (f *SportradarFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent)
	go func() {
		defer close(ch)
		f.logger.Info("sportradar: connected (stub mode — no real events)")
		<-ctx.Done()
	}()
	return ch, nil
}

// SportradarNormaliser converts Sportradar raw events to canonical format.
type SportradarNormaliser struct{}

func (n *SportradarNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	// TODO: implement full Sportradar event schema mapping.
	var parsed struct {
		Type     string `json:"type"`
		MarketID string `json:"market_id"`
	}
	if err := json.Unmarshal(raw.Data, &parsed); err != nil {
		return nil, fmt.Errorf("sportradar: unmarshal: %w", err)
	}
	return []NormalisedMarketEvent{{
		EventType:  parsed.Type,
		ProviderID: raw.ProviderID,
		MarketID:   parsed.MarketID,
	}}, nil
}
