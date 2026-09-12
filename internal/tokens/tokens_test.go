package tokens

import "testing"

func TestEstimateGrowsWithText(t *testing.T) {
	short := EstimateText("привет")
	big := EstimateText(repeat("слово ", 500))
	if short <= 0 {
		t.Fatalf("short estimate must be > 0")
	}
	if big <= short {
		t.Fatalf("long text should estimate more tokens: short=%d big=%d", short, big)
	}
}

func TestPricing(t *testing.T) {
	p := DefaultFlashPricing()
	cost := p.CostUSD(1_000_000, 1_000_000, 0, 1_000_000)
	if cost < 0.41 || cost > 0.43 {
		t.Fatalf("unexpected cost %v", cost)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
