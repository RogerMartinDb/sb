package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	sportsWSURL      = "wss://sports-api.polymarket.com/ws"
	scoreMaxBackoff  = 60 * time.Second
)

// supportedLeagues is the set of league abbreviations we consume from the
// Polymarket sports WS. Keys must match the leagueAbbreviation field exactly.
var supportedLeagues = map[string]bool{
	"nba": true,
	"cbb": true, // NCAAB
	"nhl": true,
}

// SportResult is the JSON payload received from the Polymarket sports WS.
type SportResult struct {
	GameID             int    `json:"gameId"`
	LeagueAbbreviation string `json:"leagueAbbreviation"`
	Slug               string `json:"slug"`
	HomeTeam           string `json:"homeTeam"`
	AwayTeam           string `json:"awayTeam"`
	Status             string `json:"status"` // "InProgress", "Final", etc.
	Score              string `json:"score"`   // "home-away", e.g. "3-16"
	Period             string `json:"period"`  // e.g. "Q4", "1H"
	Elapsed            string `json:"elapsed"` // e.g. "5:18"
	Live               bool   `json:"live"`
	Ended              bool   `json:"ended"`
}

// PolymarketScoreRaw is the payload emitted to the normaliser.
type PolymarketScoreRaw struct {
	EventID string      `json:"event_id"`
	Result  SportResult `json:"result"`
}

// PolymarketScoreFeed implements ProviderFeed by connecting to the Polymarket
// sports WebSocket for real-time scores.
type PolymarketScoreFeed struct {
	eventMatcher *EventMatcher
	logger       *slog.Logger
}

func NewPolymarketScoreFeed(eventMatcher *EventMatcher, logger *slog.Logger) *PolymarketScoreFeed {
	return &PolymarketScoreFeed{
		eventMatcher: eventMatcher,
		logger:       logger,
	}
}

func (f *PolymarketScoreFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent, 64)
	go func() {
		defer close(ch)
		f.run(ctx, ch)
	}()
	return ch, nil
}

func (f *PolymarketScoreFeed) run(ctx context.Context, ch chan<- RawProviderEvent) {
	backoff := time.Second
	for {
		err := f.connectAndStream(ctx, ch)
		if ctx.Err() != nil {
			return
		}
		f.logger.Error("polymarket score ws: connection lost, reconnecting", "err", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = backoff * 2
		if backoff > scoreMaxBackoff {
			backoff = scoreMaxBackoff
		}
	}
}

func (f *PolymarketScoreFeed) connectAndStream(ctx context.Context, ch chan<- RawProviderEvent) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, sportsWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	f.logger.Info("polymarket score ws: connected", "url", sportsWSURL)

	// No subscription message needed -- server starts sending immediately.

	// Read messages in a goroutine.
	msgCh := make(chan []byte, 64)
	errCh := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			select {
			case msgCh <- msg:
			default:
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errCh:
			return fmt.Errorf("read: %w", err)

		case msg := <-msgCh:
			f.handleMessage(msg, conn, ch)
		}
	}
}

func (f *PolymarketScoreFeed) handleMessage(msg []byte, conn *websocket.Conn, ch chan<- RawProviderEvent) {
	text := string(msg)

	// Server sends text "ping" every ~5s; respond with "pong".
	if text == "ping" {
		if err := conn.WriteMessage(websocket.TextMessage, []byte("pong")); err != nil {
			f.logger.Warn("polymarket score ws: pong write failed", "err", err)
		}
		return
	}

	// Try to parse as a sport_result.
	var result SportResult
	if err := json.Unmarshal(msg, &result); err != nil {
		f.logger.Debug("polymarket score ws: unmarshal failed, ignoring", "err", err, "msg", text)
		return
	}

	// Filter to supported leagues.
	if !supportedLeagues[result.LeagueAbbreviation] {
		return
	}

	// Match to our event using the EventMatcher.
	eventID, ok := f.eventMatcher.FindByTeams(result.HomeTeam, result.AwayTeam)
	if !ok {
		f.logger.Debug("polymarket score ws: no event match", "home", result.HomeTeam, "away", result.AwayTeam)
		return
	}

	raw := PolymarketScoreRaw{
		EventID: eventID,
		Result:  result,
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	select {
	case ch <- RawProviderEvent{
		ProviderID: "polymarket-scores",
		Data:       data,
		ReceivedAt: now,
	}:
	default:
	}
}
