package marketdata

import "sync"

// TokenEntry maps a Polymarket CLOB token (asset_id) to its market context.
type TokenEntry struct {
	TokenID     string // CLOB token ID (asset_id)
	ConditionID string // Polymarket conditionId — our market_id
	SelIndex    int    // 0 or 1 within the market
	ProviderID  string // e.g. "polymarket-nba", "polymarket-ncaab"
}

// TokenRegistry is a thread-safe registry mapping CLOB token IDs to market info.
// The CLOB WebSocket price feed uses this to correlate incoming price_change
// events (keyed by asset_id) back to our internal market/selection identifiers.
type TokenRegistry struct {
	mu      sync.RWMutex
	byToken map[string]TokenEntry
	notify  chan struct{}
}

// NewTokenRegistry creates an empty registry.
func NewTokenRegistry() *TokenRegistry {
	return &TokenRegistry{
		byToken: make(map[string]TokenEntry),
		notify:  make(chan struct{}, 1),
	}
}

// Register adds or updates entries in the registry and signals any listeners.
func (r *TokenRegistry) Register(entries []TokenEntry) {
	r.mu.Lock()
	for _, e := range entries {
		r.byToken[e.TokenID] = e
	}
	r.mu.Unlock()

	// Non-blocking signal to notify listeners of change.
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

// Lookup returns the entry for a given token ID.
func (r *TokenRegistry) Lookup(tokenID string) (TokenEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byToken[tokenID]
	return e, ok
}

// AllTokenIDs returns all registered token IDs.
func (r *TokenRegistry) AllTokenIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.byToken))
	for id := range r.byToken {
		ids = append(ids, id)
	}
	return ids
}

// Changed returns a channel that receives a signal whenever the registry is updated.
func (r *TokenRegistry) Changed() <-chan struct{} {
	return r.notify
}

// Len returns the number of registered tokens.
func (r *TokenRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byToken)
}
