package marketdata

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/sportsbook/sb/internal/polymarket"
)

// PoliticsNormaliser converts Polymarket binary yes/no events (e.g. politics,
// geopolitics) into catalog.upsert and price.update events.
type PoliticsNormaliser struct {
	sportID         string
	sportName       string
	competitionID   string
	competitionName string
	country         string
}

func NewPoliticsNormaliser(sportID, sportName, competitionID, competitionName, country string) *PoliticsNormaliser {
	return &PoliticsNormaliser{
		sportID:         sportID,
		sportName:       sportName,
		competitionID:   competitionID,
		competitionName: competitionName,
		country:         country,
	}
}

func (n *PoliticsNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	var ev polymarket.Event
	if err := json.Unmarshal(raw.Data, &ev); err != nil {
		return nil, fmt.Errorf("politics normaliser: unmarshal: %w", err)
	}

	// Filter to open binary markets.
	var open []polymarket.Market
	for _, m := range ev.Markets {
		if m.Closed || !m.Active || m.SportsMarketType != "" {
			continue
		}
		open = append(open, m)
	}
	if len(open) == 0 {
		return nil, nil
	}

	// Find the earliest endDate among open markets for sorting.
	earliestEnd := findEarliestEnd(open)
	if earliestEnd.IsZero() {
		return nil, nil
	}

	now := time.Now().UTC()
	cutoff := now.Add(lookAheadDays * 24 * time.Hour)
	if earliestEnd.After(cutoff) {
		return nil, nil // too far out
	}

	eventStatus := "SCHEDULED"
	startsAtStr := earliestEnd.Format(time.RFC3339)

	// Pick the main market: the open market with the soonest endDate.
	mainConditionID := ""
	soonest := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, m := range open {
		if t, ok := parseEndDate(m.EndDate); ok && t.Before(soonest) {
			soonest = t
			mainConditionID = m.ConditionID
		}
	}

	var events []NormalisedMarketEvent

	for _, m := range open {
		prices, err := m.ParseOutcomePrices()
		if err != nil || len(prices) < 2 {
			continue
		}
		p0, _ := strconv.ParseFloat(prices[0], 64)
		p1, _ := strconv.ParseFloat(prices[1], 64)
		p0 = clampProb(p0)
		p1 = clampProb(p1)

		closesAt := ""
		if t, ok := parseEndDate(m.EndDate); ok {
			closesAt = t.Format(time.RFC3339)
		}

		isMain := m.ConditionID == mainConditionID
		marketName := m.Question

		// Two selections per market: Yes and No.
		for i, selName := range []string{"Yes", "No"} {
			prob := p0
			if i == 1 {
				prob = p1
			}
			selID := fmt.Sprintf("%s-%d", m.ConditionID, i)
			events = append(events, NormalisedMarketEvent{
				EventType:       "catalog.upsert",
				ProviderID:      "polymarket",
				SportID:         n.sportID,
				SportName:       n.sportName,
				CompetitionID:   n.competitionID,
				CompetitionName: n.competitionName,
				Country:         n.country,
				EventID:         ev.ID,
				EventName:       ev.Title,
				StartsAt:        startsAtStr,
				EventStatus:     eventStatus,
				MarketID:        m.ConditionID,
				MarketName:      marketName,
				MarketType:      "BINARY",
				MarketStatus:    "OPEN",
				TargetValue:     0,
				IsMain:          isMain,
				ClosesAt:        closesAt,
				SelectionID:     selID,
				SelectionName:   selName,
				SelActive:       true,
				SelTargetValue:  0,
				FeedProbability: prob,
			})
		}
	}

	// Emit price.update for each open market.
	for _, m := range open {
		events = append(events, NormalisedMarketEvent{
			EventType:  "price.update",
			ProviderID: "polymarket",
			MarketID:   m.ConditionID,
		})
	}

	return events, nil
}

func findEarliestEnd(markets []polymarket.Market) time.Time {
	earliest := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	found := false
	for _, m := range markets {
		if t, ok := parseEndDate(m.EndDate); ok && t.Before(earliest) {
			earliest = t
			found = true
		}
	}
	if !found {
		return time.Time{}
	}
	return earliest
}

func parseEndDate(s string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
