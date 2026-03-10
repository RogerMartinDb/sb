package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const nhlScoreboardURL = "https://site.api.espn.com/apis/site/v2/sports/hockey/nhl/scoreboard"

// NHLScoreRaw is the raw event data sent by the NHL score feed.
type NHLScoreRaw struct {
	EventID string   `json:"event_id"`
	Game    ESPNGame `json:"game"`
}

// NHLScoreFeed implements ProviderFeed by polling the ESPN NHL scoreboard API.
type NHLScoreFeed struct {
	eventMatcher *EventMatcher
	client       *http.Client
	logger       *slog.Logger
}

func NewNHLScoreFeed(eventMatcher *EventMatcher, logger *slog.Logger) *NHLScoreFeed {
	return &NHLScoreFeed{
		eventMatcher: eventMatcher,
		client:       &http.Client{Timeout: 10 * time.Second},
		logger:       logger,
	}
}

func (f *NHLScoreFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent, 32)
	go func() {
		defer close(ch)
		f.logger.Info("nhl score feed: starting", "interval", scoreUpdateInterval)
		f.poll(ctx, ch)

		ticker := time.NewTicker(scoreUpdateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f.poll(ctx, ch)
			}
		}
	}()
	return ch, nil
}

func (f *NHLScoreFeed) poll(ctx context.Context, ch chan<- RawProviderEvent) {
	games, err := f.fetchScoreboard(ctx)
	if err != nil {
		f.logger.Error("nhl score feed: fetch failed", "err", err)
		return
	}

	now := time.Now().UTC()
	sent := 0
	for _, game := range games {
		if game.Status.Type.State == "pre" {
			continue // not started
		}
		if len(game.Competitions) == 0 || len(game.Competitions[0].Competitors) < 2 {
			continue
		}

		var homeTeam, awayTeam string
		for _, c := range game.Competitions[0].Competitors {
			if c.HomeAway == "home" {
				homeTeam = c.Team.DisplayName
			} else {
				awayTeam = c.Team.DisplayName
			}
		}

		eventID, ok := f.eventMatcher.FindByTeams(homeTeam, awayTeam)
		if !ok {
			continue
		}

		raw := NHLScoreRaw{EventID: eventID, Game: game}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}

		select {
		case ch <- RawProviderEvent{
			ProviderID: "nhl-scores",
			Data:       data,
			ReceivedAt: now,
		}:
			sent++
		case <-ctx.Done():
			return
		}
	}

	if sent > 0 {
		f.logger.Info("nhl score feed: poll complete", "games_sent", sent)
	}
}

func (f *NHLScoreFeed) fetchScoreboard(ctx context.Context) ([]ESPNGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nhlScoreboardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var sb struct {
		Events []ESPNGame `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return sb.Events, nil
}
