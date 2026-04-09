package optimizer

import (
	"math"
	"sort"
)

type TaxOptimizerConfig struct {
	InitialFunds   float64
	InitialTaxRate float64
	MinTaxRate     float64
	MaxTaxRate     float64
	LearningRate   float64
	MemorySize     int
	MinDataPoints  int
}

type TransactionSample struct {
	Fee      float64
	Amount   float64
	Sender   string
	Receiver string
}

type EpochMetrics struct {
	Iteration                int
	ParticipationRate        float64
	BrokerCount              int
	CurrentFunds             float64
	CurrentEarn              float64
	StrongestCompetitorFunds float64
	StrongestCompetitorEarn  float64
	Transactions             []TransactionSample
}

type TaxRateOptimizer struct {
	ID                    string
	InitialFunds          float64
	MinFeeRate            float64
	MaxFeeRate            float64
	CurrentFeeRate        float64
	LearningRate          float64
	MemorySize            int
	MinDataPoints         int
	LastParticipationRate float64

	RevenueHistory         []float64
	ParticipationHistory   []float64
	FeeRateHistory         []float64
	DeltaData              []float64
	InvestmentData         []float64
	HistoryTransactionData [][]TransactionSample

	investmentPredictor          func(float64) float64
	LastPredictedInvestment      float64
	LastStrongestCompetitorFunds float64
	LastStrongestCompetitorEarn  float64
}

type predictedTransaction struct {
	fee      float64
	amount   float64
	sender   string
	receiver string
}

func DefaultTaxOptimizerConfig(initialFunds float64) TaxOptimizerConfig {
	return TaxOptimizerConfig{
		InitialFunds:   initialFunds,
		InitialTaxRate: 0.15,
		MinTaxRate:     0.001,
		MaxTaxRate:     0.99,
		LearningRate:   0.1,
		MemorySize:     5,
		MinDataPoints:  5,
	}
}

func NewTaxRateOptimizer(id string, cfg TaxOptimizerConfig) *TaxRateOptimizer {
	defaults := DefaultTaxOptimizerConfig(cfg.InitialFunds)
	if cfg.InitialTaxRate == 0 {
		cfg.InitialTaxRate = defaults.InitialTaxRate
	}
	if cfg.MinTaxRate == 0 {
		cfg.MinTaxRate = defaults.MinTaxRate
	}
	if cfg.MaxTaxRate == 0 {
		cfg.MaxTaxRate = defaults.MaxTaxRate
	}
	if cfg.LearningRate == 0 {
		cfg.LearningRate = defaults.LearningRate
	}
	if cfg.MemorySize == 0 {
		cfg.MemorySize = defaults.MemorySize
	}
	if cfg.MinDataPoints == 0 {
		cfg.MinDataPoints = defaults.MinDataPoints
	}

	return &TaxRateOptimizer{
		ID:                     id,
		InitialFunds:           cfg.InitialFunds,
		MinFeeRate:             cfg.MinTaxRate,
		MaxFeeRate:             cfg.MaxTaxRate,
		CurrentFeeRate:         cfg.InitialTaxRate,
		LearningRate:           cfg.LearningRate,
		MemorySize:             cfg.MemorySize,
		MinDataPoints:          cfg.MinDataPoints,
		FeeRateHistory:         []float64{cfg.InitialTaxRate},
		RevenueHistory:         make([]float64, 0),
		ParticipationHistory:   make([]float64, 0),
		DeltaData:              make([]float64, 0),
		InvestmentData:         make([]float64, 0),
		HistoryTransactionData: make([][]TransactionSample, 0),
	}
}

func (o *TaxRateOptimizer) Optimize(input EpochMetrics) float64 {
	o.LastStrongestCompetitorFunds = input.StrongestCompetitorFunds
	o.LastStrongestCompetitorEarn = input.StrongestCompetitorEarn
	if input.Iteration > 1 && input.ParticipationRate < 0.1 {
		o.CurrentFeeRate = math.Max(o.MinFeeRate, o.CurrentFeeRate*0.9)
		o.FeeRateHistory = append(o.FeeRateHistory, o.CurrentFeeRate)
		o.LastPredictedInvestment = o.predictInvestment(o.CurrentFeeRate)
		return o.CurrentFeeRate
	}

	o.updateDeltaInvestmentModel(o.CurrentFeeRate, input.CurrentFunds)
	o.RevenueHistory = append(o.RevenueHistory, input.CurrentEarn+1e-5)
	o.ParticipationHistory = append(o.ParticipationHistory, input.ParticipationRate)

	predictedTransactions := o.updateB2EModel(input.Transactions)
	objective := func(delta float64) float64 {
		predictedInvestment := o.predictInvestment(delta)
		predictedEarnings := o.predictB2EEarnings(predictedTransactions, predictedInvestment)
		predictedEarnings = (predictedEarnings - o.InitialFunds) / (predictedEarnings + 1e-7) * delta
		expectedRevenue := delta * predictedEarnings * 1e-11
		if math.IsNaN(expectedRevenue) || math.IsInf(expectedRevenue, 0) {
			return math.Inf(-1)
		}
		return expectedRevenue
	}

	newDelta := maximizeBounded(objective, o.MinFeeRate, o.MaxFeeRate, 60)
	adaptiveLearningRate := o.adaptiveLearningRate()
	o.CurrentFeeRate = clamp(
		o.CurrentFeeRate+adaptiveLearningRate*(newDelta-o.CurrentFeeRate),
		o.MinFeeRate,
		o.MaxFeeRate,
	)
	o.FeeRateHistory = append(o.FeeRateHistory, o.CurrentFeeRate)
	o.LastPredictedInvestment = o.predictInvestment(o.CurrentFeeRate)
	return o.CurrentFeeRate
}

func (o *TaxRateOptimizer) FeeRate() float64 {
	return o.CurrentFeeRate
}

func (o *TaxRateOptimizer) MinFee() float64 {
	return o.MinFeeRate
}

func (o *TaxRateOptimizer) DebugState() FeeOptimizerDebug {
	return FeeOptimizerDebug{
		Mode:                     "taxrate",
		CurrentFeeRate:           o.CurrentFeeRate,
		MinFeeRate:               o.MinFeeRate,
		LastPredictedInvestment:  o.LastPredictedInvestment,
		StrongestCompetitorFunds: o.LastStrongestCompetitorFunds,
		StrongestCompetitorEarn:  o.LastStrongestCompetitorEarn,
		OptimizerPhase:           "prediction",
	}
}

func (o *TaxRateOptimizer) adaptiveLearningRate() float64 {
	volatility := 0.0
	if len(o.RevenueHistory) >= 5 {
		recent := o.RevenueHistory[len(o.RevenueHistory)-5:]
		mean := mean(recent)
		if math.Abs(mean) > 1e-9 {
			volatility = stddev(recent) / math.Abs(mean)
		}
	}
	return o.LearningRate * (1 + volatility*2)
}

func (o *TaxRateOptimizer) updateB2EModel(transactionData []TransactionSample) []predictedTransaction {
	transactionCopy := append([]TransactionSample(nil), transactionData...)
	o.HistoryTransactionData = append(o.HistoryTransactionData, transactionCopy)
	if len(o.HistoryTransactionData) > 10 {
		o.HistoryTransactionData = o.HistoryTransactionData[1:]
	}

	features := make([][]float64, 0)
	feeTargets := make([]float64, 0)
	amountTargets := make([]float64, 0)
	for _, epochTransactions := range o.HistoryTransactionData {
		for _, tx := range epochTransactions {
			features = append(features, []float64{tx.Fee, tx.Amount})
			feeTargets = append(feeTargets, tx.Fee)
			amountTargets = append(amountTargets, tx.Amount)
		}
	}
	if len(features) == 0 {
		return nil
	}

	scaledFeatures, means, stds := standardize(features)
	feeCoeffs, feeOK := fitLinearRegression(scaledFeatures, feeTargets)
	amountCoeffs, amountOK := fitLinearRegression(scaledFeatures, amountTargets)

	predicted := make([]predictedTransaction, 0, len(feeTargets))
	for _, epochTransactions := range o.HistoryTransactionData {
		for _, tx := range epochTransactions {
			scaled := []float64{
				scaleValue(tx.Fee, means[0], stds[0]),
				scaleValue(tx.Amount, means[1], stds[1]),
			}
			predictedFee := tx.Fee
			predictedAmount := tx.Amount
			if feeOK {
				predictedFee = predictLinear(feeCoeffs, scaled)
			}
			if amountOK {
				predictedAmount = predictLinear(amountCoeffs, scaled)
			}
			predicted = append(predicted, predictedTransaction{
				fee:      predictedFee,
				amount:   predictedAmount,
				sender:   tx.Sender,
				receiver: tx.Receiver,
			})
		}
	}
	return predicted
}

func (o *TaxRateOptimizer) predictB2EEarnings(predictedTransactions []predictedTransaction, predictedInvestment float64) float64 {
	if predictedInvestment <= 0 || len(predictedTransactions) == 0 {
		return 0
	}

	transactions := append([]predictedTransaction(nil), predictedTransactions...)
	sort.Slice(transactions, func(i, j int) bool {
		leftDenominator := math.Max(math.Abs(transactions[i].amount), 1e-9)
		rightDenominator := math.Max(math.Abs(transactions[j].amount), 1e-9)
		left := transactions[i].fee / leftDenominator
		right := transactions[j].fee / rightDenominator
		return left > right
	})

	totalEarnings := 0.0
	remaining := predictedInvestment
	for _, tx := range transactions {
		if tx.amount <= remaining {
			totalEarnings += tx.fee + tx.amount
			remaining -= tx.amount
		}
	}
	return totalEarnings
}

func (o *TaxRateOptimizer) updateDeltaInvestmentModel(delta float64, currentFunds float64) {
	o.DeltaData = append(o.DeltaData, delta)
	o.InvestmentData = append(o.InvestmentData, currentFunds)
	if len(o.DeltaData) > 50 {
		o.DeltaData = o.DeltaData[len(o.DeltaData)-50:]
		o.InvestmentData = o.InvestmentData[len(o.InvestmentData)-50:]
	}

	if len(o.DeltaData) < 2 {
		avgInvestment := math.Max(0, mean(o.InvestmentData))
		o.investmentPredictor = func(float64) float64 { return avgInvestment }
		return
	}

	validX := make([]float64, 0, len(o.DeltaData))
	validY := make([]float64, 0, len(o.InvestmentData))
	for idx, x := range o.DeltaData {
		y := o.InvestmentData[idx]
		if isFinite(x) && isFinite(y) {
			validX = append(validX, x)
			validY = append(validY, y)
		}
	}
	if len(validX) == 0 {
		avgInvestment := math.Max(0, mean(o.InvestmentData))
		o.investmentPredictor = func(float64) float64 { return avgInvestment }
		return
	}

	bestPredictor, bestScore := fitNonNegativeLinear(validX, validY)
	if len(validX) >= 10 {
		for _, degree := range []int{2, 3} {
			predictor, score, ok := fitPolynomialRegression(validX, validY, degree)
			if ok && score > bestScore {
				bestPredictor = predictor
				bestScore = score
			}
		}
	}
	o.investmentPredictor = func(x float64) float64 {
		return math.Max(0, bestPredictor(x))
	}
}

func (o *TaxRateOptimizer) predictInvestment(delta float64) float64 {
	if o.investmentPredictor == nil {
		if len(o.InvestmentData) == 0 {
			return 0
		}
		return math.Max(0, mean(o.InvestmentData))
	}
	return math.Max(0, o.investmentPredictor(delta))
}

func fitNonNegativeLinear(xs, ys []float64) (func(float64) float64, float64) {
	features := make([][]float64, 0, len(xs))
	for _, x := range xs {
		features = append(features, []float64{x})
	}
	coeffs, ok := fitLinearRegression(features, ys)

	type candidate struct {
		predict func(float64) float64
		sse     float64
	}

	candidates := make([]candidate, 0, 4)
	if ok && coeffs[0] >= 0 && coeffs[1] >= 0 {
		predictor := func(x float64) float64 { return coeffs[0] + coeffs[1]*x }
		candidates = append(candidates, candidate{predict: predictor, sse: computeSSE(xs, ys, predictor)})
	}

	avgY := math.Max(0, mean(ys))
	candidates = append(candidates, candidate{
		predict: func(float64) float64 { return avgY },
		sse:     computeSSE(xs, ys, func(float64) float64 { return avgY }),
	})

	sumXX := 0.0
	sumXY := 0.0
	for idx, x := range xs {
		sumXX += x * x
		sumXY += x * ys[idx]
	}
	slope := 0.0
	if sumXX > 0 {
		slope = math.Max(0, sumXY/sumXX)
	}
	candidates = append(candidates, candidate{
		predict: func(x float64) float64 { return slope * x },
		sse:     computeSSE(xs, ys, func(x float64) float64 { return slope * x }),
	})
	candidates = append(candidates, candidate{
		predict: func(float64) float64 { return 0 },
		sse:     computeSSE(xs, ys, func(float64) float64 { return 0 }),
	})

	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.sse < best.sse {
			best = candidate
		}
	}
	return best.predict, computeR2(xs, ys, best.predict)
}

func fitPolynomialRegression(xs, ys []float64, degree int) (func(float64) float64, float64, bool) {
	features := make([][]float64, 0, len(xs))
	for _, x := range xs {
		row := make([]float64, 0, degree)
		for power := 1; power <= degree; power++ {
			row = append(row, math.Pow(x, float64(power)))
		}
		features = append(features, row)
	}
	coeffs, ok := fitLinearRegression(features, ys)
	if !ok {
		return nil, 0, false
	}
	predictor := func(x float64) float64 {
		total := coeffs[0]
		for power := 1; power <= degree; power++ {
			total += coeffs[power] * math.Pow(x, float64(power))
		}
		return total
	}
	return predictor, computeR2(xs, ys, predictor), true
}

func fitLinearRegression(features [][]float64, target []float64) ([]float64, bool) {
	if len(features) == 0 || len(features) != len(target) {
		return nil, false
	}

	featureCount := len(features[0])
	matrixSize := featureCount + 1
	xtx := make([][]float64, matrixSize)
	for row := range xtx {
		xtx[row] = make([]float64, matrixSize)
	}
	xty := make([]float64, matrixSize)

	for rowIdx, row := range features {
		augmented := make([]float64, 0, matrixSize)
		augmented = append(augmented, 1.0)
		augmented = append(augmented, row...)
		for i := 0; i < matrixSize; i++ {
			xty[i] += augmented[i] * target[rowIdx]
			for j := 0; j < matrixSize; j++ {
				xtx[i][j] += augmented[i] * augmented[j]
			}
		}
	}

	coeffs, ok := solveLinearSystem(xtx, xty)
	if !ok {
		return nil, false
	}
	return coeffs, true
}

func solveLinearSystem(matrix [][]float64, vector []float64) ([]float64, bool) {
	n := len(matrix)
	augmented := make([][]float64, n)
	for i := 0; i < n; i++ {
		augmented[i] = make([]float64, n+1)
		copy(augmented[i], matrix[i])
		augmented[i][n] = vector[i]
	}

	for col := 0; col < n; col++ {
		pivot := col
		for row := col + 1; row < n; row++ {
			if math.Abs(augmented[row][col]) > math.Abs(augmented[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(augmented[pivot][col]) < 1e-12 {
			return nil, false
		}
		augmented[col], augmented[pivot] = augmented[pivot], augmented[col]

		pivotValue := augmented[col][col]
		for j := col; j <= n; j++ {
			augmented[col][j] /= pivotValue
		}

		for row := 0; row < n; row++ {
			if row == col {
				continue
			}
			factor := augmented[row][col]
			for j := col; j <= n; j++ {
				augmented[row][j] -= factor * augmented[col][j]
			}
		}
	}

	solution := make([]float64, n)
	for i := 0; i < n; i++ {
		solution[i] = augmented[i][n]
	}
	return solution, true
}

func standardize(features [][]float64) ([][]float64, []float64, []float64) {
	if len(features) == 0 {
		return nil, nil, nil
	}
	featureCount := len(features[0])
	means := make([]float64, featureCount)
	stds := make([]float64, featureCount)
	for _, row := range features {
		for idx, value := range row {
			means[idx] += value
		}
	}
	for idx := range means {
		means[idx] /= float64(len(features))
	}
	for _, row := range features {
		for idx, value := range row {
			diff := value - means[idx]
			stds[idx] += diff * diff
		}
	}
	for idx := range stds {
		stds[idx] = math.Sqrt(stds[idx] / float64(len(features)))
		if stds[idx] == 0 {
			stds[idx] = 1
		}
	}

	scaled := make([][]float64, 0, len(features))
	for _, row := range features {
		scaledRow := make([]float64, featureCount)
		for idx, value := range row {
			scaledRow[idx] = scaleValue(value, means[idx], stds[idx])
		}
		scaled = append(scaled, scaledRow)
	}
	return scaled, means, stds
}

func scaleValue(value, mean, std float64) float64 {
	if std == 0 {
		return 0
	}
	return (value - mean) / std
}

func predictLinear(coeffs []float64, features []float64) float64 {
	total := coeffs[0]
	for idx, value := range features {
		total += coeffs[idx+1] * value
	}
	return total
}

func computeSSE(xs, ys []float64, predictor func(float64) float64) float64 {
	total := 0.0
	for idx, x := range xs {
		diff := ys[idx] - predictor(x)
		total += diff * diff
	}
	return total
}

func computeR2(xs, ys []float64, predictor func(float64) float64) float64 {
	if len(ys) == 0 {
		return 0
	}
	meanY := mean(ys)
	totalSumSquares := 0.0
	residualSumSquares := 0.0
	for idx, x := range xs {
		diff := ys[idx] - predictor(x)
		residualSumSquares += diff * diff
		centered := ys[idx] - meanY
		totalSumSquares += centered * centered
	}
	if totalSumSquares == 0 {
		if residualSumSquares == 0 {
			return 1
		}
		return 0
	}
	return 1 - residualSumSquares/totalSumSquares
}

func maximizeBounded(objective func(float64) float64, lower, upper float64, iterations int) float64 {
	if lower >= upper {
		return lower
	}
	const goldenRatio = 0.6180339887498949
	left := lower
	right := upper
	c := right - goldenRatio*(right-left)
	d := left + goldenRatio*(right-left)
	fc := objective(c)
	fd := objective(d)
	for i := 0; i < iterations; i++ {
		if fc < fd {
			left = c
			c = d
			fc = fd
			d = left + goldenRatio*(right-left)
			fd = objective(d)
		} else {
			right = d
			d = c
			fd = fc
			c = right - goldenRatio*(right-left)
			fc = objective(c)
		}
	}
	return (left + right) / 2
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func stddev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	center := mean(values)
	variance := 0.0
	for _, value := range values {
		diff := value - center
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(len(values)))
}

func clamp(value, minValue, maxValue float64) float64 {
	return math.Min(math.Max(value, minValue), maxValue)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
