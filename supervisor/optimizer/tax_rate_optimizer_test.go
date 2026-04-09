package optimizer

import (
	"math"
	"testing"
)

func TestNewTaxRateOptimizerInitializesDefaults(t *testing.T) {
	opt := NewTaxRateOptimizer("hub-1", DefaultTaxOptimizerConfig(15000))

	if opt.CurrentFeeRate != 0.15 {
		t.Fatalf("expected initial fee rate 0.15, got %f", opt.CurrentFeeRate)
	}
	if len(opt.FeeRateHistory) != 1 {
		t.Fatalf("expected one fee history entry, got %d", len(opt.FeeRateHistory))
	}
	if opt.MinFeeRate != 0.001 || opt.MaxFeeRate != 0.99 {
		t.Fatalf("unexpected fee bounds: min=%f max=%f", opt.MinFeeRate, opt.MaxFeeRate)
	}
}

func TestTaxRateOptimizerHistoryCaps(t *testing.T) {
	opt := NewTaxRateOptimizer("hub-1", DefaultTaxOptimizerConfig(15000))
	txs := []TransactionSample{{
		Fee:      12,
		Amount:   100,
		Sender:   "sender",
		Receiver: "receiver",
	}}

	for i := 0; i < 55; i++ {
		opt.Optimize(EpochMetrics{
			Iteration:         i + 1,
			ParticipationRate: 0.2,
			CurrentFunds:      15000 + float64(i*10),
			CurrentEarn:       100 + float64(i%5),
			Transactions:      txs,
		})
	}

	if got := len(opt.DeltaData); got != 50 {
		t.Fatalf("expected delta history length 50, got %d", got)
	}
	if got := len(opt.InvestmentData); got != 50 {
		t.Fatalf("expected investment history length 50, got %d", got)
	}
	if got := len(opt.HistoryTransactionData); got != 10 {
		t.Fatalf("expected transaction history length 10, got %d", got)
	}
}

func TestTaxRateOptimizerLowParticipationReducesFeeWithinBounds(t *testing.T) {
	opt := NewTaxRateOptimizer("hub-1", DefaultTaxOptimizerConfig(15000))
	before := opt.CurrentFeeRate

	after := opt.Optimize(EpochMetrics{
		Iteration:         2,
		ParticipationRate: 0.05,
		CurrentFunds:      15000,
		CurrentEarn:       10,
	})

	if after >= before {
		t.Fatalf("expected low participation to reduce fee, before=%f after=%f", before, after)
	}
	if after < opt.MinFeeRate || after > opt.MaxFeeRate {
		t.Fatalf("fee rate out of bounds: %f", after)
	}
}

func TestAdaptiveLearningRateIncreasesWithVolatility(t *testing.T) {
	opt := NewTaxRateOptimizer("hub-1", DefaultTaxOptimizerConfig(15000))
	opt.RevenueHistory = []float64{1, 10, 2, 11, 3}

	adaptive := opt.adaptiveLearningRate()
	if adaptive <= opt.LearningRate {
		t.Fatalf("expected adaptive learning rate to exceed base rate, base=%f adaptive=%f", opt.LearningRate, adaptive)
	}
}

func TestPredictInvestmentNeverGoesNegative(t *testing.T) {
	opt := NewTaxRateOptimizer("hub-1", DefaultTaxOptimizerConfig(15000))
	for i := 0; i < 12; i++ {
		opt.updateDeltaInvestmentModel(0.05+0.02*float64(i), 200-float64(i*20))
	}

	predicted := opt.predictInvestment(1.5)
	if predicted < 0 {
		t.Fatalf("predicted investment should be non-negative, got %f", predicted)
	}
}

func TestTaxRateOptimizerSmoke(t *testing.T) {
	opt := NewTaxRateOptimizer("hub-1", DefaultTaxOptimizerConfig(15000))
	txs := []TransactionSample{
		{Fee: 10, Amount: 120, Sender: "a", Receiver: "b"},
		{Fee: 15, Amount: 80, Sender: "c", Receiver: "d"},
		{Fee: 8, Amount: 60, Sender: "e", Receiver: "f"},
	}

	rates := make([]float64, 0, 5)
	for i := 0; i < 5; i++ {
		rate := opt.Optimize(EpochMetrics{
			Iteration:         i + 1,
			ParticipationRate: 0.2,
			CurrentFunds:      16000 + float64(i*100),
			CurrentEarn:       80 + float64((i%2)*40),
			Transactions:      txs,
		})
		if rate < opt.MinFeeRate || rate > opt.MaxFeeRate {
			t.Fatalf("rate out of bounds at iteration %d: %f", i+1, rate)
		}
		rates = append(rates, rate)
	}

	allSame := true
	for i := 1; i < len(rates); i++ {
		if math.Abs(rates[i]-rates[0]) > 1e-9 {
			allSame = false
			break
		}
	}
	if allSame {
		t.Fatalf("expected fee rate to evolve across epochs, got %v", rates)
	}
}
