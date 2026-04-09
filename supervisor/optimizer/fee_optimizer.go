package optimizer

type FeeOptimizer interface {
	Optimize(input EpochMetrics) float64
	FeeRate() float64
	MinFee() float64
	DebugState() FeeOptimizerDebug
}

type FeeOptimizerDebug struct {
	Mode                     string
	CurrentFeeRate           float64
	MinFeeRate               float64
	LastPredictedInvestment  float64
	DynamicUpperBound        float64
	ConsecutiveSuccess       int
	ConsecutiveFailure       int
	StrongestCompetitorFunds float64
	StrongestCompetitorEarn  float64
	FundShare                float64
	DominanceStreak          int
	CriticalMERCap           float64
	CriticalMEREpoch         int
	HasCriticalMERCap        bool
	ShockExitCount           int
	ShockFundDrop            float64
	OptimizerPhase           string
}
