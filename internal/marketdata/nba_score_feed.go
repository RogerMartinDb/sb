package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	nbaScoreboardURL    = "https://cdn.nba.com/static/json/liveData/scoreboard/todaysScoreboard_00.json"
	scoreUpdateInterval = 60 * time.Second
)

// NBAGame is the relevant subset of a game from the NBA scoreboard API.
type NBAGame struct {
	GameStatus     int     `json:"gameStatus"` // 1=scheduled, 2=live, 3=final
	GameStatusText string  `json:"gameStatusText"`
	Period         int     `json:"period"`
	GameClock      string  `json:"gameClock"` // ISO 8601 duration e.g. "PT05M04.00S"
	HomeTeam       NBATeam `json:"homeTeam"`
	AwayTeam       NBATeam `json:"awayTeam"`
}

// NBATeam is a team entry in the NBA scoreboard response.
type NBATeam struct {
	TeamTricode string `json:"teamTricode"`
	TeamName    string `json:"teamName"`
	Score       int    `json:"score"`
}

// NBAScoreRaw is the raw event data sent by the NBA score feed.
// It augments the NBA game data with the matched event_id.
type NBAScoreRaw struct {
	EventID string  `json:"event_id"`
	Game    NBAGame `json:"game"`
}

// NBAScoreFeed implements ProviderFeed by polling the NBA scoreboard API.
type NBAScoreFeed struct {
	eventMatcher *EventMatcher
	client       *http.Client
	logger       *slog.Logger
}

func NewNBAScoreFeed(eventMatcher *EventMatcher, logger *slog.Logger) *NBAScoreFeed {
	return &NBAScoreFeed{
		eventMatcher: eventMatcher,
		client:       &http.Client{Timeout: 10 * time.Second},
		logger:       logger,
	}
}

func (f *NBAScoreFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent, 32)
	go func() {
		defer close(ch)
		f.logger.Info("nba score feed: starting", "interval", scoreUpdateInterval)
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

func (f *NBAScoreFeed) poll(ctx context.Context, ch chan<- RawProviderEvent) {
	games, err := f.fetchScoreboard(ctx)
	if err != nil {
		f.logger.Error("nba score feed: fetch failed", "err", err)
		return
	}

	now := time.Now().UTC()
	sent := 0
	for _, game := range games {
		if game.GameStatus == 1 {
			continue // not started
		}

		eventID, ok := f.eventMatcher.FindByTeams(game.HomeTeam.TeamName, game.AwayTeam.TeamName)
		if !ok {
			continue // no matching event in our catalog
		}

		raw := NBAScoreRaw{EventID: eventID, Game: game}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}

		select {
		case ch <- RawProviderEvent{
			ProviderID: "nba-scores",
			Data:       data,
			ReceivedAt: now,
		}:
			sent++
		case <-ctx.Done():
			return
		}
	}

	if sent > 0 {
		f.logger.Info("nba score feed: poll complete", "games_sent", sent)
	}
}

func (f *NBAScoreFeed) fetchScoreboard(ctx context.Context) ([]NBAGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nbaScoreboardURL, nil)
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
		Scoreboard struct {
			Games []NBAGame `json:"games"`
		} `json:"scoreboard"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return sb.Scoreboard.Games, nil
}
