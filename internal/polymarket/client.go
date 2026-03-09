// Package polymarket provides a client for the Polymarket Gamma REST API
// and a syncer that upserts NBA game markets into the catalog database.
package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const gammaBaseURL = "https://gamma-api.polymarket.com"

// Polymarket tag IDs for basketball competitions.
const (
	NBATagID   = 745    // NBA
	NCAABTagID = 28 // Basketball (includes CBB games with "cbb-" slug prefix)
)

// Event is a Polymarket game event (e.g., "Nets vs. Pistons") from the /events endpoint.
type Event struct {
	ID      string   `json:"id"`
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Active  bool     `json:"active"`
	Closed  bool     `json:"closed"`
	Markets []Market `json:"markets"`
}

// Market is a single Polymarket binary market within an event.
// Outcomes, OutcomePrices, and ClobTokenIDs are JSON-encoded strings
// (e.g. `"[\"Nuggets\",\"Grizzlies\"]"`), not native arrays.
type Market struct {
	ID               string   `json:"id"`
	ConditionID      string   `json:"conditionId"`
	Question         string   `json:"question"`
	Slug             string   `json:"slug"`
	GroupItemTitle   *string  `json:"groupItemTitle"` // null for moneyline
	SportsMarketType string   `json:"sportsMarketType"`
	Line             *float64 `json:"line"`
	Outcomes         string   `json:"outcomes"`      // JSON string: "[\"A\",\"B\"]"
	OutcomePrices    string   `json:"outcomePrices"` // JSON string: "[\"0.55\",\"0.45\"]"
	ClobTokenIDs     string   `json:"clobTokenIds"`  // JSON string
	GameStartTime    string   `json:"gameStartTime"`
	Active           bool     `json:"active"`
	Closed           bool     `json:"closed"`
}

// ParseOutcomes parses the JSON-encoded Outcomes string into a slice.
func (m *Market) ParseOutcomes() ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(m.Outcomes), &out); err != nil {
		return nil, fmt.Errorf("parse outcomes %q: %w", m.Outcomes, err)
	}
	return out, nil
}

// ParseOutcomePrices parses the JSON-encoded OutcomePrices string into a slice.
func (m *Market) ParseOutcomePrices() ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(m.OutcomePrices), &out); err != nil {
		return nil, fmt.Errorf("parse outcomePrices %q: %w", m.OutcomePrices, err)
	}
	return out, nil
}

// Name returns GroupItemTitle if non-nil/non-empty, otherwise Question.
func (m *Market) Name() string {
	if m.GroupItemTitle != nil && *m.GroupItemTitle != "" {
		return *m.GroupItemTitle
	}
	return m.Question
}

// Client wraps the Polymarket Gamma API.
type Client struct {
	http    *http.Client
	baseURL string
	logger  *slog.Logger
}

func NewClient(logger *slog.Logger) *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: gammaBaseURL,
		logger:  logger,
	}
}

// FetchNBAEvents returns active, non-closed NBA game events from Polymarket.
func (c *Client) FetchNBAEvents(ctx context.Context) ([]Event, error) {
	return c.FetchEvents(ctx, NBATagID)
}

// FetchNCAABEvents returns active, non-closed NCAAB game events from Polymarket.
func (c *Client) FetchNCAABEvents(ctx context.Context) ([]Event, error) {
	return c.FetchEvents(ctx, NCAABTagID)
}

// FetchEvents returns active, non-closed events for the given Polymarket tag ID.
func (c *Client) FetchEvents(ctx context.Context, tagID int) ([]Event, error) {
	var all []Event
	offset := 0
	const limit = 100

	for {
		url := fmt.Sprintf("%s/events?tag_id=%d&related_tags=true&active=true&closed=false&order=startDate&ascending=false&limit=%d&offset=%d",
			c.baseURL, tagID, limit, offset)

		c.logger.Debug("polymarket: fetching events", "url", url)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("polymarket: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("polymarket: http get: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("polymarket: unexpected status %d", resp.StatusCode)
		}

		var page []Event
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, fmt.Errorf("polymarket: decode response: %w", err)
		}

		c.logger.Debug("polymarket: fetched events page", "offset", offset, "count", len(page))
		all = append(all, page...)

		if len(page) < limit {
			break
		}
		offset += limit
	}

	c.logger.Info("polymarket: fetched events", "tag_id", tagID, "total", len(all))
	return all, nil
}
