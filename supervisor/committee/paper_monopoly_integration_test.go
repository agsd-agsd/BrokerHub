package committee

import (
	"math"
	"testing"

	optimizerPkg "blockEmulator/supervisor/optimizer"
)

func TestPaperMonopolyLifecycleCapturesCompetitionShockAndMemory(t *testing.T) {
	cfg := optimizerPkg.DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	cfg.DominanceStreakThreshold = 1
	opt := optimizerPkg.NewPaperMonopolyOptimizer("hub-1", cfg)

	rateAfterCompetition := opt.Optimize(optimizerPkg.EpochMetrics{
		Iteration:                3,
		ParticipationRate:        0.2,
		BrokerCount:              4,
		CurrentFunds:             1300,
		CurrentEarn:              10,
		StrongestCompetitorFunds: 2600,
		StrongestCompetitorEarn:  20,
	})
	if rateAfterCompetition >= 0.15 {
		t.Fatalf("expected competition phase to cut fee, got %f", rateAfterCompetition)
	}
	if opt.DebugState().OptimizerPhase != "competition" {
		t.Fatalf("expected competition phase, got %q", opt.DebugState().OptimizerPhase)
	}

	rateAfterValidation := opt.Optimize(optimizerPkg.EpochMetrics{
		Iteration:                4,
		ParticipationRate:        0.55,
		BrokerCount:              11,
		CurrentFunds:             1800,
		CurrentEarn:              16,
		StrongestCompetitorFunds: 1600,
		StrongestCompetitorEarn:  14,
	})
	if rateAfterValidation < rateAfterCompetition-1e-9 {
		t.Fatalf("expected validated fee cut to stop pushing fee lower, got %f -> %f", rateAfterCompetition, rateAfterValidation)
	}

	rateBeforeShock := opt.Optimize(optimizerPkg.EpochMetrics{
		Iteration:                5,
		ParticipationRate:        0.92,
		BrokerCount:              18,
		CurrentFunds:             2700,
		CurrentEarn:              45,
		StrongestCompetitorFunds: 1200,
		StrongestCompetitorEarn:  10,
	})
	if opt.DebugState().OptimizerPhase != "dominance" {
		t.Fatalf("expected dominance phase before shock, got %q", opt.DebugState().OptimizerPhase)
	}

	rateAfterShock := opt.Optimize(optimizerPkg.EpochMetrics{
		Iteration:                6,
		ParticipationRate:        0.7,
		BrokerCount:              14,
		CurrentFunds:             1800,
		CurrentEarn:              20,
		StrongestCompetitorFunds: 1400,
		StrongestCompetitorEarn:  15,
	})
	debugAfterShock := opt.DebugState()
	if debugAfterShock.OptimizerPhase != "shock" {
		t.Fatalf("expected shock phase, got %q", debugAfterShock.OptimizerPhase)
	}
	if !debugAfterShock.HasCriticalMERCap {
		t.Fatal("expected shock to record a critical MER cap")
	}
	if math.Abs(debugAfterShock.CriticalMERCap-rateBeforeShock) > 1e-9 {
		t.Fatalf("expected critical cap to match the pre-shock fee %.6f, got %.6f", rateBeforeShock, debugAfterShock.CriticalMERCap)
	}
	if math.Abs(rateAfterShock-0.9*debugAfterShock.CriticalMERCap) > 1e-9 {
		t.Fatalf("expected post-shock fee to fall back to 90%% of cap, got %f with cap %f", rateAfterShock, debugAfterShock.CriticalMERCap)
	}

	rateInMemory := opt.Optimize(optimizerPkg.EpochMetrics{
		Iteration:                7,
		ParticipationRate:        0.94,
		BrokerCount:              19,
		CurrentFunds:             2500,
		CurrentEarn:              48,
		StrongestCompetitorFunds: 1000,
		StrongestCompetitorEarn:  8,
	})
	debugInMemory := opt.DebugState()
	if debugInMemory.OptimizerPhase != "memory" {
		t.Fatalf("expected memory phase after the shock, got %q", debugInMemory.OptimizerPhase)
	}
	if debugInMemory.DynamicUpperBound > 0.95*debugAfterShock.CriticalMERCap+1e-9 {
		t.Fatalf("expected memory upper bound to stay below 95%% of cap, got %f", debugInMemory.DynamicUpperBound)
	}
	if rateInMemory > debugInMemory.DynamicUpperBound+1e-9 {
		t.Fatalf("expected memory fee to stay under capped upper bound, got rate %f > %f", rateInMemory, debugInMemory.DynamicUpperBound)
	}
}

func TestPaperMonopolyDominanceCooldownWithinCommitteeSuite(t *testing.T) {
	cfg := optimizerPkg.DefaultPaperMonopolyConfig(1000, 42)
	cfg.EmergencyInvestedFundsThreshold = -1
	cfg.DominanceStreakThreshold = 1
	opt := optimizerPkg.NewPaperMonopolyOptimizer("hub-1", cfg)

	rate1 := opt.Optimize(optimizerPkg.EpochMetrics{
		Iteration:                3,
		ParticipationRate:        0.92,
		BrokerCount:              18,
		CurrentFunds:             2600,
		CurrentEarn:              40,
		StrongestCompetitorFunds: 1200,
		StrongestCompetitorEarn:  10,
	})
	rate2 := opt.Optimize(optimizerPkg.EpochMetrics{
		Iteration:                4,
		ParticipationRate:        0.95,
		BrokerCount:              19,
		CurrentFunds:             2800,
		CurrentEarn:              45,
		StrongestCompetitorFunds: 1100,
		StrongestCompetitorEarn:  8,
	})

	if rate1 <= 0.15 {
		t.Fatalf("expected dominance phase to raise fee, got %f", rate1)
	}
	if math.Abs(rate2-rate1) > 1e-9 {
		t.Fatalf("expected dominance cooldown to prevent consecutive raises, got %f then %f", rate1, rate2)
	}
}
