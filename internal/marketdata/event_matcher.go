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
// Team names are matched case-insensitively as whole words so that "Kansas"
// does not false-match "Kansas State", "Michigan" does not match
// "Michigan State", etc.
func (m *EventMatcher) FindByTeams(homeTeam, awayTeam string) (string, bool) {
	home := strings.ToLower(homeTeam)
	away := strings.ToLower(awayTeam)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entry := range m.events {
		name := strings.ToLower(entry.name)
		if containsWholeWord(name, home) && containsWholeWord(name, away) {
			return entry.eventID, true
		}
	}
	return "", false
}

// containsWholeWord reports whether s contains word as a whole-word match,
// i.e. not immediately surrounded by ASCII letters on either side.
// This prevents "kansas" from matching "kansas state" and vice versa.
func containsWholeWord(s, word string) bool {
	for i := 0; i <= len(s)-len(word); {
		idx := strings.Index(s[i:], word)
		if idx < 0 {
			return false
		}
		idx += i
		end := idx + len(word)
		leftOK := idx == 0 || !isASCIILetter(s[idx-1])
		rightOK := end == len(s) || !isASCIILetter(s[end])
		if leftOK && rightOK {
			return true
		}
		i = idx + 1
	}
	return false
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
