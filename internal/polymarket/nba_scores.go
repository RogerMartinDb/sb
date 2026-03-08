package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	nbaScoreboardURL    = "https://cdn.nba.com/static/json/liveData/scoreboard/todaysScoreboard_00.json"
	scoreUpdateInterval = 60 * time.Second
)

// nbaScoreboard is the response from the NBA scoreboard API.
type nbaScoreboard struct {
	Scoreboard struct {
		Games []nbaGame `json:"games"`
	} `json:"scoreboard"`
}

type nbaGame struct {
	GameStatus     int    `json:"gameStatus"` // 1=scheduled, 2=live, 3=final
	GameStatusText string `json:"gameStatusText"`
	Period         int    `json:"period"`
	GameClock      string `json:"gameClock"` // ISO 8601 duration e.g. "PT05M04.00S"
	HomeTeam       nbaTeam `json:"homeTeam"`
	AwayTeam       nbaTeam `json:"awayTeam"`
}

type nbaTeam struct {
	TeamTricode string `json:"teamTricode"`
	TeamName    string `json:"teamName"`
	Score       int    `json:"score"`
}

// ScoreUpdater polls the NBA scoreboard API and updates live game scores
// in the catalog database.
type ScoreUpdater struct {
	db     *pgxpool.Pool
	client *http.Client
	logger *slog.Logger
}

func NewScoreUpdater(db *pgxpool.Pool, logger *slog.Logger) *ScoreUpdater {
	return &ScoreUpdater{
		db:     db,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// Run starts the score update loop.
func (u *ScoreUpdater) Run(ctx context.Context) {
	u.logger.Info("nba score updater starting", "interval", scoreUpdateInterval)
	u.update(ctx)

	ticker := time.NewTicker(scoreUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.update(ctx)
		}
	}
}

func (u *ScoreUpdater) update(ctx context.Context) {
	games, err := u.fetchScoreboard(ctx)
	if err != nil {
		u.logger.Error("nba: fetch scoreboard failed", "err", err)
		return
	}

	if len(games) == 0 {
		return
	}

	// Build a lookup of live events from our DB that are LIVE or SCHEDULED today.
	rows, err := u.db.Query(ctx, `
		SELECT event_id, name, status FROM events
		WHERE status IN ('SCHEDULED', 'LIVE')
		  AND starts_at::date >= CURRENT_DATE - INTERVAL '1 day'
		  AND starts_at::date <= CURRENT_DATE + INTERVAL '1 day'`)
	if err != nil {
		u.logger.Error("nba: query events failed", "err", err)
		return
	}
	defer rows.Close()

	type dbEvent struct {
		eventID string
		name    string
		status  string
	}
	var events []dbEvent
	for rows.Next() {
		var e dbEvent
		if err := rows.Scan(&e.eventID, &e.name, &e.status); err != nil {
			continue
		}
		events = append(events, e)
	}

	updated := 0
	for _, game := range games {
		if game.GameStatus == 1 {
			continue // not started yet
		}

		// Match game to our event by team names in event name.
		// Event names are like "Celtics @ Cavaliers".
		var matched *dbEvent
		for i := range events {
			if matchesGame(events[i].name, game) {
				matched = &events[i]
				break
			}
		}
		if matched == nil {
			continue
		}

		period := formatPeriod(game)
		clock := formatClock(game)

		// Determine status: LIVE for in-progress, keep as-is for final.
		newStatus := matched.status
		if game.GameStatus == 2 {
			newStatus = "LIVE"
		} else if game.GameStatus == 3 {
			newStatus = "FINISHED"
		}

		_, err := u.db.Exec(ctx, `
			UPDATE events
			SET home_score  = $2,
			    away_score  = $3,
			    game_period = $4,
			    game_clock  = $5,
			    status      = $6,
			    updated_at  = NOW()
			WHERE event_id = $1`,
			matched.eventID,
			game.HomeTeam.Score,
			game.AwayTeam.Score,
			period,
			clock,
			newStatus,
		)
		if err != nil {
			u.logger.Error("nba: update event score failed",
				"event_id", matched.eventID, "err", err)
			continue
		}
		updated++
	}

	if updated > 0 {
		u.logger.Info("nba: scores updated", "count", updated, "games_total", len(games))
	}
}

func (u *ScoreUpdater) fetchScoreboard(ctx context.Context) ([]nbaGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nbaScoreboardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var sb nbaScoreboard
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return sb.Scoreboard.Games, nil
}

// matchesGame checks if an event name (e.g. "Celtics @ Cavaliers") matches
// an NBA game by team name.
func matchesGame(eventName string, game nbaGame) bool {
	name := strings.ToLower(eventName)
	home := strings.ToLower(game.HomeTeam.TeamName)
	away := strings.ToLower(game.AwayTeam.TeamName)
	return strings.Contains(name, home) && strings.Contains(name, away)
}

// formatPeriod returns a human-readable period string like "Q1", "Q2", "OT1", "FINAL".
func formatPeriod(game nbaGame) string {
	if game.GameStatus == 3 {
		if game.Period > 4 {
			return fmt.Sprintf("FINAL/OT%d", game.Period-4)
		}
		return "FINAL"
	}
	if game.Period <= 4 {
		return fmt.Sprintf("Q%d", game.Period)
	}
	return fmt.Sprintf("OT%d", game.Period-4)
}

// formatClock parses the ISO 8601 duration (e.g. "PT05M04.00S") into "5:04".
// Returns empty string for final games or zero clock.
func formatClock(game nbaGame) string {
	if game.GameStatus == 3 {
		return ""
	}

	// gameStatusText often contains the user-friendly version like "END Q3"
	// or "5:04 - Q3". Use it if the clock is at zero (end of period).
	if game.GameClock == "" || game.GameClock == "PT00M00.00S" {
		return ""
	}

	// Parse "PT05M04.00S" → "5:04"
	s := strings.TrimPrefix(game.GameClock, "PT")
	s = strings.TrimSuffix(s, "S")
	parts := strings.SplitN(s, "M", 2)
	if len(parts) != 2 {
		return game.GameClock
	}

	min := strings.TrimLeft(parts[0], "0")
	if min == "" {
		min = "0"
	}

	sec := parts[1]
	// Remove fractional seconds
	if dot := strings.Index(sec, "."); dot >= 0 {
		sec = sec[:dot]
	}
	if len(sec) == 1 {
		sec = "0" + sec
	}
	if sec == "" {
		sec = "00"
	}

	return min + ":" + sec
}
