package marketdata

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// NCAABScoreNormaliser converts ESPN NCAAB score raw events to game.state events.
type NCAABScoreNormaliser struct{}

func (n *NCAABScoreNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	var r NCAABScoreRaw
	if err := json.Unmarshal(raw.Data, &r); err != nil {
		return nil, fmt.Errorf("ncaab score normaliser: unmarshal: %w", err)
	}

	if len(r.Game.Competitions) == 0 || len(r.Game.Competitions[0].Competitors) < 2 {
		return nil, fmt.Errorf("ncaab score normaliser: missing competitors")
	}

	status := "LIVE"
	if r.Game.Status.Type.Completed {
		status = "FINISHED"
	}

	var homeScore, awayScore int
	for _, c := range r.Game.Competitions[0].Competitors {
		score, _ := strconv.Atoi(c.Score)
		if c.HomeAway == "home" {
			homeScore = score
		} else {
			awayScore = score
		}
	}

	return []NormalisedMarketEvent{{
		EventType:   "game.state",
		ProviderID:  "ncaab-scores",
		EventID:     r.EventID,
		EventStatus: status,
		HomeScore:   homeScore,
		AwayScore:   awayScore,
		GamePeriod:  formatNCAABPeriod(r.Game),
		GameClock:   formatNCAABClock(r.Game),
	}}, nil
}

// formatNCAABPeriod returns a human-readable period string like "1H", "2H", "OT1", "FINAL".
func formatNCAABPeriod(game ESPNGame) string {
	if game.Status.Type.Completed {
		if game.Status.Period > 2 {
			return fmt.Sprintf("FINAL/OT%d", game.Status.Period-2)
		}
		return "FINAL"
	}
	switch game.Status.Period {
	case 1:
		return "1H"
	case 2:
		return "2H"
	default:
		return fmt.Sprintf("OT%d", game.Status.Period-2)
	}
}

// formatNCAABClock returns the display clock string from ESPN (already MM:SS formatted).
func formatNCAABClock(game ESPNGame) string {
	if game.Status.Type.Completed {
		return ""
	}
	return game.Status.DisplayClock
}
