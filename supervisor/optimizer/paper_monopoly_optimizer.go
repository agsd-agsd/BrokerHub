package optimizer

import "math"

const (
	paperPhaseCompetition = "competition"
	paperPhaseDominance   = "dominance"
	paperPhaseShock       = "shock"
	paperPhaseMemory      = "memory"

	paperWarmupBrokerThreshold        = 2
	paperWarmupParticipationThreshold = 0.1
	paperDominanceBrokerThreshold     = 2
	paperShockBrokerFloor             = 5
)

type paperMonopolyTuning struct {
	CompetitionCutStepBase     float64
	CompetitionCutStepMax      float64
	CompetitionRecoveryStepMax float64
	DominanceRaiseStepBase     float64
	DominanceRaiseStepMax      float64
	DominancePullbackStep      float64
	MemoryStepUpMax            float64
	MemoryStepDownMax          float64
	MemoryBandMinWidth         float64
	MemoryUpperCapRatio        float64
	MemoryLowerCapRatio        float64
}

func defaultPaperMonopolyTuning() paperMonopolyTuning {
	return paperMonopolyTuning{
		CompetitionCutStepBase:     -0.0035,
		CompetitionCutStepMax:      -0.0120,
		CompetitionRecoveryStepMax: 0.0035,
		DominanceRaiseStepBase:     0.0035,
		DominanceRaiseStepMax:      0.0075,
		DominancePullbackStep:      -0.0040,
		MemoryStepUpMax:            0.0040,
		MemoryStepDownMax:          -0.0055,
		MemoryBandMinWidth:         0.0080,
		MemoryUpperCapRatio:        0.95,
		MemoryLowerCapRatio:        0.88,
	}
}

type PaperMonopolyConfig struct {
	InitialFunds                    float64
	InitialTaxRate                  float64
	MinTaxRate                      float64
	MaxTaxRate                      float64
	WindowSize                      int
	SuccessThreshold                int
	FailureThreshold                int
	EmergencyInvestedFundsThreshold float64
	UpperBoundStartSlack            float64
	UpperBoundFloorSlack            float64
	UpperBoundDecayEpoch            int
	DominanceFundShareThreshold     float64
	DominanceParticipationThreshold float64
	DominanceStreakThreshold        int
	ShockParticipationDropThreshold float64
	ShockBrokerDropThreshold        int
	ShockFundDropThreshold          float64
	WarmupMinFee                    float64
	Seed                            int64
}

type PaperMonopolyOptimizer struct {
	ID                              string
	InitialFunds                    float64
	CurrentFeeRate                  float64
	MinFeeRate                      float64
	MaxFeeRate                      float64
	WindowSize                      int
	SuccessThreshold                int
	FailureThreshold                int
	EmergencyInvestedFundsThreshold float64
	UpperBoundStartSlack            float64
	UpperBoundFloorSlack            float64
	UpperBoundDecayEpoch            int
	DominanceFundShareThreshold     float64
	DominanceParticipationThreshold float64
	DominanceStreakThreshold        int
	ShockParticipationDropThreshold float64
	ShockBrokerDropThreshold        int
	ShockFundDropThreshold          float64
	WarmupMinFee                    float64

	FeeRateHistory       []float64
	RevenueHistory       []float64
	FundsHistory         []float64
	InvestedFundsHistory []float64
	FundShareHistory     []float64
	ParticipationHistory []float64
	BrokerCountHistory   []int

	HistoricalMaxSuccessful      float64
	ConsecutiveSuccess           int
	ConsecutiveFailure           int
	LastUpperBound               float64
	LastStrongestCompetitorFunds float64
	LastStrongestCompetitorEarn  float64
	LastFundShare                float64
	DominanceStreak              int
	CriticalMERCap               float64
	CriticalMEREpoch             int
	LastShockExitCount           int
	LastShockFundDrop            float64
	HasCriticalMERCap            bool
	CurrentPhase                 string

	LastRaiseEpoch            int
	LastFeeCutEpoch           int
	LastFeeCutRevenueBaseline float64
	LastFeeCutFundsBaseline   float64
	LastFeeCutValidated       bool
	tuning                    paperMonopolyTuning
}

func DefaultPaperMonopolyConfig(initialFunds float64, seed int64) PaperMonopolyConfig {
	return PaperMonopolyConfig{
		InitialFunds:                    initialFunds,
		InitialTaxRate:                  0.15,
		MinTaxRate:                      0.03,
		MaxTaxRate:                      0.99,
		WindowSize:                      5,
		SuccessThreshold:                4,
		FailureThreshold:                2,
		EmergencyInvestedFundsThreshold: -1,
		UpperBoundStartSlack:            0.35,
		UpperBoundFloorSlack:            0.08,
		UpperBoundDecayEpoch:            100,
		DominanceFundShareThreshold:     0.8,
		DominanceParticipationThreshold: 0.8,
		DominanceStreakThreshold:        3,
		ShockParticipationDropThreshold: 0.2,
		ShockBrokerDropThreshold:        3,
		ShockFundDropThreshold:          0.15,
		WarmupMinFee:                    0.06,
		Seed:                            seed,
	}
}

func NewPaperMonopolyOptimizer(id string, cfg PaperMonopolyConfig) *PaperMonopolyOptimizer {
	defaults := DefaultPaperMonopolyConfig(cfg.InitialFunds, cfg.Seed)
	if cfg.InitialTaxRate == 0 {
		cfg.InitialTaxRate = defaults.InitialTaxRate
	}
	if cfg.MinTaxRate == 0 {
		cfg.MinTaxRate = defaults.MinTaxRate
	}
	if cfg.MaxTaxRate == 0 {
		cfg.MaxTaxRate = defaults.MaxTaxRate
	}
	if cfg.WindowSize == 0 {
		cfg.WindowSize = defaults.WindowSize
	}
	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = defaults.SuccessThreshold
	}
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = defaults.FailureThreshold
	}
	if cfg.UpperBoundStartSlack == 0 {
		cfg.UpperBoundStartSlack = defaults.UpperBoundStartSlack
	}
	if cfg.UpperBoundFloorSlack == 0 {
		cfg.UpperBoundFloorSlack = defaults.UpperBoundFloorSlack
	}
	if cfg.UpperBoundDecayEpoch == 0 {
		cfg.UpperBoundDecayEpoch = defaults.UpperBoundDecayEpoch
	}
	if cfg.DominanceFundShareThreshold == 0 {
		cfg.DominanceFundShareThreshold = defaults.DominanceFundShareThreshold
	}
	if cfg.DominanceParticipationThreshold == 0 {
		cfg.DominanceParticipationThreshold = defaults.DominanceParticipationThreshold
	}
	if cfg.DominanceStreakThreshold == 0 {
		cfg.DominanceStreakThreshold = defaults.DominanceStreakThreshold
	}
	if cfg.ShockParticipationDropThreshold == 0 {
		cfg.ShockParticipationDropThreshold = defaults.ShockParticipationDropThreshold
	}
	if cfg.ShockBrokerDropThreshold == 0 {
		cfg.ShockBrokerDropThreshold = defaults.ShockBrokerDropThreshold
	}
	if cfg.ShockFundDropThreshold == 0 {
		cfg.ShockFundDropThreshold = defaults.ShockFundDropThreshold
	}
	if cfg.WarmupMinFee == 0 {
		cfg.WarmupMinFee = defaults.WarmupMinFee
	}

	return &PaperMonopolyOptimizer{
		ID:                              id,
		InitialFunds:                    cfg.InitialFunds,
		CurrentFeeRate:                  cfg.InitialTaxRate,
		MinFeeRate:                      cfg.MinTaxRate,
		MaxFeeRate:                      cfg.MaxTaxRate,
		WindowSize:                      cfg.WindowSize,
		SuccessThreshold:                cfg.SuccessThreshold,
		FailureThreshold:                cfg.FailureThreshold,
		EmergencyInvestedFundsThreshold: cfg.EmergencyInvestedFundsThreshold,
		UpperBoundStartSlack:            cfg.UpperBoundStartSlack,
		UpperBoundFloorSlack:            cfg.UpperBoundFloorSlack,
		UpperBoundDecayEpoch:            cfg.UpperBoundDecayEpoch,
		DominanceFundShareThreshold:     cfg.DominanceFundShareThreshold,
		DominanceParticipationThreshold: cfg.DominanceParticipationThreshold,
		DominanceStreakThreshold:        cfg.DominanceStreakThreshold,
		ShockParticipationDropThreshold: cfg.ShockParticipationDropThreshold,
		ShockBrokerDropThreshold:        cfg.ShockBrokerDropThreshold,
		ShockFundDropThreshold:          cfg.ShockFundDropThreshold,
		WarmupMinFee:                    cfg.WarmupMinFee,
		FeeRateHistory:                  []float64{cfg.InitialTaxRate},
		RevenueHistory:                  make([]float64, 0),
		FundsHistory:                    make([]float64, 0),
		InvestedFundsHistory:            make([]float64, 0),
		FundShareHistory:                make([]float64, 0),
		ParticipationHistory:            make([]float64, 0),
		BrokerCountHistory:              make([]int, 0),
		HistoricalMaxSuccessful:         cfg.InitialTaxRate,
		LastUpperBound:                  cfg.InitialTaxRate,
		CurrentPhase:                    paperPhaseCompetition,
		LastFeeCutEpoch:                 -1,
		LastRaiseEpoch:                  -100,
		tuning:                          defaultPaperMonopolyTuning(),
	}
}

func (o *PaperMonopolyOptimizer) Optimize(input EpochMetrics) float64 {
	investedFunds := math.Max(0, input.CurrentFunds-o.InitialFunds)
	competitorInvestedFunds := math.Max(0, input.StrongestCompetitorFunds-o.InitialFunds)
	fundShare := input.CurrentFunds / math.Max(input.CurrentFunds+input.StrongestCompetitorFunds, 1e-9)
	effectiveMinFee := o.effectiveMinFee(input.BrokerCount, input.ParticipationRate)

	prevParticipation := latestFloat64(o.ParticipationHistory, input.ParticipationRate)
	prevBrokerCount := latestInt(o.BrokerCountHistory, input.BrokerCount)
	prevInvestedFunds := latestFloat64(o.InvestedFundsHistory, investedFunds)

	o.LastStrongestCompetitorFunds = input.StrongestCompetitorFunds
	o.LastStrongestCompetitorEarn = input.StrongestCompetitorEarn
	o.LastFundShare = fundShare
	o.LastShockExitCount = 0
	o.LastShockFundDrop = 0

	o.recordState(input, investedFunds, fundShare)
	o.updateSuccessFailure()
	o.updateDominanceStreak(fundShare, input.ParticipationRate, input.BrokerCount)
	o.LastFeeCutValidated = o.detectValidatedCut(input.Iteration, input.CurrentEarn, investedFunds)

	if input.Iteration <= 2 {
		o.CurrentFeeRate = clamp(o.CurrentFeeRate, effectiveMinFee, o.MaxFeeRate)
		o.LastUpperBound = clamp(o.CurrentFeeRate, effectiveMinFee, o.MaxFeeRate)
		o.FeeRateHistory = append(o.FeeRateHistory, o.CurrentFeeRate)
		return o.CurrentFeeRate
	}

	if o.detectShock(input.Iteration, prevParticipation, input.ParticipationRate, prevBrokerCount, input.BrokerCount, prevInvestedFunds, investedFunds) {
		o.CurrentPhase = paperPhaseShock
		o.CurrentFeeRate = math.Max(effectiveMinFee, 0.9*o.CriticalMERCap)
		o.LastUpperBound = o.capAwareUpperBound(input.Iteration)
		o.FeeRateHistory = append(o.FeeRateHistory, o.CurrentFeeRate)
		return o.CurrentFeeRate
	}

	o.CurrentPhase = o.determinePhase(fundShare, input.ParticipationRate, input.BrokerCount)
	upperBound := o.capAwareUpperBound(input.Iteration)
	if o.CurrentPhase == paperPhaseDominance && !o.HasCriticalMERCap {
		upperBound = math.Max(
			upperBound,
			clamp(o.CurrentFeeRate+0.08, effectiveMinFee, o.MaxFeeRate),
		)
	}
	revenueSlope, fundsSlope := o.recentTrends()

	var adjustment float64
	switch o.CurrentPhase {
	case paperPhaseDominance:
		adjustment = o.dominanceAdjustment(input.Iteration, investedFunds, competitorInvestedFunds, fundShare, revenueSlope, fundsSlope)
	case paperPhaseMemory:
		adjustment = o.memoryAdjustment(fundShare, revenueSlope, fundsSlope)
	default:
		adjustment = o.competitionAdjustment(
			investedFunds,
			prevInvestedFunds,
			competitorInvestedFunds,
			fundShare,
			input.ParticipationRate,
			input.BrokerCount,
			revenueSlope,
			fundsSlope,
		)
	}

	upperBound = math.Max(upperBound, effectiveMinFee)
	proposedRate := clamp(o.CurrentFeeRate+adjustment, effectiveMinFee, upperBound)
	o.trackFeeChange(input.Iteration, input.CurrentEarn, investedFunds, proposedRate)
	o.CurrentFeeRate = proposedRate
	o.LastUpperBound = upperBound
	o.FeeRateHistory = append(o.FeeRateHistory, o.CurrentFeeRate)
	return o.CurrentFeeRate
}

func (o *PaperMonopolyOptimizer) FeeRate() float64 {
	return o.CurrentFeeRate
}

func (o *PaperMonopolyOptimizer) MinFee() float64 {
	return o.MinFeeRate
}

func (o *PaperMonopolyOptimizer) DebugState() FeeOptimizerDebug {
	return FeeOptimizerDebug{
		Mode:                     "paper_monopoly",
		CurrentFeeRate:           o.CurrentFeeRate,
		MinFeeRate:               o.MinFeeRate,
		LastPredictedInvestment:  0,
		DynamicUpperBound:        o.LastUpperBound,
		ConsecutiveSuccess:       o.ConsecutiveSuccess,
		ConsecutiveFailure:       o.ConsecutiveFailure,
		StrongestCompetitorFunds: o.LastStrongestCompetitorFunds,
		StrongestCompetitorEarn:  o.LastStrongestCompetitorEarn,
		FundShare:                o.LastFundShare,
		DominanceStreak:          o.DominanceStreak,
		CriticalMERCap:           o.CriticalMERCap,
		CriticalMEREpoch:         o.CriticalMEREpoch,
		HasCriticalMERCap:        o.HasCriticalMERCap,
		ShockExitCount:           o.LastShockExitCount,
		ShockFundDrop:            o.LastShockFundDrop,
		OptimizerPhase:           o.CurrentPhase,
	}
}

func (o *PaperMonopolyOptimizer) recordState(input EpochMetrics, investedFunds, fundShare float64) {
	o.RevenueHistory = append(o.RevenueHistory, input.CurrentEarn)
	o.FundsHistory = append(o.FundsHistory, input.CurrentFunds)
	o.InvestedFundsHistory = append(o.InvestedFundsHistory, investedFunds)
	o.ParticipationHistory = append(o.ParticipationHistory, input.ParticipationRate)
	o.FundShareHistory = append(o.FundShareHistory, fundShare)
	o.BrokerCountHistory = append(o.BrokerCountHistory, input.BrokerCount)
}

func (o *PaperMonopolyOptimizer) updateSuccessFailure() {
	if len(o.RevenueHistory) < o.WindowSize || len(o.InvestedFundsHistory) < o.WindowSize {
		return
	}

	recentRevenues := o.RevenueHistory[len(o.RevenueHistory)-o.WindowSize:]
	revenueSlope := CalculateSlope(recentRevenues)
	meanRevenue := Mean(recentRevenues)
	revenueChange := revenueSlope / math.Max(Abs(meanRevenue), 1)

	recentFunds := o.InvestedFundsHistory[len(o.InvestedFundsHistory)-o.WindowSize:]
	fundsSlope := CalculateSlope(recentFunds)
	meanFunds := Mean(recentFunds)
	fundsChange := fundsSlope / math.Max(Abs(meanFunds), 1)

	if revenueChange >= 1e-5 || fundsChange >= 1e-5 {
		o.ConsecutiveSuccess++
		o.ConsecutiveFailure = 0
	} else if revenueChange <= -1e-5 || fundsChange <= -1e-5 {
		o.ConsecutiveFailure++
		o.ConsecutiveSuccess = 0
	}

	if o.ConsecutiveSuccess >= o.SuccessThreshold {
		o.HistoricalMaxSuccessful = math.Min(o.MaxFeeRate, math.Max(o.HistoricalMaxSuccessful, o.CurrentFeeRate+0.015))
		o.ConsecutiveSuccess = 0
	}

	if o.ConsecutiveFailure >= o.FailureThreshold {
		o.HistoricalMaxSuccessful = math.Max(o.MinFeeRate+0.01, o.HistoricalMaxSuccessful*0.96)
		o.ConsecutiveFailure = 0
	}
}

func (o *PaperMonopolyOptimizer) updateDominanceStreak(fundShare, participationRate float64, brokerCount int) {
	if brokerCount >= paperDominanceBrokerThreshold &&
		(fundShare >= o.DominanceFundShareThreshold || participationRate >= o.DominanceParticipationThreshold) {
		o.DominanceStreak++
		return
	}
	o.DominanceStreak = 0
}

func (o *PaperMonopolyOptimizer) determinePhase(fundShare, participationRate float64, brokerCount int) string {
	if brokerCount >= paperDominanceBrokerThreshold && o.DominanceStreak >= o.DominanceStreakThreshold {
		if o.HasCriticalMERCap {
			return paperPhaseMemory
		}
		return paperPhaseDominance
	}
	if fundShare < o.DominanceFundShareThreshold && participationRate < o.DominanceParticipationThreshold {
		return paperPhaseCompetition
	}
	return paperPhaseCompetition
}

func (o *PaperMonopolyOptimizer) detectValidatedCut(iteration int, currentRevenue, investedFunds float64) bool {
	if o.LastFeeCutEpoch < 0 || iteration <= o.LastFeeCutEpoch {
		return false
	}
	if iteration > o.LastFeeCutEpoch+3 {
		return false
	}
	return investedFunds > o.LastFeeCutFundsBaseline && currentRevenue > o.LastFeeCutRevenueBaseline
}

func (o *PaperMonopolyOptimizer) detectShock(iteration int, prevParticipation, currentParticipation float64, prevBrokerCount, currentBrokerCount int, prevInvestedFunds, investedFunds float64) bool {
	if o.CurrentPhase != paperPhaseDominance {
		return false
	}
	if prevBrokerCount < paperShockBrokerFloor {
		return false
	}
	if o.CurrentFeeRate <= o.WarmupMinFee {
		return false
	}
	if !o.raiseWithinWindow(iteration, 2) {
		return false
	}

	partDrop := math.Max(0, prevParticipation-currentParticipation)
	brokerDrop := prevBrokerCount - currentBrokerCount
	requiredBrokerDrop := maxInt(o.ShockBrokerDropThreshold, int(math.Ceil(0.15*float64(prevBrokerCount))))
	fundDropRatio := 0.0
	if prevInvestedFunds > 1e-9 {
		fundDropRatio = math.Max(0, (prevInvestedFunds-investedFunds)/prevInvestedFunds)
	}

	if partDrop < o.ShockParticipationDropThreshold &&
		brokerDrop < requiredBrokerDrop &&
		fundDropRatio < o.ShockFundDropThreshold {
		return false
	}

	o.LastShockExitCount = maxInt(brokerDrop, 0)
	o.LastShockFundDrop = fundDropRatio
	if !o.HasCriticalMERCap || o.CurrentFeeRate < o.CriticalMERCap {
		o.CriticalMERCap = o.CurrentFeeRate
		o.CriticalMEREpoch = iteration
		o.HasCriticalMERCap = true
	}
	return true
}

func (o *PaperMonopolyOptimizer) raiseWithinWindow(iteration, window int) bool {
	if o.LastRaiseEpoch < 0 {
		return false
	}
	return iteration-o.LastRaiseEpoch >= 1 && iteration-o.LastRaiseEpoch <= window
}

func (o *PaperMonopolyOptimizer) recentTrends() (float64, float64) {
	revenueWindow := minInt(o.WindowSize, len(o.RevenueHistory))
	fundWindow := minInt(o.WindowSize, len(o.InvestedFundsHistory))
	if revenueWindow == 0 || fundWindow == 0 {
		return 0, 0
	}

	recentRevenues := o.RevenueHistory[len(o.RevenueHistory)-revenueWindow:]
	recentFunds := o.InvestedFundsHistory[len(o.InvestedFundsHistory)-fundWindow:]
	return CalculateSlope(recentRevenues), CalculateSlope(recentFunds)
}

func (o *PaperMonopolyOptimizer) competitionAdjustment(investedFunds, prevInvestedFunds, competitorInvestedFunds, fundShare, participationRate float64, brokerCount int, revenueSlope, fundsSlope float64) float64 {
	tuning := o.tuning
	investedFundsRatio := 1.0
	if prevInvestedFunds > 0 {
		investedFundsRatio = investedFunds / math.Max(prevInvestedFunds, 1)
	}
	warmupActive := brokerCount < paperWarmupBrokerThreshold && participationRate < paperWarmupParticipationThreshold
	cutFloor := tuning.CompetitionCutStepMax
	if warmupActive {
		cutFloor = math.Max(cutFloor, tuning.CompetitionCutStepBase)
	}
	if prevInvestedFunds > 0 && investedFundsRatio <= 0.2 &&
		(o.EmergencyInvestedFundsThreshold < 0 || investedFunds <= o.EmergencyInvestedFundsThreshold) {
		return cutFloor
	}

	fundsGapRatio := (competitorInvestedFunds - investedFunds) / math.Max(competitorInvestedFunds+investedFunds, 1)
	adjustment := tuning.CompetitionCutStepBase

	if fundsGapRatio > 0.03 {
		adjustment -= math.Min(math.Abs(cutFloor-tuning.CompetitionCutStepBase), 0.025*fundsGapRatio+0.003)
	}
	if participationRate < 0.45 {
		adjustment -= 0.0020
	}
	if revenueSlope < 0 {
		adjustment -= 0.0025
	}
	if fundsSlope < 0 {
		adjustment -= 0.0025
	}
	if o.LastFeeCutValidated {
		if revenueSlope >= 0 && fundsSlope >= 0 {
			return clamp(tuning.CompetitionRecoveryStepMax*0.6, cutFloor, tuning.CompetitionRecoveryStepMax)
		}
		adjustment += 0.004
	}
	if revenueSlope > 0 && fundsSlope > 0 && fundShare > 0.62 {
		adjustment += tuning.CompetitionRecoveryStepMax * 0.5
	}
	if fundsGapRatio < -0.08 && revenueSlope > 0 && fundsSlope > 0 {
		adjustment += tuning.CompetitionRecoveryStepMax * 0.35
	}
	return clamp(adjustment, cutFloor, tuning.CompetitionRecoveryStepMax)
}

func (o *PaperMonopolyOptimizer) dominanceAdjustment(iteration int, investedFunds, competitorInvestedFunds, fundShare, revenueSlope, fundsSlope float64) float64 {
	tuning := o.tuning
	if iteration <= o.LastRaiseEpoch+1 {
		if revenueSlope < 0 || fundsSlope < 0 {
			return tuning.DominancePullbackStep
		}
		return 0
	}

	if revenueSlope < 0 || fundsSlope < 0 {
		return tuning.DominancePullbackStep
	}

	leadRatio := math.Max(0, (investedFunds-competitorInvestedFunds)/math.Max(investedFunds+competitorInvestedFunds, 1))
	adjustment := tuning.DominanceRaiseStepBase
	if leadRatio > 0.1 || fundShare > 0.9 {
		adjustment = tuning.DominanceRaiseStepMax
	} else if leadRatio < 0.03 {
		adjustment = tuning.DominanceRaiseStepBase * 0.6
	}
	return clamp(adjustment, tuning.DominancePullbackStep, tuning.DominanceRaiseStepMax)
}

func (o *PaperMonopolyOptimizer) memoryAdjustment(fundShare, revenueSlope, fundsSlope float64) float64 {
	if o.CriticalMERCap <= 0 {
		return 0
	}

	lowerMemoryBound, upperMemoryBound := o.memoryBandBounds()

	anchor := lowerMemoryBound + 0.5*(upperMemoryBound-lowerMemoryBound)
	if fundShare > 0.92 && revenueSlope >= 0 && fundsSlope >= 0 {
		anchor = upperMemoryBound - 0.001
	}
	if revenueSlope < 0 || fundsSlope < 0 {
		anchor = lowerMemoryBound
	}
	anchor = clamp(anchor, lowerMemoryBound, upperMemoryBound)
	return clamp(anchor-o.CurrentFeeRate, o.tuning.MemoryStepDownMax, o.tuning.MemoryStepUpMax)
}

func (o *PaperMonopolyOptimizer) memoryBandBounds() (float64, float64) {
	upperMemoryBound := math.Min(o.CriticalMERCap*o.tuning.MemoryUpperCapRatio, o.CriticalMERCap-0.001)
	lowerMemoryBound := math.Max(
		o.MinFeeRate,
		math.Min(
			o.CriticalMERCap*o.tuning.MemoryLowerCapRatio,
			upperMemoryBound-o.tuning.MemoryBandMinWidth,
		),
	)
	if upperMemoryBound-lowerMemoryBound < o.tuning.MemoryBandMinWidth {
		lowerMemoryBound = math.Max(o.MinFeeRate, upperMemoryBound-o.tuning.MemoryBandMinWidth)
	}
	if lowerMemoryBound > upperMemoryBound {
		lowerMemoryBound = upperMemoryBound
	}
	return lowerMemoryBound, upperMemoryBound
}

func (o *PaperMonopolyOptimizer) capAwareUpperBound(iteration int) float64 {
	upperBound := clamp(
		o.HistoricalMaxSuccessful*(1+o.decayedSlack(iteration)),
		o.MinFeeRate,
		o.MaxFeeRate,
	)
	if o.HasCriticalMERCap {
		upperBound = math.Min(upperBound, o.tuning.MemoryUpperCapRatio*o.CriticalMERCap)
	}
	return clamp(upperBound, o.MinFeeRate, o.MaxFeeRate)
}

func (o *PaperMonopolyOptimizer) trackFeeChange(iteration int, currentRevenue, investedFunds, proposedRate float64) {
	switch {
	case proposedRate > o.CurrentFeeRate+1e-9:
		o.LastRaiseEpoch = iteration
	case proposedRate < o.CurrentFeeRate-1e-9:
		o.LastFeeCutEpoch = iteration
		o.LastFeeCutRevenueBaseline = currentRevenue
		o.LastFeeCutFundsBaseline = investedFunds
		o.LastFeeCutValidated = false
	}
}

func (o *PaperMonopolyOptimizer) decayedSlack(iteration int) float64 {
	if o.UpperBoundDecayEpoch <= 0 || iteration >= o.UpperBoundDecayEpoch {
		return o.UpperBoundFloorSlack
	}
	progress := float64(maxInt(iteration-1, 0)) / float64(o.UpperBoundDecayEpoch)
	return o.UpperBoundStartSlack - progress*(o.UpperBoundStartSlack-o.UpperBoundFloorSlack)
}

func (o *PaperMonopolyOptimizer) effectiveMinFee(brokerCount int, participationRate float64) float64 {
	if brokerCount < paperWarmupBrokerThreshold && participationRate < paperWarmupParticipationThreshold {
		return math.Max(o.MinFeeRate, o.WarmupMinFee)
	}
	return o.MinFeeRate
}

func latestFloat64(values []float64, fallback float64) float64 {
	if len(values) == 0 {
		return fallback
	}
	return values[len(values)-1]
}

func latestInt(values []int, fallback int) int {
	if len(values) == 0 {
		return fallback
	}
	return values[len(values)-1]
}

func maxInt(value, fallback int) int {
	if value > fallback {
		return value
	}
	return fallback
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
