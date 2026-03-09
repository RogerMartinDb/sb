package marketdata

import (
	"strings"
	"sync"
)

// EventMatcher maintains a mapping from team names to event IDs so the NBA
// score feed can match live games to catalog events. Populated by the
// Polymarket feed, consumed by the NBA score feed/normaliser.
type EventMatcher struct {
	mu     sync.RWMutex
	events map[string]matchEntry // event_id → entry
}

type matchEntry struct {
	eventID string
	name    string // e.g. "Celtics @ Cavaliers"
}

func NewEventMatcher() *EventMatcher {
	return &EventMatcher{events: make(map[string]matchEntry)}
}

// Register adds or updates an event in the matcher.
func (m *EventMatcher) Register(eventID, name string) {
	m.mu.Lock()
	m.events[eventID] = matchEntry{eventID: eventID, name: name}
	m.mu.Unlock()
}

// FindByTeams returns the event ID for a game matching both team names.
// Team names are matched case-insensitively as substrings of the event name.
func (m *EventMatcher) FindByTeams(homeTeam, awayTeam string) (string, bool) {
	home := strings.ToLower(homeTeam)
	away := strings.ToLower(awayTeam)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entry := range m.events {
		name := strings.ToLower(entry.name)
		if strings.Contains(name, home) && strings.Contains(name, away) {
			return entry.eventID, true
		}
	}
	return "", false
}
