package params

import (
	"fmt"
	"strings"
)

const (
	ExchangeModeInfinite      = "infinite"
	ExchangeModeLimit100      = "limit100"
	ExchangeModeLimit300      = "limit300"
	ExchangeModeLimit100Epoch = 100
	ExchangeModeLimit300Epoch = 300
)

var ExchangeMode = ExchangeModeInfinite

func NormalizeExchangeMode(mode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	switch normalized {
	case "", ExchangeModeInfinite:
		return ExchangeModeInfinite, nil
	case ExchangeModeLimit100, "limit_100", "limited100", "limited_100", "100":
		return ExchangeModeLimit100, nil
	case ExchangeModeLimit300, "limit_300", "limited300", "limited_300", "300":
		return ExchangeModeLimit300, nil
	default:
		return "", fmt.Errorf(
			"unsupported exchange_mode %q, expected %q, %q, or %q",
			mode,
			ExchangeModeInfinite,
			ExchangeModeLimit100,
			ExchangeModeLimit300,
		)
	}
}

func SetExchangeMode(mode string) error {
	normalized, err := NormalizeExchangeMode(mode)
	if err != nil {
		return err
	}
	ExchangeMode = normalized
	return nil
}

func ExchangeModeEpochLimit(mode string) int {
	normalized, err := NormalizeExchangeMode(mode)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	switch normalized {
	case ExchangeModeLimit100:
		return ExchangeModeLimit100Epoch
	case ExchangeModeLimit300:
		return ExchangeModeLimit300Epoch
	}
	return int(^uint(0) >> 1)
}
