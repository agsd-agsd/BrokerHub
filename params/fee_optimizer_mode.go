package params

import (
	"fmt"
	"strings"
)

const (
	FeeOptimizerModeTaxRate       = "taxrate"
	FeeOptimizerModePaperMonopoly = "paper_monopoly"
)

var FeeOptimizerMode = FeeOptimizerModeTaxRate

func NormalizeFeeOptimizerMode(mode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	switch normalized {
	case "", FeeOptimizerModeTaxRate, "tax":
		return FeeOptimizerModeTaxRate, nil
	case FeeOptimizerModePaperMonopoly, "paper", "monopoly":
		return FeeOptimizerModePaperMonopoly, nil
	default:
		return "", fmt.Errorf(
			"unsupported fee_optimizer %q, expected %q or %q",
			mode,
			FeeOptimizerModeTaxRate,
			FeeOptimizerModePaperMonopoly,
		)
	}
}

func SetFeeOptimizerMode(mode string) error {
	normalized, err := NormalizeFeeOptimizerMode(mode)
	if err != nil {
		return err
	}
	FeeOptimizerMode = normalized
	return nil
}
