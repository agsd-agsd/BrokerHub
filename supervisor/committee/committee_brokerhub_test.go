package committee

import (
	"blockEmulator/broker"
	"blockEmulator/core"
	"blockEmulator/message"
	"blockEmulator/params"
	optimizerPkg "blockEmulator/supervisor/optimizer"
	"blockEmulator/supervisor/supervisor_log"
	"blockEmulator/utils"
	"encoding/csv"
	"io"
	"log"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllocateBrokerhubRevenueAccumulatesB2EProfit(t *testing.T) {
	brokerID := "broker-test"
	bcm := newTestBrokerhubCommittee(brokerID)
	fee := big.NewFloat(12.5)

	bcm.allocateBrokerhubRevenue(brokerID, 0, fee)

	got, _ := bcm.brokerEpochProfitInB2E[brokerID].Float64()
	if got != 12.5 {
		t.Fatalf("expected B2E epoch profit to be 12.5, got %f", got)
	}
}

func TestAllocateBrokerhubRevenueAccumulatesHubProfit(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	hubID := bcm.BrokerHubAccountList[0]
	fee := big.NewFloat(9.75)

	bcm.allocateBrokerhubRevenue(hubID, 0, fee)

	got, _ := bcm.brokerhubEpochProfit[hubID].Float64()
	if got != 9.75 {
		t.Fatalf("expected hub epoch profit to be 9.75, got %f", got)
	}
}

func TestAllocateBrokerhubRevenueSplitsToHubMembers(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	hubID := bcm.BrokerHubAccountList[0]
	brokerOne := strings.Repeat("c", 40)
	brokerTwo := strings.Repeat("d", 40)
	bcm.feeOptimizers[hubID].(*optimizerPkg.TaxRateOptimizer).CurrentFeeRate = 0.25
	bcm.brokerInfoListInBrokerHub[hubID] = []*message.BrokerInfoInBrokerhub{
		{BrokerAddr: brokerOne, BrokerBalance: big.NewInt(100), BrokerProfit: big.NewFloat(0)},
		{BrokerAddr: brokerTwo, BrokerBalance: big.NewInt(100), BrokerProfit: big.NewFloat(0)},
	}
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		bcm.Broker.BrokerBalance[hubID][shard] = big.NewInt(0)
	}
	bcm.Broker.BrokerBalance[hubID][0] = big.NewInt(100)
	bcm.Broker.BrokerBalance[hubID][1] = big.NewInt(100)

	bcm.allocateBrokerhubRevenue(hubID, 0, big.NewFloat(100))

	gotOne, _ := bcm.brokerInfoListInBrokerHub[hubID][0].BrokerProfit.Float64()
	gotTwo, _ := bcm.brokerInfoListInBrokerHub[hubID][1].BrokerProfit.Float64()
	gotHub, _ := bcm.brokerhubEpochProfit[hubID].Float64()
	if math.Abs(gotOne-37.5) > 1e-9 || math.Abs(gotTwo-37.5) > 1e-9 {
		t.Fatalf("expected each broker to receive 37.5, got %f and %f", gotOne, gotTwo)
	}
	if math.Abs(gotHub-25.0) > 1e-9 {
		t.Fatalf("expected hub to retain net revenue 25.0, got %f", gotHub)
	}
}

func TestAllocateBrokerhubRevenueKeepsRawGrossRevenue(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	hubID := bcm.BrokerHubAccountList[0]

	bcm.allocateBrokerhubRevenue(hubID, 0, big.NewFloat(40))

	got, _ := bcm.brokerhubEpochGrossRevenue[hubID].Float64()
	if math.Abs(got-40) > 1e-9 {
		t.Fatalf("expected gross revenue to remain unscaled at 40, got %f", got)
	}
}

func TestDealTxByBrokerCollectsCrossTxSamples(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	for _, addr := range bcm.Broker.BrokerAddress {
		for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
			bcm.Broker.BrokerBalance[addr][shard] = big.NewInt(0)
		}
	}

	crossShardTx := core.NewTransaction(
		strings.Repeat("0", 32)+"00000000",
		strings.Repeat("0", 32)+"00000001",
		big.NewInt(25),
		1,
		big.NewInt(7),
	)
	secondCrossShardTx := core.NewTransaction(
		strings.Repeat("0", 32)+"00000002",
		strings.Repeat("0", 32)+"00000001",
		big.NewInt(30),
		2,
		big.NewInt(9),
	)

	bcm.dealTxByBroker([]*core.Transaction{crossShardTx})
	bcm.dealTxByBroker2([]*core.Transaction{secondCrossShardTx})

	if got := len(bcm.epochCrossTxSamples); got != 2 {
		t.Fatalf("expected 2 sampled cross-shard txs, got %d", got)
	}
	if bcm.epochCrossTxSamples[0].Fee != 7 || bcm.epochCrossTxSamples[0].Amount != 25 {
		t.Fatalf("unexpected first sample: %+v", bcm.epochCrossTxSamples[0])
	}
	if bcm.epochCrossTxSamples[1].Fee != 9 || bcm.epochCrossTxSamples[1].Amount != 30 {
		t.Fatalf("unexpected second sample: %+v", bcm.epochCrossTxSamples[1])
	}
}

func TestBrokerBehaviourSimulatorConsumesSamplesAndWritesDebugColumns(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	hubID := bcm.BrokerHubAccountList[0]
	bcm.Broker.BrokerAddress = []utils.Address{hubID}
	bcm.brokerInfoListInBrokerHub[hubID] = []*message.BrokerInfoInBrokerhub{
		{BrokerAddr: strings.Repeat("c", 40), BrokerBalance: big.NewInt(2000), BrokerProfit: big.NewFloat(0)},
		{BrokerAddr: strings.Repeat("d", 40), BrokerBalance: big.NewInt(2000), BrokerProfit: big.NewFloat(0)},
	}
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		bcm.Broker.BrokerBalance[hubID][shard] = big.NewInt(6000)
	}
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(250)
	bcm.hubParams.currentEpoch = 3
	bcm.appendEpochCrossTxSamples([]*message.BrokerRawMeg{
		{
			Tx: core.NewTransaction(
				strings.Repeat("0", 32)+"00000000",
				strings.Repeat("0", 32)+"00000001",
				big.NewInt(50),
				1,
				big.NewInt(11),
			),
			Broker: hubID,
		},
	})

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to switch working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	if err := os.MkdirAll("hubres", os.ModePerm); err != nil {
		t.Fatalf("failed to create hubres directory: %v", err)
	}

	before := bcm.feeOptimizers[hubID].FeeRate()
	bcm.writeDataToCsv(true, 0)
	bcm.broker_behaviour_simulator(true)

	if got := len(bcm.epochCrossTxSamples); got != 0 {
		t.Fatalf("expected sampled tx buffer to be cleared, got %d items", got)
	}
	after := bcm.feeOptimizers[hubID].FeeRate()
	if after == before {
		t.Fatalf("expected optimizer to update fee rate, still %f", after)
	}

	csvPath := filepath.Join(tempDir, "hubres", "hub0.csv")
	file, err := os.Open(csvPath)
	if err != nil {
		t.Fatalf("failed to open csv output: %v", err)
	}
	defer file.Close()

	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("failed to read csv output: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header and one data row, got %d rows", len(rows))
	}
	header := rows[0]
	if got := len(header); got != 16 {
		t.Fatalf("expected 16 csv columns, got %d", got)
	}
	expectedTail := []string{
		"participation_rate",
		"current_investment",
		"sampled_cross_txs",
		"predicted_investment",
		"fund_share",
		"dominance_streak",
		"critical_mer_cap",
		"shock_exit_count",
		"shock_fund_drop",
		"optimizer_phase",
	}
	for idx, expected := range expectedTail {
		if header[len(header)-len(expectedTail)+idx] != expected {
			t.Fatalf("expected csv header column %q, got %q", expected, header[len(header)-len(expectedTail)+idx])
		}
	}
}

func TestReachedEpochLimitOnlyStopsFiniteMode(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	bcm.hubParams.exchangeMode = params.ExchangeModeInfinite
	bcm.hubParams.endedEpoch = params.ExchangeModeEpochLimit(params.ExchangeModeInfinite)
	bcm.hubParams.currentEpoch = params.ExchangeModeLimit300Epoch
	if bcm.reachedEpochLimit() {
		t.Fatal("infinite mode should not stop at the 300th epoch")
	}

	bcm.hubParams.exchangeMode = params.ExchangeModeLimit100
	bcm.hubParams.endedEpoch = params.ExchangeModeEpochLimit(params.ExchangeModeLimit100)
	bcm.hubParams.currentEpoch = params.ExchangeModeLimit100Epoch
	if !bcm.reachedEpochLimit() {
		t.Fatal("limit100 mode should stop once the epoch limit is reached")
	}

	bcm.hubParams.exchangeMode = params.ExchangeModeLimit300
	bcm.hubParams.endedEpoch = params.ExchangeModeEpochLimit(params.ExchangeModeLimit300)
	bcm.hubParams.currentEpoch = params.ExchangeModeLimit300Epoch
	if !bcm.reachedEpochLimit() {
		t.Fatal("limit300 mode should stop once the epoch limit is reached")
	}
}

func TestBrokerBehaviourSimulatorKeepsBrokerInB2EOnRevenueTie(t *testing.T) {
	brokerID := strings.Repeat("b", 40)
	bcm := newTestBrokerhubCommittee(brokerID)
	hubID := bcm.BrokerHubAccountList[0]
	bcm.hubParams.currentEpoch = 3
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(0)
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(0)
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		bcm.Broker.BrokerBalance[brokerID][shard] = big.NewInt(100)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to switch working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	if err := os.MkdirAll("hubres", os.ModePerm); err != nil {
		t.Fatalf("failed to create hubres directory: %v", err)
	}

	bcm.writeDataToCsv(true, 0)
	bcm.broker_behaviour_simulator(true)

	if joinedHub, ok := bcm.brokerJoinBrokerHubState[brokerID]; ok {
		t.Fatalf("expected broker to remain in B2E on revenue tie, but joined hub %s", joinedHub)
	}
}

func TestBrokerBehaviourSimulatorKeepsBrokerInB2EOnPositiveHubTie(t *testing.T) {
	brokerID := strings.Repeat("b", 40)
	hubOneID := strings.Repeat("a", 40)
	hubTwoID := strings.Repeat("c", 40)
	bcm := newTestBrokerhubCommittee(brokerID)

	bcm.BrokerHubAccountList = []utils.Address{hubOneID, hubTwoID}
	bcm.Broker.BrokerAddress = []utils.Address{brokerID, hubOneID, hubTwoID}
	bcm.Broker.BrokerBalance[brokerID] = shardBalances(100)
	bcm.Broker.BrokerBalance[hubOneID] = shardBalances(200)
	bcm.Broker.BrokerBalance[hubTwoID] = shardBalances(200)
	bcm.Broker.LockBalance[hubTwoID] = shardBalances(0)
	bcm.Broker.ProfitBalance[hubTwoID] = shardProfitBalances()
	bcm.brokerInfoListInBrokerHub[hubOneID] = []*message.BrokerInfoInBrokerhub{}
	bcm.brokerInfoListInBrokerHub[hubTwoID] = []*message.BrokerInfoInBrokerhub{}
	bcm.brokerhubEpochProfit[hubOneID] = big.NewFloat(40)
	bcm.brokerhubEpochProfit[hubTwoID] = big.NewFloat(40)
	bcm.brokerhubEpochGrossRevenue[hubOneID] = big.NewFloat(40)
	bcm.brokerhubEpochGrossRevenue[hubTwoID] = big.NewFloat(40)
	bcm.feeOptimizers = map[string]optimizerPkg.FeeOptimizer{
		hubOneID: &staticFeeOptimizer{fee: 0.05},
		hubTwoID: &staticFeeOptimizer{fee: 0.05},
	}
	bcm.hubParams.currentEpoch = 3
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(2)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to switch working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	if err := os.MkdirAll("hubres", os.ModePerm); err != nil {
		t.Fatalf("failed to create hubres directory: %v", err)
	}

	bcm.writeDataToCsv(true, 0)
	bcm.broker_behaviour_simulator(true)

	if joinedHub, ok := bcm.brokerJoinBrokerHubState[brokerID]; ok {
		t.Fatalf("expected broker to remain in B2E on positive tie, but joined hub %s", joinedHub)
	}
}

func TestRefreshDirectUtilityEstimatesUsesTrailingMeanAndInterpolation(t *testing.T) {
	brokerA := strings.Repeat("b", 40)
	brokerB := strings.Repeat("c", 40)
	brokerC := strings.Repeat("d", 40)
	bcm := newTestBrokerhubCommittee(brokerA)
	hubID := bcm.BrokerHubAccountList[0]

	bcm.Broker.BrokerAddress = []utils.Address{brokerA, brokerB, brokerC, hubID}
	bcm.Broker.BrokerBalance[brokerA] = shardBalances(100)
	bcm.Broker.BrokerBalance[brokerB] = shardBalances(200)
	bcm.Broker.BrokerBalance[brokerC] = shardBalances(150)
	bcm.Broker.LockBalance[brokerA] = shardBalances(0)
	bcm.Broker.LockBalance[brokerB] = shardBalances(0)
	bcm.Broker.LockBalance[brokerC] = shardBalances(0)
	bcm.Broker.ProfitBalance[brokerA] = shardProfitBalances()
	bcm.Broker.ProfitBalance[brokerB] = shardProfitBalances()
	bcm.Broker.ProfitBalance[brokerC] = shardProfitBalances()
	bcm.brokerJoinBrokerHubState[brokerC] = hubID
	bcm.brokerB2EProfitHistory[brokerA] = []float64{6, 9, 12}
	bcm.brokerB2EProfitHistory[brokerB] = []float64{8, 10, 12}

	bcm.refreshDirectUtilityEstimates()

	if math.Abs(bcm.brokerDirectUtilityEst[brokerA]-9) > 1e-9 {
		t.Fatalf("expected trailing mean utility 9 for brokerA, got %f", bcm.brokerDirectUtilityEst[brokerA])
	}
	if math.Abs(bcm.brokerDirectUtilityEst[brokerC]-9.5) > 1e-9 {
		t.Fatalf("expected interpolated utility 9.5 for brokerC, got %f", bcm.brokerDirectUtilityEst[brokerC])
	}
}

func TestWriteDataToCsvTruncatesExistingFileAndUsesCurrentFunds(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	hubID := bcm.BrokerHubAccountList[0]
	bcm.hubParams.currentEpoch = 7
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(12.5)
	bcm.feeOptimizers[hubID].(*optimizerPkg.TaxRateOptimizer).CurrentFeeRate = 0.2
	bcm.feeOptimizers[hubID].(*optimizerPkg.TaxRateOptimizer).LastPredictedInvestment = 123.4
	bcm.Broker.BrokerBalance[hubID][0] = big.NewInt(70)
	if params.ShardNum > 1 {
		bcm.Broker.BrokerBalance[hubID][1] = big.NewInt(80)
	}
	if params.ShardNum > 2 {
		bcm.Broker.BrokerBalance[hubID][2] = big.NewInt(90)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to switch working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	if err := os.MkdirAll("hubres", os.ModePerm); err != nil {
		t.Fatalf("failed to create hubres directory: %v", err)
	}

	bcm.writeDataToCsv(true, 0)
	bcm.writeDataToCsv(false, 4)
	bcm.writeDataToCsv(true, 0)
	bcm.writeDataToCsv(false, 4)

	csvPath := filepath.Join(tempDir, "hubres", "hub0.csv")
	file, err := os.Open(csvPath)
	if err != nil {
		t.Fatalf("failed to open csv output: %v", err)
	}
	defer file.Close()

	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("failed to read csv output: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected csv to be truncated to header plus one row, got %d rows", len(rows))
	}

	row := rows[1]
	if got := row[4]; got != "240" {
		t.Fatalf("expected fund column to show 240, got %q", got)
	}
	if got := row[7]; got != "240.000000" {
		t.Fatalf("expected current_investment to track current funds, got %q", got)
	}
	if got := row[8]; got != "4" {
		t.Fatalf("expected sampled_cross_txs to be 4, got %q", got)
	}
	if got := row[9]; got != "123.400000" {
		t.Fatalf("expected predicted_investment to be preserved, got %q", got)
	}
	if got := row[15]; got != "prediction" {
		t.Fatalf("expected optimizer phase to be preserved, got %q", got)
	}
}

func newTestBrokerhubCommittee(brokerID string) *BrokerhubCommitteeMod {
	hubID := strings.Repeat("a", 40)
	initialFunds := big.NewInt(0).Mul(new(big.Int).Set(params.Init_broker_Balance), big.NewInt(int64(params.ShardNum)))
	initialFundsFloat, _ := new(big.Float).SetInt(initialFunds).Float64()
	balances := map[string]map[uint64]*big.Int{
		brokerID: shardBalances(0),
		hubID:    shardBalances(params.Init_broker_Balance.Int64()),
	}
	lockBalances := map[string]map[uint64]*big.Int{
		brokerID: shardBalances(0),
		hubID:    shardBalances(0),
	}
	profitBalances := map[string]map[uint64]*big.Float{
		brokerID: shardProfitBalances(),
		hubID:    shardProfitBalances(),
	}

	return &BrokerhubCommitteeMod{
		Broker: &broker.Broker{
			BrokerAddress:  []utils.Address{brokerID, hubID},
			BrokerBalance:  balances,
			LockBalance:    lockBalances,
			ProfitBalance:  profitBalances,
			BrokerRawMegs:  make(map[string]*message.BrokerRawMeg),
			RawTx2BrokerTx: make(map[string][]string),
			Brokerage:      big.NewFloat(1),
		},
		BrokerHubAccountList:   []utils.Address{hubID},
		brokerConfirm1Pool:     make(map[string]*message.Mag1Confirm),
		brokerConfirm2Pool:     make(map[string]*message.Mag2Confirm),
		brokerEpochProfitInB2E: make(map[string]*big.Float),
		brokerhubEpochProfit: map[string]*big.Float{
			hubID: big.NewFloat(0),
		},
		brokerhubEpochGrossRevenue: map[string]*big.Float{
			hubID: big.NewFloat(0),
		},
		brokerInfoListInBrokerHub: map[string][]*message.BrokerInfoInBrokerhub{
			hubID: {},
		},
		brokerJoinBrokerHubState: make(map[string]string),
		brokerDirectUtilityEst:   make(map[string]float64),
		brokerB2EProfitHistory:   make(map[string][]float64),
		feeOptimizers: map[string]optimizerPkg.FeeOptimizer{
			hubID: optimizerPkg.NewTaxRateOptimizer(hubID, optimizerPkg.DefaultTaxOptimizerConfig(initialFundsFloat)),
		},
		brokerCompetitionState: make(map[string]*competitionDecisionState),
		hubObservedFeeHistory:  make(map[string][]float64),
		feeOptimizerMode:       params.FeeOptimizerModeTaxRate,
		rng:                    rand.New(rand.NewSource(1)),
		competitionTuning:      defaultCompetitionDecisionTuning(),
		sl: &supervisor_log.SupervisorLog{
			Slog: log.New(io.Discard, "", 0),
		},
		restBrokerRawMegPool:  make([]*message.BrokerRawMeg, 0),
		restBrokerRawMegPool2: make([]*message.BrokerRawMeg, 0),
		epochCrossTxSamples:   make([]optimizerPkg.TransactionSample, 0),
		hubParams: simulation_param{
			endedEpoch:   params.ExchangeModeEpochLimit(params.ExchangeModeInfinite),
			currentEpoch: 1,
			exchangeMode: params.ExchangeModeInfinite,
		},
	}
}

func TestNewFeeOptimizerUsesPaperMonopolyMode(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	bcm.feeOptimizerMode = params.FeeOptimizerModePaperMonopoly
	bcm.simSeed = 42

	opt := bcm.newFeeOptimizer(strings.Repeat("a", 40), 15000)
	if got := opt.DebugState().Mode; got != params.FeeOptimizerModePaperMonopoly {
		t.Fatalf("expected paper monopoly optimizer, got %q", got)
	}
}

func TestCalManagementExpanseRatioPassesStrongestCompetitorMetrics(t *testing.T) {
	bcm := newTestBrokerhubCommittee(strings.Repeat("b", 40))
	secondHubID := strings.Repeat("c", 40)
	bcm.BrokerHubAccountList = []utils.Address{strings.Repeat("a", 40), secondHubID}
	bcm.brokerInfoListInBrokerHub[secondHubID] = []*message.BrokerInfoInBrokerhub{}
	bcm.brokerhubEpochProfit[secondHubID] = big.NewFloat(75)
	bcm.Broker.BrokerAddress = append(bcm.Broker.BrokerAddress, secondHubID)
	bcm.Broker.BrokerBalance[secondHubID] = shardBalances(params.Init_broker_Balance.Int64() * 2)
	bcm.Broker.LockBalance[secondHubID] = shardBalances(0)
	bcm.Broker.ProfitBalance[secondHubID] = shardProfitBalances()
	bcm.feeOptimizerMode = params.FeeOptimizerModePaperMonopoly
	bcm.simSeed = 99
	primaryHubID := bcm.BrokerHubAccountList[0]
	bcm.feeOptimizers = map[string]optimizerPkg.FeeOptimizer{
		primaryHubID: bcm.newFeeOptimizer(primaryHubID, bcm.initialHubFundsFloat64()),
		secondHubID:  bcm.newFeeOptimizer(secondHubID, bcm.initialHubFundsFloat64()),
	}
	bcm.Broker.BrokerBalance[primaryHubID] = shardBalances(params.Init_broker_Balance.Int64())
	bcm.brokerhubEpochProfit[primaryHubID] = big.NewFloat(10)
	bcm.hubParams.currentEpoch = 3

	bcm.calManagementExpanseRatio(nil)

	debugState := bcm.feeOptimizers[primaryHubID].DebugState()
	expectedFunds := bigIntToFloat64(big.NewInt(0).Mul(params.Init_broker_Balance, big.NewInt(int64(params.ShardNum*2))))
	if math.Abs(debugState.StrongestCompetitorFunds-expectedFunds) > 1e-9 {
		t.Fatalf("expected strongest competitor funds %.2f, got %.2f", expectedFunds, debugState.StrongestCompetitorFunds)
	}
	if math.Abs(debugState.StrongestCompetitorEarn-75) > 1e-9 {
		t.Fatalf("expected strongest competitor earn 75, got %.2f", debugState.StrongestCompetitorEarn)
	}
}

func TestBrokerBehaviourSimulatorCompetitionJoinNeedsConfirmation(t *testing.T) {
	brokerID := strings.Repeat("b", 40)
	bcm := newTestBrokerhubCommittee(brokerID)
	hubID := bcm.BrokerHubAccountList[0]
	bcm.hubParams.currentEpoch = 3
	bcm.feeOptimizers[hubID] = &staticFeeOptimizer{
		fee: 0.1,
		debug: optimizerPkg.FeeOptimizerDebug{
			Mode:           params.FeeOptimizerModePaperMonopoly,
			CurrentFeeRate: 0.1,
			MinFeeRate:     0.001,
			OptimizerPhase: competitionPhaseCompetition,
		},
	}
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		bcm.Broker.BrokerBalance[hubID][shard] = big.NewInt(100)
		bcm.Broker.BrokerBalance[brokerID][shard] = big.NewInt(100)
	}
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(120)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(120)
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(10)

	cleanup := prepareSimulationWorkspace(t)
	defer cleanup()
	bcm.writeDataToCsv(true, 0)

	bcm.broker_behaviour_simulator(true)
	if _, ok := bcm.brokerJoinBrokerHubState[brokerID]; ok {
		t.Fatal("expected broker to stay in B2E until the competition signal is confirmed three times")
	}
	state := bcm.brokerCompetitionState[brokerID]
	if state == nil || state.PendingTarget != hubID || state.PendingMode != competitionPendingModeHub || state.ConsecutiveEpochs != 1 {
		t.Fatalf("expected pending join state after first epoch, got %#v", state)
	}

	bcm.hubParams.currentEpoch = 4
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(120)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(120)
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(10)
	bcm.broker_behaviour_simulator(true)
	if _, ok := bcm.brokerJoinBrokerHubState[brokerID]; ok {
		t.Fatal("expected broker to remain in B2E after only two confirmations")
	}

	bcm.hubParams.currentEpoch = 5
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(120)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(120)
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(10)
	bcm.broker_behaviour_simulator(true)
	if joinedHub := bcm.brokerJoinBrokerHubState[brokerID]; joinedHub != hubID {
		t.Fatalf("expected broker to join hub %s after the third confirmation, got %q", hubID, joinedHub)
	}
}

func TestBrokerBehaviourSimulatorCompetitionExitNeedsConfirmation(t *testing.T) {
	brokerID := strings.Repeat("b", 40)
	bcm := newTestBrokerhubCommittee(brokerID)
	hubID := bcm.BrokerHubAccountList[0]
	bcm.hubParams.currentEpoch = 3
	bcm.feeOptimizers[hubID] = &staticFeeOptimizer{
		fee: 0.85,
		debug: optimizerPkg.FeeOptimizerDebug{
			Mode:           params.FeeOptimizerModePaperMonopoly,
			CurrentFeeRate: 0.85,
			MinFeeRate:     0.001,
			OptimizerPhase: competitionPhaseCompetition,
		},
	}
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		bcm.Broker.BrokerBalance[hubID][shard] = big.NewInt(100)
		bcm.Broker.BrokerBalance[brokerID][shard] = big.NewInt(100)
	}
	if res := bcm.JoiningToBrokerhub(brokerID, hubID); res != "done" {
		t.Fatalf("expected initial join to succeed, got %q", res)
	}
	bcm.brokerB2EProfitHistory[brokerID] = []float64{25, 25, 25}
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(30)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(30)
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(25)

	cleanup := prepareSimulationWorkspace(t)
	defer cleanup()
	bcm.writeDataToCsv(true, 0)

	bcm.broker_behaviour_simulator(true)
	if joinedHub := bcm.brokerJoinBrokerHubState[brokerID]; joinedHub != hubID {
		t.Fatalf("expected broker to remain in hub %s until three negative epochs confirm exit, got %q", hubID, joinedHub)
	}
	state := bcm.brokerCompetitionState[brokerID]
	if state == nil || state.PendingTarget != competitionPendingModeB2E || state.PendingMode != competitionPendingModeB2E || state.ConsecutiveEpochs != 1 {
		t.Fatalf("expected pending B2E exit state after first epoch, got %#v", state)
	}

	bcm.hubParams.currentEpoch = 4
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(30)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(30)
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(25)
	bcm.broker_behaviour_simulator(true)
	if joinedHub := bcm.brokerJoinBrokerHubState[brokerID]; joinedHub != hubID {
		t.Fatalf("expected broker to remain in hub %s after only two confirmations, got %q", hubID, joinedHub)
	}

	bcm.hubParams.currentEpoch = 5
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(30)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(30)
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(25)
	bcm.broker_behaviour_simulator(true)
	if _, ok := bcm.brokerJoinBrokerHubState[brokerID]; ok {
		t.Fatal("expected broker to exit to B2E after three consecutive negative competition epochs")
	}
}

func TestBrokerBehaviourSimulatorBypassesCompetitionHysteresisOutsideCompetition(t *testing.T) {
	brokerID := strings.Repeat("b", 40)
	bcm := newTestBrokerhubCommittee(brokerID)
	hubID := bcm.BrokerHubAccountList[0]
	bcm.hubParams.currentEpoch = 3
	bcm.feeOptimizers[hubID] = &staticFeeOptimizer{
		fee: 0.85,
		debug: optimizerPkg.FeeOptimizerDebug{
			Mode:           params.FeeOptimizerModePaperMonopoly,
			CurrentFeeRate: 0.85,
			MinFeeRate:     0.001,
			OptimizerPhase: "memory",
		},
	}
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		bcm.Broker.BrokerBalance[hubID][shard] = big.NewInt(100)
		bcm.Broker.BrokerBalance[brokerID][shard] = big.NewInt(100)
	}
	if res := bcm.JoiningToBrokerhub(brokerID, hubID); res != "done" {
		t.Fatalf("expected initial join to succeed, got %q", res)
	}
	bcm.brokerB2EProfitHistory[brokerID] = []float64{25, 25, 25}
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(30)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(30)
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(25)

	cleanup := prepareSimulationWorkspace(t)
	defer cleanup()
	bcm.writeDataToCsv(true, 0)

	bcm.broker_behaviour_simulator(true)
	if _, ok := bcm.brokerJoinBrokerHubState[brokerID]; ok {
		t.Fatal("expected negative utility in memory phase to exit immediately without competition hysteresis")
	}
}

func TestBrokerBehaviourSimulatorCompetitionLetsSmallerBrokersMoveFirst(t *testing.T) {
	smallBrokerID := strings.Repeat("b", 40)
	largeBrokerID := strings.Repeat("c", 40)
	bcm := newTestBrokerhubCommittee(smallBrokerID)
	hubID := bcm.BrokerHubAccountList[0]
	bcm.Broker.BrokerAddress = []utils.Address{smallBrokerID, largeBrokerID, hubID}
	bcm.Broker.BrokerBalance[smallBrokerID] = shardBalances(10)
	bcm.Broker.BrokerBalance[largeBrokerID] = shardBalances(20)
	bcm.Broker.LockBalance[largeBrokerID] = shardBalances(0)
	bcm.Broker.ProfitBalance[largeBrokerID] = shardProfitBalances()
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		bcm.Broker.BrokerBalance[hubID][shard] = big.NewInt(100)
	}
	bcm.feeOptimizers[hubID] = &staticFeeOptimizer{
		fee: 0.1,
		debug: optimizerPkg.FeeOptimizerDebug{
			Mode:           params.FeeOptimizerModePaperMonopoly,
			CurrentFeeRate: 0.1,
			MinFeeRate:     0.001,
			OptimizerPhase: competitionPhaseCompetition,
		},
	}
	bcm.hubParams.currentEpoch = 3
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(30)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(30)
	bcm.brokerEpochProfitInB2E[smallBrokerID] = big.NewFloat(0.0)
	bcm.brokerEpochProfitInB2E[largeBrokerID] = big.NewFloat(6.0)

	cleanup := prepareSimulationWorkspace(t)
	defer cleanup()
	bcm.writeDataToCsv(true, 0)

	bcm.broker_behaviour_simulator(true)
	if _, ok := bcm.brokerJoinBrokerHubState[smallBrokerID]; ok {
		t.Fatal("expected smaller broker to wait for a third confirmation before joining")
	}
	if _, ok := bcm.brokerJoinBrokerHubState[largeBrokerID]; ok {
		t.Fatal("expected larger broker to remain in B2E after the first epoch")
	}

	bcm.hubParams.currentEpoch = 4
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(30)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(30)
	bcm.brokerEpochProfitInB2E[smallBrokerID] = big.NewFloat(0.0)
	bcm.brokerEpochProfitInB2E[largeBrokerID] = big.NewFloat(6.0)
	bcm.broker_behaviour_simulator(true)
	if _, ok := bcm.brokerJoinBrokerHubState[smallBrokerID]; ok {
		t.Fatal("expected smaller broker to remain in B2E after only two confirmations")
	}

	bcm.hubParams.currentEpoch = 5
	bcm.brokerhubEpochProfit[hubID] = big.NewFloat(30)
	bcm.brokerhubEpochGrossRevenue[hubID] = big.NewFloat(30)
	bcm.brokerEpochProfitInB2E[smallBrokerID] = big.NewFloat(0.0)
	bcm.brokerEpochProfitInB2E[largeBrokerID] = big.NewFloat(6.0)
	bcm.broker_behaviour_simulator(true)
	if joinedHub := bcm.brokerJoinBrokerHubState[smallBrokerID]; joinedHub != hubID {
		t.Fatalf("expected smaller broker to join hub %s first, got %q", hubID, joinedHub)
	}
	if _, ok := bcm.brokerJoinBrokerHubState[largeBrokerID]; ok {
		t.Fatal("expected larger broker to remain in B2E because its scaled join margin is higher")
	}
}

func TestEstimateHubUtilityCompetitionFeeCutBoostsAttraction(t *testing.T) {
	brokerID := strings.Repeat("b", 40)
	incumbentHubID := strings.Repeat("a", 40)
	challengerHubID := strings.Repeat("c", 40)
	bcm := newTestBrokerhubCommittee(brokerID)

	bcm.BrokerHubAccountList = []utils.Address{incumbentHubID, challengerHubID}
	bcm.Broker.BrokerAddress = []utils.Address{brokerID, incumbentHubID, challengerHubID}
	bcm.Broker.BrokerBalance[brokerID] = shardBalances(100)
	bcm.Broker.BrokerBalance[incumbentHubID] = shardBalances(400)
	bcm.Broker.BrokerBalance[challengerHubID] = shardBalances(50)
	bcm.Broker.LockBalance[challengerHubID] = shardBalances(0)
	bcm.Broker.ProfitBalance[challengerHubID] = shardProfitBalances()
	bcm.brokerInfoListInBrokerHub[incumbentHubID] = []*message.BrokerInfoInBrokerhub{}
	bcm.brokerInfoListInBrokerHub[challengerHubID] = []*message.BrokerInfoInBrokerhub{}
	bcm.brokerhubEpochGrossRevenue[incumbentHubID] = big.NewFloat(60)
	bcm.brokerhubEpochGrossRevenue[challengerHubID] = big.NewFloat(30)
	bcm.feeOptimizers = map[string]optimizerPkg.FeeOptimizer{
		incumbentHubID: &staticFeeOptimizer{
			fee: 0.05,
			debug: optimizerPkg.FeeOptimizerDebug{
				OptimizerPhase: competitionPhaseCompetition,
			},
		},
		challengerHubID: &staticFeeOptimizer{
			fee: 0.05,
			debug: optimizerPkg.FeeOptimizerDebug{
				OptimizerPhase: competitionPhaseCompetition,
			},
		},
	}
	bcm.hubObservedFeeHistory[incumbentHubID] = []float64{0.05, 0.05}
	bcm.hubObservedFeeHistory[challengerHubID] = []float64{0.15, 0.05}

	directUtility := 10.0
	boostedUtility := bcm.estimateHubUtility(brokerID, challengerHubID, directUtility)
	bcm.hubObservedFeeHistory[challengerHubID] = []float64{0.05, 0.05}
	baseUtility := bcm.estimateHubUtility(brokerID, challengerHubID, directUtility)
	if boostedUtility <= baseUtility {
		t.Fatalf("expected recent competition fee cut to raise utility, got boosted %.6f <= base %.6f", boostedUtility, baseUtility)
	}
	if boostedUtility/baseUtility > bcm.competitionTuning.AttractionBoostCap+1e-9 {
		t.Fatalf("expected attraction boost ratio to stay under cap %f, got %.6f -> %.6f", bcm.competitionTuning.AttractionBoostCap, baseUtility, boostedUtility)
	}
}

func TestBrokerBehaviourSimulatorCompetitionRetentionPreventsMildJump(t *testing.T) {
	brokerID := strings.Repeat("b", 40)
	currentHubID := strings.Repeat("a", 40)
	challengerHubID := strings.Repeat("c", 40)
	bcm := newTestBrokerhubCommittee(brokerID)
	bcm.BrokerHubAccountList = []utils.Address{currentHubID, challengerHubID}
	bcm.Broker.BrokerAddress = []utils.Address{brokerID, currentHubID, challengerHubID}
	bcm.Broker.BrokerBalance[brokerID] = shardBalances(100)
	bcm.Broker.BrokerBalance[currentHubID] = shardBalances(300)
	bcm.Broker.BrokerBalance[challengerHubID] = shardBalances(320)
	bcm.Broker.LockBalance[challengerHubID] = shardBalances(0)
	bcm.Broker.ProfitBalance[challengerHubID] = shardProfitBalances()
	bcm.brokerInfoListInBrokerHub[currentHubID] = []*message.BrokerInfoInBrokerhub{}
	bcm.brokerInfoListInBrokerHub[challengerHubID] = []*message.BrokerInfoInBrokerhub{}
	bcm.brokerhubEpochGrossRevenue[currentHubID] = big.NewFloat(100)
	bcm.brokerhubEpochGrossRevenue[challengerHubID] = big.NewFloat(100)
	bcm.feeOptimizers = map[string]optimizerPkg.FeeOptimizer{
		currentHubID: &staticFeeOptimizer{
			fee:   0.10,
			debug: optimizerPkg.FeeOptimizerDebug{OptimizerPhase: competitionPhaseCompetition},
		},
		challengerHubID: &staticFeeOptimizer{
			fee:   0.09,
			debug: optimizerPkg.FeeOptimizerDebug{OptimizerPhase: competitionPhaseCompetition},
		},
	}
	if res := bcm.JoiningToBrokerhub(brokerID, currentHubID); res != "done" {
		t.Fatalf("expected initial join to succeed, got %q", res)
	}
	bcm.brokerB2EProfitHistory[brokerID] = []float64{5, 5, 5}
	bcm.brokerEpochProfitInB2E[brokerID] = big.NewFloat(5)

	cleanup := prepareSimulationWorkspace(t)
	defer cleanup()
	bcm.writeDataToCsv(true, 0)

	for epoch := 3; epoch <= 5; epoch++ {
		bcm.hubParams.currentEpoch = epoch
		bcm.brokerhubEpochProfit[currentHubID] = big.NewFloat(100)
		bcm.brokerhubEpochGrossRevenue[currentHubID] = big.NewFloat(100)
		bcm.brokerhubEpochProfit[challengerHubID] = big.NewFloat(100)
		bcm.brokerhubEpochGrossRevenue[challengerHubID] = big.NewFloat(100)
		bcm.broker_behaviour_simulator(true)
	}

	if joinedHub := bcm.brokerJoinBrokerHubState[brokerID]; joinedHub != currentHubID {
		t.Fatalf("expected incumbent retention margin to keep broker in hub %s, got %q", currentHubID, joinedHub)
	}
}

func shardBalances(value int64) map[uint64]*big.Int {
	result := make(map[uint64]*big.Int)
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		result[shard] = big.NewInt(value)
	}
	return result
}

func prepareSimulationWorkspace(t *testing.T) func() {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to switch working directory: %v", err)
	}
	if err := os.MkdirAll("hubres", os.ModePerm); err != nil {
		t.Fatalf("failed to create hubres directory: %v", err)
	}
	return func() {
		_ = os.Chdir(oldWd)
	}
}

func shardProfitBalances() map[uint64]*big.Float {
	result := make(map[uint64]*big.Float)
	for shard := uint64(0); shard < uint64(params.ShardNum); shard++ {
		result[shard] = big.NewFloat(0)
	}
	return result
}

type staticFeeOptimizer struct {
	fee   float64
	debug optimizerPkg.FeeOptimizerDebug
}

func (s *staticFeeOptimizer) Optimize(_ optimizerPkg.EpochMetrics) float64 {
	s.debug.CurrentFeeRate = s.fee
	s.debug.MinFeeRate = 0.001
	return s.fee
}

func (s *staticFeeOptimizer) FeeRate() float64 {
	return s.fee
}

func (s *staticFeeOptimizer) MinFee() float64 {
	return 0.001
}

func (s *staticFeeOptimizer) DebugState() optimizerPkg.FeeOptimizerDebug {
	s.debug.CurrentFeeRate = s.fee
	s.debug.MinFeeRate = 0.001
	return s.debug
}
