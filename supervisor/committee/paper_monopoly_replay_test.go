package committee

import (
	"encoding/csv"
	"io"
	"log"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"blockEmulator/core"
	"blockEmulator/message"
	"blockEmulator/mytool"
	"blockEmulator/params"
	"blockEmulator/supervisor/Broker2Earn"
	"blockEmulator/supervisor/signal"
	"blockEmulator/supervisor/supervisor_log"
)

func TestRunPaperMonopolyLocalHarnessLimit300Seed42(t *testing.T) {
	oldShardNum := params.ShardNum
	oldNodesInShard := params.NodesInShard
	oldBrokerNum := params.BrokerNum
	oldExchangeMode := params.ExchangeMode
	oldFeeOptimizerMode := params.FeeOptimizerMode
	oldSimSeed := params.SimSeed
	oldAddressSet := append([]string(nil), AddressSet...)

	t.Cleanup(func() {
		params.ShardNum = oldShardNum
		params.NodesInShard = oldNodesInShard
		params.BrokerNum = oldBrokerNum
		params.ExchangeMode = oldExchangeMode
		params.FeeOptimizerMode = oldFeeOptimizerMode
		params.SimSeed = oldSimSeed
		AddressSet = oldAddressSet
		mytool.UserRequestB2EQueue = nil
	})

	params.ShardNum = 2
	params.NodesInShard = 4
	params.BrokerNum = 20
	params.ExchangeMode = params.ExchangeModeLimit300
	params.FeeOptimizerMode = params.FeeOptimizerModePaperMonopoly
	params.SimSeed = 42
	AddressSet = nil
	mytool.UserRequestB2EQueue = nil

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(oldWd, "..", ".."))
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("failed to switch to repo root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})
	hub0Rows, hub1Rows := runPaperMonopolyReplay(t, repoRoot)
	if len(hub0Rows) == 0 || len(hub1Rows) == 0 {
		t.Fatalf("expected both replays to contain rows, got %d and %d", len(hub0Rows), len(hub1Rows))
	}
	if _, err := os.Stat("./hubres/hub0.csv"); err != nil {
		t.Fatalf("expected hub0.csv to be written, got %v", err)
	}
	if _, err := os.Stat("./hubres/hub1.csv"); err != nil {
		t.Fatalf("expected hub1.csv to be written, got %v", err)
	}

	if !hasNonZeroBrokerBeforeEpoch(hub0Rows, 40) && !hasNonZeroBrokerBeforeEpoch(hub1Rows, 40) {
		t.Fatal("expected at least one hub to attract brokers before epoch 40")
	}
	if !hasCompetitionWindow(hub0Rows, 80) && !hasCompetitionWindow(hub1Rows, 80) {
		t.Fatal("expected at least one hub to show MER down -> broker_count up -> fund up -> lagged revenue up before epoch 80")
	}
	if !hasPhaseBeforeEpoch(hub0Rows, "shock", 150) && !hasPhaseBeforeEpoch(hub1Rows, "shock", 150) {
		t.Fatal("expected at least one shock phase before epoch 150")
	}
	firstShockEpoch := earliestPhaseEpoch(hub0Rows, hub1Rows, "shock")
	if firstShockEpoch > 0 {
		hub0P90, hub0Max := competitionBrokerDeltaStatsBeforeEpoch(hub0Rows, firstShockEpoch)
		hub1P90, hub1Max := competitionBrokerDeltaStatsBeforeEpoch(hub1Rows, firstShockEpoch)
		if hub0P90 > 2 || hub1P90 > 2 || hub0Max > 6 || hub1Max > 6 {
			t.Fatalf("expected smoother competition broker jumps before the first shock, got hub0 p90/max=%d/%d hub1 p90/max=%d/%d", hub0P90, hub0Max, hub1P90, hub1Max)
		}
	}
	if math.Max(maxCriticalCapBeforeEpoch(hub0Rows, 150), maxCriticalCapBeforeEpoch(hub1Rows, 150)) <= 0.06 {
		t.Fatal("expected a critical MER cap above 0.06 before epoch 150")
	}

	if !(hasNonZeroBrokerBeforeEpoch(hub0Rows, 150) && hasNonZeroBrokerBeforeEpoch(hub1Rows, 150)) {
		if countLeaderSwitches(hub0Rows, hub1Rows, 150) < 2 {
			t.Fatal("expected both hubs to win brokers at least once before epoch 150, or fund-share leadership to switch at least twice")
		}
	}

	winnerRows := hub0Rows
	loserRows := hub1Rows
	if tailFundShareMean(hub1Rows, 100) > tailFundShareMean(hub0Rows, 100) {
		winnerRows = hub1Rows
		loserRows = hub0Rows
	}
	if tailPhaseCount(winnerRows, "memory", 12) < 8 {
		t.Fatal("expected the winner to end in a memory-dominant regime in the final 12 epochs")
	}
	if tailMERRange(winnerRows, 100) <= 0.015 {
		t.Fatal("expected winner MER to keep a visibly wider tail range after retuning")
	}
	if finalCriticalCap(winnerRows) > 0 && finalTailMaxMER(winnerRows, 12) >= finalCriticalCap(winnerRows) {
		t.Fatal("expected winner MER to stay below the learned critical MER cap once memory takes over")
	}
	if tailPhaseCount(loserRows, "memory", 100) == 100 && tailMERRange(loserRows, 100) <= 1e-4 {
		t.Fatal("expected loser trajectory to remain responsive rather than collapsing into a flat memory line")
	}

	hub0RowsRepeat, hub1RowsRepeat := runPaperMonopolyReplay(t, repoRoot)
	if !sameBrokerTrajectoryPrefix(hub0Rows, hub0RowsRepeat, 80) || !sameBrokerTrajectoryPrefix(hub1Rows, hub1RowsRepeat, 80) {
		t.Fatal("expected repeated seed=42 replay runs to keep the same broker-count trajectory for the first 80 epochs")
	}
}

func runLocalBrokerhubEpoch(
	bcm *BrokerhubCommitteeMod,
	pending []*message.BrokerRawMeg,
	txs []*core.Transaction,
) []*message.BrokerRawMeg {
	brokerRawMegs := append([]*message.BrokerRawMeg{}, pending...)
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		tx.Recipient = FormatStringToLength(tx.Recipient, 40)
		if tx.Recipient == "error" {
			continue
		}
		tx.Sender = FormatStringToLength(tx.Sender, 40)
		if tx.Sender == "error" || tx.Recipient == tx.Sender {
			continue
		}
		if bcm.fetchModifiedMap(tx.Recipient) != bcm.fetchModifiedMap(tx.Sender) {
			brokerRawMegs = append(brokerRawMegs, &message.BrokerRawMeg{
				Tx:     tx,
				Broker: bcm.Broker.BrokerAddress[0],
			})
		}
	}

	if len(brokerRawMegs) == 0 {
		return nil
	}

	bcm.appendEpochCrossTxSamples(brokerRawMegs)
	if len(brokerRawMegs) > 1000 {
		brokerRawMegs = brokerRawMegs[:1000]
	}

	allocatedBrokerRawMegs, restBrokerRawMeg := Broker2Earn.B2E(
		brokerRawMegs,
		bcm.handleBrokerB2EBalance(),
	)
	if len(allocatedBrokerRawMegs) > 0 {
		bcm.markNonEmptyBlockInfo()
	}
	for _, brokerRawMeg := range allocatedBrokerRawMegs {
		if brokerRawMeg == nil || brokerRawMeg.Tx == nil {
			continue
		}
		fee := new(big.Float).SetInt(brokerRawMeg.Tx.Fee)
		fee.Mul(fee, bcm.Broker.Brokerage)
		bcm.allocateBrokerhubRevenue(
			brokerRawMeg.Broker,
			bcm.fetchModifiedMap(brokerRawMeg.Tx.Sender),
			fee,
		)
	}
	return restBrokerRawMeg
}

func runPaperMonopolyReplay(t *testing.T, repoRoot string) ([]replayRow, []replayRow) {
	t.Helper()

	AddressSet = nil
	mytool.UserRequestB2EQueue = nil
	stopSignal := signal.NewStopSignal(2 * params.ShardNum)
	bcm := NewBrokerhubCommitteeMod(
		map[uint64]map[uint64]string{},
		stopSignal,
		&supervisor_log.SupervisorLog{
			Slog: log.New(io.Discard, "", 0),
		},
		params.FileInput,
		params.TotalDataSize,
		params.BatchSize,
		params.ExchangeModeLimit300,
		params.FeeOptimizerModePaperMonopoly,
		42,
	)

	if err := os.MkdirAll("hubres", os.ModePerm); err != nil {
		t.Fatalf("failed to create hubres directory: %v", err)
	}
	_ = os.Remove("./hubres/hub0.csv")
	_ = os.Remove("./hubres/hub1.csv")

	bcm.init_brokerhub()
	bcm.writeDataToCsv(true, 0)

	var pending []*message.BrokerRawMeg
	for !bcm.reachedEpochLimit() {
		bcm.hubParams.currentEpoch++
		txs := bcm.generateRandomTxs()
		pending = runLocalBrokerhubEpoch(bcm, pending, txs)
		bcm.broker_behaviour_simulator(true)
	}

	hub0Rows, err := loadReplayRows(filepath.Join(repoRoot, "hubres", "hub0.csv"))
	if err != nil {
		t.Fatalf("failed to parse hub0 replay: %v", err)
	}
	hub1Rows, err := loadReplayRows(filepath.Join(repoRoot, "hubres", "hub1.csv"))
	if err != nil {
		t.Fatalf("failed to parse hub1 replay: %v", err)
	}
	return hub0Rows, hub1Rows
}

type replayRow struct {
	Epoch          int
	Revenue        float64
	BrokerNum      int
	MER            float64
	Fund           float64
	FundShare      float64
	CriticalMERCap float64
	OptimizerPhase string
}

func loadReplayRows(path string) ([]replayRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	rawRows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rawRows) <= 1 {
		return nil, nil
	}

	headerIndex := make(map[string]int)
	for idx, name := range rawRows[0] {
		headerIndex[name] = idx
	}

	rows := make([]replayRow, 0, len(rawRows)-1)
	for _, raw := range rawRows[1:] {
		rows = append(rows, replayRow{
			Epoch:          mustParseInt(raw[headerIndex["epoch"]]),
			Revenue:        mustParseFloat(raw[headerIndex["revenue"]]),
			BrokerNum:      mustParseInt(raw[headerIndex["broker_num"]]),
			MER:            mustParseFloat(raw[headerIndex["mer"]]),
			Fund:           mustParseFloat(raw[headerIndex["fund"]]),
			FundShare:      mustParseFloat(raw[headerIndex["fund_share"]]),
			CriticalMERCap: mustParseFloat(raw[headerIndex["critical_mer_cap"]]),
			OptimizerPhase: raw[headerIndex["optimizer_phase"]],
		})
	}
	return rows, nil
}

func mustParseInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func mustParseFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func hasNonZeroBrokerBeforeEpoch(rows []replayRow, maxEpoch int) bool {
	for _, row := range rows {
		if row.Epoch > maxEpoch {
			break
		}
		if row.BrokerNum > 0 {
			return true
		}
	}
	return false
}

func hasCompetitionWindow(rows []replayRow, maxEpoch int) bool {
	for idx := 1; idx < len(rows); idx++ {
		prev := rows[idx-1]
		feeCutRow := rows[idx]
		if feeCutRow.Epoch > maxEpoch {
			break
		}
		if feeCutRow.MER >= prev.MER-1e-9 {
			continue
		}
		competitionWindowEnd := replayMinInt(idx+3, len(rows)-1)
		for fundIdx := idx + 1; fundIdx <= competitionWindowEnd; fundIdx++ {
			fundRow := rows[fundIdx]
			if fundRow.BrokerNum <= feeCutRow.BrokerNum || fundRow.Fund <= feeCutRow.Fund+1e-9 {
				continue
			}
			revenueWindowEnd := replayMinInt(fundIdx+3, len(rows)-1)
			for revenueIdx := fundIdx; revenueIdx <= revenueWindowEnd; revenueIdx++ {
				if rows[revenueIdx].Revenue > feeCutRow.Revenue+1e-9 {
					return true
				}
			}
		}
	}
	return false
}

func earliestPhaseEpoch(hub0Rows, hub1Rows []replayRow, phase string) int {
	bestEpoch := 0
	for _, row := range append(append([]replayRow(nil), hub0Rows...), hub1Rows...) {
		if row.OptimizerPhase != phase {
			continue
		}
		if bestEpoch == 0 || row.Epoch < bestEpoch {
			bestEpoch = row.Epoch
		}
	}
	return bestEpoch
}

func hasCompetitionPhaseFlushBeforeEpoch(rows []replayRow, maxEpoch, totalBrokerCount int) bool {
	if totalBrokerCount <= 0 {
		return false
	}
	flushThreshold := int(math.Ceil(0.7 * float64(totalBrokerCount)))
	for idx := 1; idx < len(rows); idx++ {
		prev := rows[idx-1]
		current := rows[idx]
		if prev.Epoch >= maxEpoch || current.Epoch >= maxEpoch {
			break
		}
		if prev.OptimizerPhase != competitionPhaseCompetition || current.OptimizerPhase != competitionPhaseCompetition {
			continue
		}
		if prev.BrokerNum >= flushThreshold && current.BrokerNum == 0 {
			return true
		}
	}
	return false
}

func competitionBrokerDeltaStatsBeforeEpoch(rows []replayRow, maxEpoch int) (int, int) {
	deltas := make([]int, 0, len(rows))
	maxDelta := 0
	for idx := 1; idx < len(rows); idx++ {
		prev := rows[idx-1]
		current := rows[idx]
		if prev.Epoch >= maxEpoch || current.Epoch >= maxEpoch {
			break
		}
		if prev.OptimizerPhase != competitionPhaseCompetition || current.OptimizerPhase != competitionPhaseCompetition {
			continue
		}
		delta := absReplayInt(current.BrokerNum - prev.BrokerNum)
		deltas = append(deltas, delta)
		if delta > maxDelta {
			maxDelta = delta
		}
	}
	if len(deltas) == 0 {
		return 0, 0
	}
	sorted := append([]int(nil), deltas...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	p90Index := int(math.Ceil(0.9*float64(len(sorted)))) - 1
	if p90Index < 0 {
		p90Index = 0
	}
	if p90Index >= len(sorted) {
		p90Index = len(sorted) - 1
	}
	return sorted[p90Index], maxDelta
}

func sameBrokerTrajectoryPrefix(left, right []replayRow, maxEpoch int) bool {
	leftByEpoch := make(map[int]int, len(left))
	for _, row := range left {
		if row.Epoch > maxEpoch {
			break
		}
		leftByEpoch[row.Epoch] = row.BrokerNum
	}
	for _, row := range right {
		if row.Epoch > maxEpoch {
			break
		}
		if leftByEpoch[row.Epoch] != row.BrokerNum {
			return false
		}
	}
	return len(leftByEpoch) > 0
}

func hasPhaseBeforeEpoch(rows []replayRow, phase string, maxEpoch int) bool {
	for _, row := range rows {
		if row.Epoch > maxEpoch {
			break
		}
		if row.OptimizerPhase == phase {
			return true
		}
	}
	return false
}

func maxCriticalCapBeforeEpoch(rows []replayRow, maxEpoch int) float64 {
	best := 0.0
	for _, row := range rows {
		if row.Epoch > maxEpoch {
			break
		}
		if row.CriticalMERCap > best {
			best = row.CriticalMERCap
		}
	}
	return best
}

func countLeaderSwitches(hub0Rows, hub1Rows []replayRow, maxEpoch int) int {
	hub0ByEpoch := make(map[int]float64, len(hub0Rows))
	for _, row := range hub0Rows {
		if row.Epoch <= maxEpoch {
			hub0ByEpoch[row.Epoch] = row.FundShare
		}
	}
	lastLeader := 0
	switches := 0
	for _, row := range hub1Rows {
		if row.Epoch > maxEpoch {
			break
		}
		hub0Share, ok := hub0ByEpoch[row.Epoch]
		if !ok {
			continue
		}
		leader := 0
		switch {
		case hub0Share > row.FundShare+1e-9:
			leader = 1
		case row.FundShare > hub0Share+1e-9:
			leader = -1
		}
		if leader == 0 {
			continue
		}
		if lastLeader != 0 && leader != lastLeader {
			switches++
		}
		lastLeader = leader
	}
	return switches
}

func tailFundShareMean(rows []replayRow, window int) float64 {
	if len(rows) == 0 {
		return 0
	}
	if len(rows) < window {
		window = len(rows)
	}
	total := 0.0
	for _, row := range rows[len(rows)-window:] {
		total += row.FundShare
	}
	return total / float64(window)
}

func tailPhaseCount(rows []replayRow, phase string, window int) int {
	if len(rows) == 0 {
		return 0
	}
	if len(rows) < window {
		window = len(rows)
	}
	count := 0
	for _, row := range rows[len(rows)-window:] {
		if row.OptimizerPhase == phase {
			count++
		}
	}
	return count
}

func tailMERRange(rows []replayRow, window int) float64 {
	if len(rows) == 0 {
		return 0
	}
	if len(rows) < window {
		window = len(rows)
	}
	minMER := rows[len(rows)-window].MER
	maxMER := minMER
	for _, row := range rows[len(rows)-window:] {
		minMER = math.Min(minMER, row.MER)
		maxMER = math.Max(maxMER, row.MER)
	}
	return maxMER - minMER
}

func finalCriticalCap(rows []replayRow) float64 {
	if len(rows) == 0 {
		return 0
	}
	return rows[len(rows)-1].CriticalMERCap
}

func finalTailMaxMER(rows []replayRow, window int) float64 {
	if len(rows) == 0 {
		return 0
	}
	if len(rows) < window {
		window = len(rows)
	}
	best := 0.0
	for _, row := range rows[len(rows)-window:] {
		best = math.Max(best, row.MER)
	}
	return best
}

func replayMinInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func absReplayInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
