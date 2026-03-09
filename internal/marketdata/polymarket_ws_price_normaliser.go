package marketdata

import (
	"encoding/json"
	"fmt"
)

// PolymarketWSPriceNormaliser converts real-time CLOB WebSocket price events
// into price.update NormalisedMarketEvents, matching the format already
// emitted by the Gamma polling normaliser.
type PolymarketWSPriceNormaliser struct{}

func (n *PolymarketWSPriceNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	var p wsPriceRaw
	if err := json.Unmarshal(raw.Data, &p); err != nil {
		return nil, fmt.Errorf("polymarket ws price normaliser: unmarshal: %w", err)
	}

	return []NormalisedMarketEvent{{
		EventType:  "price.update",
		ProviderID: "polymarket",
		MarketID:   p.ConditionID,
	}}, nil
}
