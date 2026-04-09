package params

import "testing"

func TestNormalizeFeeOptimizerMode(t *testing.T) {
	tests := map[string]string{
		"":                FeeOptimizerModeTaxRate,
		"taxrate":         FeeOptimizerModeTaxRate,
		"tax":             FeeOptimizerModeTaxRate,
		"paper_monopoly":  FeeOptimizerModePaperMonopoly,
		"paper":           FeeOptimizerModePaperMonopoly,
		"monopoly":        FeeOptimizerModePaperMonopoly,
	}

	for input, expected := range tests {
		got, err := NormalizeFeeOptimizerMode(input)
		if err != nil {
			t.Fatalf("NormalizeFeeOptimizerMode(%q) returned error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("NormalizeFeeOptimizerMode(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestNormalizeFeeOptimizerModeRejectsUnknownValue(t *testing.T) {
	if _, err := NormalizeFeeOptimizerMode("unknown"); err == nil {
		t.Fatal("expected NormalizeFeeOptimizerMode to reject unknown values")
	}
}
