package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const ncaabScoreboardURL = "https://site.api.espn.com/apis/site/v2/sports/basketball/mens-college-basketball/scoreboard"

// ESPNGame is the relevant subset of a game from the ESPN scoreboard API.
type ESPNGame struct {
	Status struct {
		DisplayClock string `json:"displayClock"` // e.g. "5:04"
		Period       int    `json:"period"`        // 1=1st half, 2=2nd half, >2=OT
		Type         struct {
			State     string `json:"state"` // "pre", "in", "post"
			Completed bool   `json:"completed"`
		} `json:"type"`
	} `json:"status"`
	Competitions []struct {
		Competitors []struct {
			HomeAway string `json:"homeAway"` // "home" or "away"
			Team     struct {
				DisplayName string `json:"displayName"` // e.g. "Duke Blue Devils"
				Location    string `json:"location"`    // e.g. "Duke"
			} `json:"team"`
			Score string `json:"score"` // integer as string, e.g. "72"
		} `json:"competitors"`
	} `json:"competitions"`
}

// NCAABScoreRaw is the raw event data sent by the NCAAB score feed.
type NCAABScoreRaw struct {
	EventID string   `json:"event_id"`
	Game    ESPNGame `json:"game"`
}

// NCAABScoreFeed implements ProviderFeed by polling the ESPN NCAAB scoreboard API.
type NCAABScoreFeed struct {
	eventMatcher *EventMatcher
	client       *http.Client
	logger       *slog.Logger
}

func NewNCAABScoreFeed(eventMatcher *EventMatcher, logger *slog.Logger) *NCAABScoreFeed {
	return &NCAABScoreFeed{
		eventMatcher: eventMatcher,
		client:       &http.Client{Timeout: 10 * time.Second},
		logger:       logger,
	}
}

func (f *NCAABScoreFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent, 32)
	go func() {
		defer close(ch)
		f.logger.Info("ncaab score feed: starting", "interval", scoreUpdateInterval)
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

func (f *NCAABScoreFeed) poll(ctx context.Context, ch chan<- RawProviderEvent) {
	games, err := f.fetchScoreboard(ctx)
	if err != nil {
		f.logger.Error("ncaab score feed: fetch failed", "err", err)
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
			name := c.Team.Location
			if name == "" {
				name = c.Team.DisplayName
			}
			if c.HomeAway == "home" {
				homeTeam = name
			} else {
				awayTeam = name
			}
		}

		eventID, ok := f.eventMatcher.FindByTeams(homeTeam, awayTeam)
		if !ok {
			continue
		}

		raw := NCAABScoreRaw{EventID: eventID, Game: game}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}

		select {
		case ch <- RawProviderEvent{
			ProviderID: "ncaab-scores",
			Data:       data,
			ReceivedAt: now,
		}:
			sent++
		case <-ctx.Done():
			return
		}
	}

	if sent > 0 {
		f.logger.Info("ncaab score feed: poll complete", "games_sent", sent)
	}
}

func (f *NCAABScoreFeed) fetchScoreboard(ctx context.Context) ([]ESPNGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ncaabScoreboardURL, nil)
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
