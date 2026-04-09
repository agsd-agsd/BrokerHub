package params

import "testing"

func TestNormalizeExchangeMode(t *testing.T) {
	tests := map[string]string{
		"":            ExchangeModeInfinite,
		"infinite":    ExchangeModeInfinite,
		"limit100":    ExchangeModeLimit100,
		"limit_100":   ExchangeModeLimit100,
		"limited100":  ExchangeModeLimit100,
		"limited_100": ExchangeModeLimit100,
		"100":         ExchangeModeLimit100,
		"limit300":    ExchangeModeLimit300,
		"limit_300":   ExchangeModeLimit300,
		"limited300":  ExchangeModeLimit300,
		"limited_300": ExchangeModeLimit300,
		"300":         ExchangeModeLimit300,
	}

	for input, expected := range tests {
		got, err := NormalizeExchangeMode(input)
		if err != nil {
			t.Fatalf("NormalizeExchangeMode(%q) returned error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("NormalizeExchangeMode(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestNormalizeExchangeModeRejectsUnknownValue(t *testing.T) {
	if _, err := NormalizeExchangeMode("unknown"); err == nil {
		t.Fatal("expected NormalizeExchangeMode to reject unknown values")
	}
}

func TestExchangeModeEpochLimit(t *testing.T) {
	if got := ExchangeModeEpochLimit(ExchangeModeInfinite); got <= ExchangeModeLimit300Epoch {
		t.Fatalf("expected infinite mode limit to be larger than %d, got %d", ExchangeModeLimit300Epoch, got)
	}
	if got := ExchangeModeEpochLimit(ExchangeModeLimit100); got != ExchangeModeLimit100Epoch {
		t.Fatalf("expected limit100 mode to stop at %d epochs, got %d", ExchangeModeLimit100Epoch, got)
	}
	if got := ExchangeModeEpochLimit(ExchangeModeLimit300); got != ExchangeModeLimit300Epoch {
		t.Fatalf("expected limit300 mode to stop at %d epochs, got %d", ExchangeModeLimit300Epoch, got)
	}
}
