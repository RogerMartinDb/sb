package marketdata

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sportsbook/sb/internal/polymarket"
)

const (
	sportID         = "basketball"
	sportName       = "Basketball"
	competitionID   = "nba"
	competitionName = "NBA"
	lookAheadDays   = 14
)

// allowedTypes maps Polymarket sportsMarketType → our market_type enum.
var allowedTypes = map[string]string{
	"moneyline": "ML",
	"spreads":   "SPREAD",
	"totals":    "TOTAL",
}

// PolymarketNormaliser converts a Polymarket game event into catalog.upsert
// and price.update events.
type PolymarketNormaliser struct{}

func (n *PolymarketNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	var ev polymarket.Event
	if err := json.Unmarshal(raw.Data, &ev); err != nil {
		return nil, fmt.Errorf("polymarket normaliser: unmarshal: %w", err)
	}

	startTime, ok := gameStartTime(ev.Markets)
	if !ok {
		return nil, fmt.Errorf("polymarket normaliser: no game start time for %s", ev.Slug)
	}

	now := time.Now().UTC()
	liveWindow := now.Add(-6 * time.Hour)
	cutoff := now.Add(lookAheadDays * 24 * time.Hour)
	if startTime.Before(liveWindow) || startTime.After(cutoff) {
		return nil, nil // outside window, skip silently
	}

	eventStatus := "SCHEDULED"
	if !startTime.After(now) {
		eventStatus = "LIVE"
	}
	eventName := strings.ReplaceAll(ev.Title, " vs. ", " @ ")
	startsAtStr := startTime.Format(time.RFC3339)

	// Filter to allowed market types.
	var allowed []polymarket.Market
	for _, m := range ev.Markets {
		if _, ok := allowedTypes[m.SportsMarketType]; ok && !m.Closed && m.Active {
			allowed = append(allowed, m)
		}
	}

	isMainMap := computeIsMain(allowed)

	var events []NormalisedMarketEvent

	// Emit catalog.upsert events: one per selection.
	for _, m := range allowed {
		ourType := allowedTypes[m.SportsMarketType]
		isMain := isMainMap[m.ConditionID]

		outcomes, err := m.ParseOutcomes()
		if err != nil {
			continue
		}
		prices, err := m.ParseOutcomePrices()
		if err != nil {
			continue
		}
		if len(outcomes) != 2 || len(prices) != 2 {
			continue
		}

		marketName := m.Name()
		sels := buildSelections(ourType, outcomes, prices, m.Line)
		targetValue := 0.0
		if len(sels) > 0 {
			targetValue = sels[0].targetValue
		}

		for i, sel := range sels {
			selID := fmt.Sprintf("%s-%d", m.ConditionID, i)
			events = append(events, NormalisedMarketEvent{
				EventType:       "catalog.upsert",
				ProviderID:      "polymarket",
				SportID:         sportID,
				SportName:       sportName,
				CompetitionID:   competitionID,
				CompetitionName: competitionName,
				Country:         "US",
				EventID:         ev.ID,
				EventName:       eventName,
				StartsAt:        startsAtStr,
				EventStatus:     eventStatus,
				MarketID:        m.ConditionID,
				MarketName:      marketName,
				MarketType:      ourType,
				MarketStatus:    "OPEN",
				TargetValue:     targetValue,
				IsMain:          isMain,
				ClosesAt:        startsAtStr,
				SelectionID:     selID,
				SelectionName:   sel.name,
				SelActive:       true,
				SelTargetValue:  sel.targetValue,
				FeedProbability: sel.prob,
			})
		}
	}

	// Emit price.update events: one per market (triggers odds management).
	for _, m := range allowed {
		events = append(events, NormalisedMarketEvent{
			EventType:  "price.update",
			ProviderID: "polymarket",
			MarketID:   m.ConditionID,
		})
	}

	return events, nil
}

// ── Pure helpers (moved from polymarket/sync.go) ────────────────────────────

type selRow struct {
	name        string
	targetValue float64
	prob        float64
}

func buildSelections(marketType string, outcomes, prices []string, line *float64) []selRow {
	p0, _ := strconv.ParseFloat(prices[0], 64)
	p1, _ := strconv.ParseFloat(prices[1], 64)

	switch marketType {
	case "ML":
		return []selRow{
			{name: outcomes[0], targetValue: 0, prob: p0},
			{name: outcomes[1], targetValue: 0, prob: p1},
		}
	case "SPREAD":
		l := derefLine(line)
		return []selRow{
			{name: outcomes[0], targetValue: l, prob: p0},
			{name: outcomes[1], targetValue: -l, prob: p1},
		}
	case "TOTAL":
		l := absLine(line)
		return []selRow{
			{name: outcomes[0], targetValue: l, prob: p0},
			{name: outcomes[1], targetValue: -l, prob: p1},
		}
	default:
		return []selRow{
			{name: outcomes[0], targetValue: 0, prob: p0},
			{name: outcomes[1], targetValue: 0, prob: p1},
		}
	}
}

func computeIsMain(markets []polymarket.Market) map[string]bool {
	result := make(map[string]bool)
	byType := make(map[string][]polymarket.Market)
	for _, m := range markets {
		ourType := allowedTypes[m.SportsMarketType]
		byType[ourType] = append(byType[ourType], m)
	}

	for _, m := range byType["ML"] {
		result[m.ConditionID] = true
	}

	for _, mtype := range []string{"SPREAD", "TOTAL"} {
		group := byType[mtype]
		if len(group) == 0 {
			continue
		}
		bestID := ""
		bestDist := math.MaxFloat64
		for _, m := range group {
			prices, err := m.ParseOutcomePrices()
			if err != nil || len(prices) < 2 {
				continue
			}
			maxProb := 0.0
			for _, ps := range prices {
				p, _ := strconv.ParseFloat(ps, 64)
				if p > maxProb {
					maxProb = p
				}
			}
			dist := math.Abs(maxProb - 0.5)
			if dist < bestDist {
				bestDist = dist
				bestID = m.ConditionID
			}
		}
		if bestID != "" {
			result[bestID] = true
		}
	}

	return result
}

func gameStartTime(markets []polymarket.Market) (time.Time, bool) {
	for _, m := range markets {
		if m.GameStartTime == "" {
			continue
		}
		for _, layout := range []string{
			"2006-01-02 15:04:05-07",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05-07:00",
		} {
			if t, err := time.Parse(layout, m.GameStartTime); err == nil {
				return t.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

func absLine(line *float64) float64 {
	if line == nil {
		return 0
	}
	return math.Abs(*line)
}

func derefLine(line *float64) float64 {
	if line == nil {
		return 0
	}
	return *line
}
