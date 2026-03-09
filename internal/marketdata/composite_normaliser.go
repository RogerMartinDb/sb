package marketdata

import "fmt"

// CompositeNormaliser dispatches to the correct normaliser based on ProviderID.
type CompositeNormaliser struct {
	normalisers map[string]Normaliser
}

func NewCompositeNormaliser(normalisers map[string]Normaliser) *CompositeNormaliser {
	return &CompositeNormaliser{normalisers: normalisers}
}

func (c *CompositeNormaliser) Normalise(raw RawProviderEvent) ([]NormalisedMarketEvent, error) {
	n, ok := c.normalisers[raw.ProviderID]
	if !ok {
		return nil, fmt.Errorf("composite: unknown provider %q", raw.ProviderID)
	}
	return n.Normalise(raw)
}
