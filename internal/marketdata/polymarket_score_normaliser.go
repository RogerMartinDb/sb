package marketdata

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PolymarketScoreNormaliser converts Polymarket sports WebSocket score events
// into game.state NormalisedMarketEvents.
type PolymarketScoreNormaliser struct{}

func (n *PolymarketScoreNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	var r PolymarketScoreRaw
	if err := json.Unmarshal(raw.Data, &r); err != nil {
		return nil, fmt.Errorf("polymarket score normaliser: unmarshal: %w", err)
	}

	status := "LIVE"
	if r.Result.Ended {
		status = "FINISHED"
	} else if !r.Result.Live {
		status = "SCHEDULED"
	}

	homeScore, awayScore := parseScore(r.Result.Score)

	return []NormalisedMarketEvent{{
		EventType:   "game.state",
		ProviderID:  "polymarket-scores",
		EventID:     r.EventID,
		EventStatus: status,
		HomeScore:   homeScore,
		AwayScore:   awayScore,
		GamePeriod:  r.Result.Period,
		GameClock:   r.Result.Elapsed,
	}}, nil
}

// parseScore splits "home-away" (e.g. "3-16") into two ints.
func parseScore(s string) (int, int) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	home, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	away, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return home, away
}
