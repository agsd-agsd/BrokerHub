package optimizer

import (
	"math"
	"testing"
)

func TestNewPaperMonopolyOptimizerInitializesDefaults(t *testing.T) {
	opt := NewPaperMonopolyOptimizer("hub-1", DefaultPaperMonopolyConfig(15000, 42))

	if opt.CurrentFeeRate != 0.15 {
		t.Fatalf("expected initial fee rate 0.15, got %f", opt.CurrentFeeRate)
	}
	if opt.MinFeeRate != 0.03 || opt.MaxFeeRate != 0.99 {
		t.Fatalf("unexpected fee bounds: min=%f max=%f", opt.MinFeeRate, opt.MaxFeeRate)
	}
	if got := opt.DebugState().Mode; got != "paper_monopoly" {
		t.Fatalf("expected paper_monopoly mode, got %q", got)
	}
}

func TestPaperMonopolyOptimizerKeepsInitialFeeForFirstTwoIterations(t *testing.T) {
	opt := NewPaperMonopolyOptimizer("hub-1", DefaultPaperMonopolyConfig(1000, 42))

	rate1 := opt.Optimize(EpochMetrics{Iteration: 1, CurrentFunds: 1100, CurrentEarn: 10, BrokerCount: 2})
	rate2 := opt.Optimize(EpochMetrics{Iteration: 2, CurrentFunds: 1200, CurrentEarn: 12, BrokerCount: 3})

	if rate1 != 0.15 || rate2 != 0.15 {
		t.Fatalf("expected first two iterations to keep initial fee, got %f and %f", rate1, rate2)
	}
}

func TestPaperMonopolyWarmupPreventsImmediateFloorCut(t *testing.T) {
	opt := NewPaperMonopolyOptimizer("hub-1", DefaultPaperMonopolyConfig(1000, 42))

	lastRate := opt.CurrentFeeRate
	for iteration := 1; iteration <= 15; iteration++ {
		lastRate = opt.Optimize(EpochMetrics{
			Iteration:                iteration,
			ParticipationRate:        0.05,
			BrokerCount:              0,
			CurrentFunds:             1000,
			CurrentEarn:              0,
			StrongestCompetitorFunds: 1200,
			StrongestCompetitorEarn:  12,
		})
	}

	if lastRate < 0.06-1e-9 {
		t.Fatalf("expected warmup floor to keep fee at or above 0.06, got %f", lastRate)
	}
	if lastRate <= opt.MinFeeRate+1e-9 {
		t.Fatalf("expected warmup to avoid collapsing to min fee %f, got %f", opt.MinFeeRate, lastRate)
	}
}

func TestPaperMonopolyCompetitionCutsFeeWhenTrailing(t *testing.T) {
	cfg := DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	opt := NewPaperMonopolyOptimizer("hub-1", cfg)

	rate := opt.Optimize(EpochMetrics{
		Iteration:                3,
		ParticipationRate:        0.2,
		BrokerCount:              4,
		CurrentFunds:             1300,
		CurrentEarn:              10,
		StrongestCompetitorFunds: 2600,
		StrongestCompetitorEarn:  20,
	})

	if rate >= 0.15 {
		t.Fatalf("expected competition phase to cut fee, got %f", rate)
	}
	if opt.DebugState().OptimizerPhase != paperPhaseCompetition {
		t.Fatalf("expected competition phase, got %q", opt.DebugState().OptimizerPhase)
	}
	if delta := rate - 0.15; delta < opt.tuning.CompetitionCutStepMax-1e-9 || delta > opt.tuning.CompetitionRecoveryStepMax+1e-9 {
		t.Fatalf("expected competition move %.6f to stay within tuning bounds [%f, %f]", delta, opt.tuning.CompetitionCutStepMax, opt.tuning.CompetitionRecoveryStepMax)
	}
}

func TestPaperMonopolyCompetitionRecognizesValidatedFeeCut(t *testing.T) {
	cfg := DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	opt := NewPaperMonopolyOptimizer("hub-1", cfg)

	rate1 := opt.Optimize(EpochMetrics{
		Iteration:                3,
		ParticipationRate:        0.2,
		BrokerCount:              4,
		CurrentFunds:             1300,
		CurrentEarn:              10,
		StrongestCompetitorFunds: 2600,
		StrongestCompetitorEarn:  20,
	})
	rate2 := opt.Optimize(EpochMetrics{
		Iteration:                4,
		ParticipationRate:        0.5,
		BrokerCount:              10,
		CurrentFunds:             1800,
		CurrentEarn:              16,
		StrongestCompetitorFunds: 2300,
		StrongestCompetitorEarn:  18,
	})

	if !opt.LastFeeCutValidated {
		t.Fatalf("expected competition phase to validate the previous fee cut")
	}
	if rate2 < rate1-1e-9 {
		t.Fatalf("expected validated cut to stop pushing fee down, got %f -> %f", rate1, rate2)
	}
	if delta := rate2 - rate1; delta > opt.tuning.CompetitionRecoveryStepMax+1e-9 {
		t.Fatalf("expected rebound step %.6f to stay within recovery max %f", delta, opt.tuning.CompetitionRecoveryStepMax)
	}
}

func TestPaperMonopolyDominanceUsesRaiseCooldown(t *testing.T) {
	cfg := DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	cfg.DominanceStreakThreshold = 1
	opt := NewPaperMonopolyOptimizer("hub-1", cfg)

	rate1 := opt.Optimize(EpochMetrics{
		Iteration:                3,
		ParticipationRate:        0.9,
		BrokerCount:              18,
		CurrentFunds:             2600,
		CurrentEarn:              40,
		StrongestCompetitorFunds: 1200,
		StrongestCompetitorEarn:  10,
	})
	rate2 := opt.Optimize(EpochMetrics{
		Iteration:                4,
		ParticipationRate:        0.95,
		BrokerCount:              19,
		CurrentFunds:             2800,
		CurrentEarn:              45,
		StrongestCompetitorFunds: 1100,
		StrongestCompetitorEarn:  8,
	})

	if opt.DebugState().OptimizerPhase != paperPhaseDominance {
		t.Fatalf("expected dominance phase, got %q", opt.DebugState().OptimizerPhase)
	}
	if rate1 <= 0.154 {
		t.Fatalf("expected dominance phase to raise fee by more than the old +0.004 pattern, got %f", rate1)
	}
	if math.Abs(rate2-rate1) > 1e-9 {
		t.Fatalf("expected cooldown to prevent consecutive raises, got %f then %f", rate1, rate2)
	}
	if delta := rate1 - 0.15; delta > opt.tuning.DominanceRaiseStepMax+1e-9 {
		t.Fatalf("expected dominance raise step %.6f to stay within tuning max %f", delta, opt.tuning.DominanceRaiseStepMax)
	}
}

func TestPaperMonopolyOptimizerRecordsCriticalCapOnShock(t *testing.T) {
	cfg := DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	opt := NewPaperMonopolyOptimizer("hub-1", cfg)
	opt.CurrentFeeRate = 0.2
	opt.CurrentPhase = paperPhaseDominance
	opt.LastRaiseEpoch = 3
	opt.ParticipationHistory = []float64{0.95}
	opt.BrokerCountHistory = []int{20}
	opt.InvestedFundsHistory = []float64{1400}
	opt.RevenueHistory = []float64{50}
	opt.FundShareHistory = []float64{0.9}
	opt.FundsHistory = []float64{2400}

	rate := opt.Optimize(EpochMetrics{
		Iteration:                4,
		ParticipationRate:        0.7,
		BrokerCount:              16,
		CurrentFunds:             1700,
		CurrentEarn:              25,
		StrongestCompetitorFunds: 1300,
		StrongestCompetitorEarn:  15,
	})

	debug := opt.DebugState()
	if debug.OptimizerPhase != paperPhaseShock {
		t.Fatalf("expected shock phase, got %q", debug.OptimizerPhase)
	}
	if math.Abs(debug.CriticalMERCap-0.2) > 1e-9 {
		t.Fatalf("expected critical mer cap 0.2, got %f", debug.CriticalMERCap)
	}
	if debug.CriticalMEREpoch != 4 {
		t.Fatalf("expected critical MER epoch 4, got %d", debug.CriticalMEREpoch)
	}
	if debug.ShockExitCount != 4 {
		t.Fatalf("expected shock exit count 4, got %d", debug.ShockExitCount)
	}
	if math.Abs(rate-0.18) > 1e-9 {
		t.Fatalf("expected post-shock fallback fee 0.18, got %f", rate)
	}
}

func TestPaperMonopolyOptimizerIgnoresWarmupShockCap(t *testing.T) {
	cfg := DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	opt := NewPaperMonopolyOptimizer("hub-1", cfg)
	opt.CurrentFeeRate = 0.06
	opt.CurrentPhase = paperPhaseDominance
	opt.LastRaiseEpoch = 3
	opt.ParticipationHistory = []float64{0.95}
	opt.BrokerCountHistory = []int{20}
	opt.InvestedFundsHistory = []float64{1400}
	opt.RevenueHistory = []float64{50}
	opt.FundShareHistory = []float64{0.9}
	opt.FundsHistory = []float64{2400}

	rate := opt.Optimize(EpochMetrics{
		Iteration:                4,
		ParticipationRate:        0.7,
		BrokerCount:              16,
		CurrentFunds:             1700,
		CurrentEarn:              25,
		StrongestCompetitorFunds: 1300,
		StrongestCompetitorEarn:  15,
	})

	debug := opt.DebugState()
	if debug.OptimizerPhase == paperPhaseShock {
		t.Fatalf("expected warmup-level fee to avoid shock phase, got %q", debug.OptimizerPhase)
	}
	if debug.HasCriticalMERCap {
		t.Fatalf("expected no critical cap to be recorded at warmup fee, got %f", debug.CriticalMERCap)
	}
	if rate < opt.MinFeeRate-1e-9 {
		t.Fatalf("expected fee to stay above optimizer min fee %f, got %f", opt.MinFeeRate, rate)
	}
}

func TestPaperMonopolyMemoryPhaseCapsUpperBound(t *testing.T) {
	cfg := DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	cfg.DominanceStreakThreshold = 1
	opt := NewPaperMonopolyOptimizer("hub-1", cfg)
	opt.CurrentFeeRate = 0.18
	opt.HasCriticalMERCap = true
	opt.CriticalMERCap = 0.2
	opt.CriticalMEREpoch = 3
	opt.HistoricalMaxSuccessful = 0.6

	rate := opt.Optimize(EpochMetrics{
		Iteration:                5,
		ParticipationRate:        0.95,
		BrokerCount:              19,
		CurrentFunds:             2600,
		CurrentEarn:              70,
		StrongestCompetitorFunds: 900,
		StrongestCompetitorEarn:  10,
	})

	debug := opt.DebugState()
	if debug.OptimizerPhase != paperPhaseMemory {
		t.Fatalf("expected memory phase, got %q", debug.OptimizerPhase)
	}
	if debug.DynamicUpperBound > 0.19+1e-9 {
		t.Fatalf("expected upper bound to stay below 0.95 * critical cap, got %f", debug.DynamicUpperBound)
	}
	if rate > debug.DynamicUpperBound+1e-9 {
		t.Fatalf("expected fee to stay under capped upper bound, got rate %f > %f", rate, debug.DynamicUpperBound)
	}
	lowerBound, upperBound := opt.memoryBandBounds()
	if upperBound-lowerBound < opt.tuning.MemoryBandMinWidth-1e-9 {
		t.Fatalf("expected memory band width to be at least %f, got [%f, %f]", opt.tuning.MemoryBandMinWidth, lowerBound, upperBound)
	}
	if rate < lowerBound-1e-9 || rate > upperBound+1e-9 {
		t.Fatalf("expected memory rate %f to remain within computed band [%f, %f]", rate, lowerBound, upperBound)
	}
}
