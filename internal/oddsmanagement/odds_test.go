package oddsmanagement

import (
	"math"
	"testing"
)

func TestLogitSigmoidRoundTrip(t *testing.T) {
	for _, p := range []float64{0.1, 0.25, 0.5, 0.75, 0.9} {
		got := sigmoid(logit(p))
		if math.Abs(got-p) > 1e-12 {
			t.Errorf("sigmoid(logit(%v)) = %v, want %v", p, got, p)
		}
	}
}

func TestFind20CentDelta_EvenMoney(t *testing.T) {
	// At 50/50, a 20-cent line should produce -110/-110.
	delta := find20CentDelta(0.5, 0.5)
	q := sigmoid(logit(0.5) + delta)
	dec := 1.0 / q
	am := DecimalToAmerican(dec)

	if am != -110 {
		t.Errorf("even money: got American %d, want -110", am)
	}
	t.Logf("even money: delta=%.6f, q=%.6f, decimal=%.4f, american=%d", delta, q, dec, am)
}

func TestFind20CentDelta_Asymmetric(t *testing.T) {
	tests := []struct {
		name    string
		p0, p1  float64
		wantFav int // expected favorite American (negative)
		wantDog int // expected underdog American (positive)
	}{
		{"60/40", 0.6, 0.4, -168, 148},
		{"70/30", 0.7, 0.3, -259, 239},
		{"55/45", 0.55, 0.45, -135, 115},
		{"80/20", 0.8, 0.2, -450, 430},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta := find20CentDelta(tt.p0, tt.p1)
			q0 := sigmoid(logit(tt.p0) + delta)
			q1 := sigmoid(logit(tt.p1) + delta)

			d0 := 1.0 / q0
			d1 := 1.0 / q1
			a0 := DecimalToAmerican(d0)
			a1 := DecimalToAmerican(d1)

			// Determine fav/dog.
			favAm, dogAm := a0, a1
			if a1 < a0 {
				favAm, dogAm = a1, a0
			}

			spread := (-favAm) - dogAm
			if spread < 19 || spread > 21 {
				t.Errorf("spread = %d, want ~20 (fav=%d, dog=%d)", spread, favAm, dogAm)
			}
			t.Logf("delta=%.6f | fav=%d dog=%d spread=%d | q0=%.6f q1=%.6f",
				delta, favAm, dogAm, spread, q0, q1)
		})
	}
}

func TestComputeMarketOdds_Basic(t *testing.T) {
	sels := []SelectionInput{
		{ID: "sel-0", FeedProbability: 0.55},
		{ID: "sel-1", FeedProbability: 0.45},
	}
	results := ComputeMarketOdds(sels)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Decimal == 0 {
			t.Errorf("selection %s: unexpected zero price (feed=%.4f, vigged=%.6f)",
				r.ID, r.FeedProbability, r.ViggedProb)
		}
		t.Logf("selection %s: feed=%.4f vigged=%.6f decimal=%.4f american=%d",
			r.ID, r.FeedProbability, r.ViggedProb, r.Decimal, r.American)
	}

	// Check the spread is ~20 cents.
	a0, a1 := results[0].American, results[1].American
	favAm, dogAm := a0, a1
	if a1 < a0 {
		favAm, dogAm = a1, a0
	}
	spread := (-favAm) - dogAm
	if spread < 19 || spread > 21 {
		t.Errorf("spread = %d, want ~20", spread)
	}
}

func TestComputeMarketOdds_ZeroWhenViggedBelowFeed(t *testing.T) {
	// Feed probabilities sum to >1 (feed has its own overround).
	// After normalisation, the vigged prob may be below the original feed prob.
	sels := []SelectionInput{
		{ID: "sel-0", FeedProbability: 0.55},
		{ID: "sel-1", FeedProbability: 0.55},
	}
	results := ComputeMarketOdds(sels)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Normalised fair probs are 0.5/0.5. Vigged at -110/-110 → q ≈ 0.5244.
	// 0.5244 < 0.55 feed prob → both should be zeroed.
	for _, r := range results {
		if r.Decimal != 0 {
			t.Errorf("selection %s: expected zero price (feed=%.4f, vigged=%.6f, decimal=%.4f)",
				r.ID, r.FeedProbability, r.ViggedProb, r.Decimal)
		}
		t.Logf("selection %s: feed=%.4f vigged=%.6f decimal=%.4f american=%d (zeroed=%v)",
			r.ID, r.FeedProbability, r.ViggedProb, r.Decimal, r.American, r.Decimal == 0)
	}
}

func TestComputeMarketOdds_FeedSumsToOne(t *testing.T) {
	// When feed sums to exactly 1, vigged probs are always > fair > feed,
	// so no zeroing should occur.
	sels := []SelectionInput{
		{ID: "sel-0", FeedProbability: 0.6},
		{ID: "sel-1", FeedProbability: 0.4},
	}
	results := ComputeMarketOdds(sels)
	for _, r := range results {
		if r.Decimal == 0 {
			t.Errorf("selection %s: unexpected zero price", r.ID)
		}
		if r.ViggedProb <= r.FeedProbability {
			t.Errorf("selection %s: vigged=%.6f should be > feed=%.4f",
				r.ID, r.ViggedProb, r.FeedProbability)
		}
	}
}

func TestDecimalToAmerican(t *testing.T) {
	tests := []struct {
		dec  float64
		want int
	}{
		{2.50, 150},
		{1.50, -200},
		{2.00, 100},
		{1.952381, -105}, // -105 from 105/205
		{1.0, 0},
	}
	for _, tt := range tests {
		got := DecimalToAmerican(tt.dec)
		if got != tt.want {
			t.Errorf("DecimalToAmerican(%.6f) = %d, want %d", tt.dec, got, tt.want)
		}
	}
}
