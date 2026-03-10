package marketdata

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// NHLScoreNormaliser converts ESPN NHL score raw events to game.state events.
type NHLScoreNormaliser struct{}

func (n *NHLScoreNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	var r NHLScoreRaw
	if err := json.Unmarshal(raw.Data, &r); err != nil {
		return nil, fmt.Errorf("nhl score normaliser: unmarshal: %w", err)
	}

	if len(r.Game.Competitions) == 0 || len(r.Game.Competitions[0].Competitors) < 2 {
		return nil, fmt.Errorf("nhl score normaliser: missing competitors")
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
		ProviderID:  "nhl-scores",
		EventID:     r.EventID,
		EventStatus: status,
		HomeScore:   homeScore,
		AwayScore:   awayScore,
		GamePeriod:  formatNHLPeriod(r.Game),
		GameClock:   formatNHLClock(r.Game),
	}}, nil
}

// formatNHLPeriod returns a human-readable period string like "P1", "P2", "P3",
// "OT", "SO", "FINAL", "FINAL/OT", or "FINAL/SO".
func formatNHLPeriod(game ESPNGame) string {
	period := game.Status.Period
	if game.Status.Type.Completed {
		switch period {
		case 4:
			return "FINAL/OT"
		case 5:
			return "FINAL/SO"
		default:
			return "FINAL"
		}
	}
	switch period {
	case 1:
		return "P1"
	case 2:
		return "P2"
	case 3:
		return "P3"
	case 4:
		return "OT"
	default:
		return "SO"
	}
}

// formatNHLClock returns the display clock from ESPN (already MM:SS formatted).
func formatNHLClock(game ESPNGame) string {
	if game.Status.Type.Completed {
		return ""
	}
	return game.Status.DisplayClock
}
