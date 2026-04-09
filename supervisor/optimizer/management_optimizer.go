package optimizer

import (
	"math"
)

// ManagementFeeOptimizer manages and optimizes the tax rate based on heuristics and historical trends.
type ManagementFeeOptimizer struct {
	ID             string
	InitialFunds   float64
	MinFeeRate     float64
	MaxFeeRate     float64
	BaseFeeRate    float64
	CurrentFeeRate float64

	// 历史数据窗口
	FeeRateHistory       []float64
	RevenueHistory       []float64
	NetRevenueHistory    []float64
	FundsHistory         []float64
	InvestedFundsHistory []float64

	WindowSize      int
	AdjustThreshold float64

	// 状态与阈值管理
	HistoricalMaxSuccessful float64
	SuccessThreshold        int
	FailureThreshold        int
	ConsecutiveSuccess      int
	ConsecutiveFailure      int
	Status                  int
	PreviousInvestedFunds   float64
}

// NewManagementFeeOptimizer 初始化一个新的费率优化器实例
func NewManagementFeeOptimizer(id string, initialFunds, initialTaxRate float64) *ManagementFeeOptimizer {
	return &ManagementFeeOptimizer{
		ID:                      id,
		InitialFunds:            initialFunds,
		MinFeeRate:              0.05,
		MaxFeeRate:              0.99,
		BaseFeeRate:             0.0,
		CurrentFeeRate:          initialTaxRate,
		FeeRateHistory:          []float64{initialTaxRate},
		WindowSize:              5,
		AdjustThreshold:         0.1,
		HistoricalMaxSuccessful: 0.99, // default to max_fee_rate
		SuccessThreshold:        5,
		FailureThreshold:        2,
		ConsecutiveSuccess:      0,
		ConsecutiveFailure:      0,
		Status:                  0,
		PreviousInvestedFunds:   0.0,
	}
}

// CalculateSlope 计算给定历史数据切片的一元线性回归斜率
// 它是应对 np.polyfit(x, y, 1)[0] 的纯 Go 替代实现，其中 x 为 [0, 1, 2, ..., n-1]
func CalculateSlope(history []float64) float64 {
	n := len(history)
	if n < 2 {
		return 0
	}

	var sumX, sumY, sumXY, sumX2 float64
	for i, y := range history {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	nFloat := float64(n)
	denominator := nFloat*sumX2 - sumX*sumX
	if denominator == 0 { // 防止除以零
		return 0
	}

	return (nFloat*sumXY - sumX*sumY) / denominator
}

// CalculateAdjustmentCoefficient 根据与竞争对手的有效资金差值倍数计算调整系数
func (m *ManagementFeeOptimizer) CalculateAdjustmentCoefficient(totalFunds, competitorFunds float64) float64 {
	investedFunds := totalFunds - m.InitialFunds
	competitorInvestedFunds := competitorFunds - m.InitialFunds

	if investedFunds <= 0 {
		return 0.2 // 如果没有投资者资金，使用最大调整系数
	}

	fundsRatio := competitorInvestedFunds / investedFunds

	if fundsRatio > 2 {
		return 0.2
	} else if fundsRatio > 1.5 {
		return 0.15
	} else if fundsRatio > 1.2 {
		return 0.1
	}
	return 0.05
}

// UpdateRateBounds 根据投资者资金变化更新费率上限
func (m *ManagementFeeOptimizer) UpdateRateBounds() {
	if len(m.NetRevenueHistory) <= 5 || len(m.InvestedFundsHistory) <= 5 || len(m.FeeRateHistory) <= 5 {
		return
	}

	recentRevenues := m.NetRevenueHistory[len(m.NetRevenueHistory)-5:]
	revenueSlope := CalculateSlope(recentRevenues)
	meanRev := Mean(recentRevenues)
	revenueChange := revenueSlope / math.Max(Abs(meanRev), 1)

	recentFunds := m.InvestedFundsHistory[len(m.InvestedFundsHistory)-5:]
	fundsSlope := CalculateSlope(recentFunds)
	meanFunds := Mean(recentFunds)
	fundsChange := fundsSlope / math.Max(Abs(meanFunds), 1)

	// 更新成功/失败计数器
	if revenueChange >= 1e-5 || fundsChange >= 1e-5 {
		m.ConsecutiveSuccess++
		m.ConsecutiveFailure = 0
	} else if revenueChange <= -1e-5 {
		m.ConsecutiveFailure++
		m.ConsecutiveSuccess = 0
	}

	// 当达到阈值时更新历史安全红线
	if m.ConsecutiveSuccess >= m.SuccessThreshold {
		m.HistoricalMaxSuccessful = math.Min(m.MaxFeeRate, math.Max(m.HistoricalMaxSuccessful, m.CurrentFeeRate*1.05))
		m.ConsecutiveSuccess = 0
	}

	if m.ConsecutiveFailure >= m.FailureThreshold {
		m.HistoricalMaxSuccessful = math.Max(m.MinFeeRate, m.HistoricalMaxSuccessful*0.95)
		m.ConsecutiveFailure = 0
	}
}

// Optimize 计算当前周期的最佳管理费率
func (m *ManagementFeeOptimizer) Optimize(iteration int, currentFunds, competitorFunds, currentEarn, competitorEarn float64) float64 {
	investedFunds := currentFunds - m.InitialFunds
	if investedFunds < 0 {
		investedFunds = 0
	}

	// 紧急刹车：大幅资金流失
	if m.PreviousInvestedFunds > 0 && (investedFunds-m.PreviousInvestedFunds)/m.PreviousInvestedFunds < -0.1 {
		if len(m.FeeRateHistory) >= 2 {
			previousRate := m.FeeRateHistory[len(m.FeeRateHistory)-2]
			m.HistoricalMaxSuccessful = math.Min(m.HistoricalMaxSuccessful, previousRate*0.8)
		}
	}
	m.PreviousInvestedFunds = investedFunds

	// 净收益计算 (假定 currentEarn 是收入)
	netRevenue := currentEarn
	m.NetRevenueHistory = append(m.NetRevenueHistory, netRevenue)
	m.FundsHistory = append(m.FundsHistory, currentFunds)
	m.InvestedFundsHistory = append(m.InvestedFundsHistory, investedFunds)

	if iteration > 2 {
		m.UpdateRateBounds()
	}

	// 投资者流失保护
	if investedFunds <= 0 && iteration > 2 {
		proposedFee := m.CurrentFeeRate - m.CurrentFeeRate/2.0
		m.CurrentFeeRate = math.Max(m.MinFeeRate, proposedFee)
		m.FeeRateHistory = append(m.FeeRateHistory, m.CurrentFeeRate)
		return m.CurrentFeeRate
	}

	if iteration <= 2 {
		m.FeeRateHistory = append(m.FeeRateHistory, m.CurrentFeeRate)
		return m.CurrentFeeRate
	}

	// 计算 Bounds 收敛因子
	var bounds float64
	if iteration > 100 {
		bounds = (0.1 * 100) / float64(iteration)
	} else {
		decayRate := (0.9 - 0.1) / 100.0
		bounds = 0.9 - float64(iteration)*decayRate
	}

	// 竞争对手调整
	competitorInvested := competitorFunds - m.InitialFunds
	fundsDiffRatio := (competitorInvested - investedFunds) / math.Max(investedFunds, 1e-10) // 防零除

	var compAdjustment float64
	if fundsDiffRatio > 0 { // 落后于对手
		coef := m.CalculateAdjustmentCoefficient(currentFunds, competitorFunds)

		// 估算收益率
		myReturnRate := netRevenue / math.Max(investedFunds, 1e-10)
		compReturnRate := competitorEarn / math.Max(competitorInvested, 1e-10)

		if myReturnRate < compReturnRate {
			compAdjustment = -coef * fundsDiffRatio
		} else {
			compAdjustment = -coef * fundsDiffRatio * 0.5
		}
	} else { // 领先于对手
		coef := m.CalculateAdjustmentCoefficient(currentFunds, competitorFunds)
		compAdjustment = coef * math.Abs(fundsDiffRatio) * 0.3
	}

	// 边界计算
	lowerBound := m.MinFeeRate
	upperBound := math.Min(m.MaxFeeRate, m.HistoricalMaxSuccessful*(1.0+bounds))

	// 限制调整后的费率
	proposedRate := m.CurrentFeeRate + compAdjustment
	m.CurrentFeeRate = math.Min(math.Max(proposedRate, lowerBound), upperBound)

	m.FeeRateHistory = append(m.FeeRateHistory, m.CurrentFeeRate)
	return m.CurrentFeeRate
}

// Abs 用于求绝对值
func Abs(x float64) float64 {
	return math.Abs(x)
}

// Mean 计算 float64 切片的平均值
func Mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	var sum float64
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}
