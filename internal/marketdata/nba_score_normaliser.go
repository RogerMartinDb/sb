package marketdata

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NBAScoreNormaliser converts NBA score raw events to game.state events.
type NBAScoreNormaliser struct{}

func (n *NBAScoreNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	var r NBAScoreRaw
	if err := json.Unmarshal(raw.Data, &r); err != nil {
		return nil, fmt.Errorf("nba score normaliser: unmarshal: %w", err)
	}

	status := "LIVE"
	if r.Game.GameStatus == 3 {
		status = "FINISHED"
	}

	return []NormalisedMarketEvent{{
		EventType:   "game.state",
		ProviderID:  "nba-scores",
		EventID:     r.EventID,
		EventStatus: status,
		HomeScore:   r.Game.HomeTeam.Score,
		AwayScore:   r.Game.AwayTeam.Score,
		GamePeriod:  formatPeriod(r.Game),
		GameClock:   formatClock(r.Game),
	}}, nil
}

// formatPeriod returns a human-readable period string like "Q1", "OT1", "FINAL".
func formatPeriod(game NBAGame) string {
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
func formatClock(game NBAGame) string {
	if game.GameStatus == 3 {
		return ""
	}
	if game.GameClock == "" || game.GameClock == "PT00M00.00S" {
		return ""
	}

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
