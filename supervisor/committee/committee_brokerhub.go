package committee

import (
	"blockEmulator/broker"
	"blockEmulator/core"
	"blockEmulator/message"
	"blockEmulator/mytool"
	"blockEmulator/networks"
	"blockEmulator/params"
	"blockEmulator/supervisor/Broker2Earn"
	optimizerPkg "blockEmulator/supervisor/optimizer"
	"blockEmulator/supervisor/signal"
	"blockEmulator/supervisor/supervisor_log"
	"blockEmulator/utils"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"math/rand"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type simulation_param struct {
	hubHigherRate int64
	endedEpoch    int
	txsPerEpoch   int
	currentEpoch  int
	exchangeMode  string
}

type competitionDecisionState struct {
	PendingTarget     string
	PendingMode       string
	ConsecutiveEpochs int
}

type competitionDecisionTuning struct {
	FeeHistoryWindow         int
	JoinConfirmEpochs        int
	SwitchConfirmEpochs      int
	ExitConfirmEpochs        int
	JoinMarginBase           float64
	SwitchMarginBase         float64
	ExitMarginBase           float64
	JoinSwitchUtilityScale   float64
	ExitUtilityScale         float64
	SwitchScaleCap           float64
	IncumbentRetentionMargin float64
	RecentFeeCutWeight       float64
	FeeGapWeight             float64
	AttractionBoostCap       float64
}

func defaultCompetitionDecisionTuning() competitionDecisionTuning {
	return competitionDecisionTuning{
		FeeHistoryWindow:         4,
		JoinConfirmEpochs:        3,
		SwitchConfirmEpochs:      3,
		ExitConfirmEpochs:        3,
		JoinMarginBase:           2.55,
		SwitchMarginBase:         2.95,
		ExitMarginBase:           1.28,
		JoinSwitchUtilityScale:   0.12,
		ExitUtilityScale:         0.07,
		SwitchScaleCap:           2.20,
		IncumbentRetentionMargin: 0.78,
		RecentFeeCutWeight:       0.60,
		FeeGapWeight:             0.25,
		AttractionBoostCap:       1.12,
	}
}

// CLPA committee operations
type BrokerhubCommitteeMod struct {
	csvPath      string
	dataTotalNum int
	nowDataNum   int
	dataTxNums   int
	batchDataNum int

	//Broker related  attributes avatar
	Broker                *broker.Broker
	brokerConfirm1Pool    map[string]*message.Mag1Confirm
	brokerConfirm2Pool    map[string]*message.Mag2Confirm
	restBrokerRawMegPool  []*message.BrokerRawMeg
	restBrokerRawMegPool2 []*message.BrokerRawMeg
	brokerTxPool          []*core.Transaction
	BrokerModuleLock      sync.Mutex
	BrokerBalanceLock     sync.Mutex

	// logger module
	sl *supervisor_log.SupervisorLog

	// control components
	Ss          *signal.StopSignal // to control the stop message sending
	IpNodeTable map[uint64]map[uint64]string

	// transaction revenue list
	transaction_fee_list []*big.Int

	// transaction value list
	transaction_value_list []*big.Int

	// log balance
	Result_lockBalance   map[string][]string
	Result_brokerBalance map[string][]string
	Result_Profit        map[string][]string
	LastInvokeTime       map[string]time.Time
	LastInvokeTimeMutex  sync.Mutex

	// Broker infomation in BrokerHub
	brokerInfoListInBrokerHub map[string][]*message.BrokerInfoInBrokerhub

	// BorkerHub List
	BrokerHubAccountList []utils.Address

	// Broker加入Brokerhub的状态
	brokerJoinBrokerHubState map[string]string

	// Broker 最近一次加入B2E的收益
	brokerEpochProfitInB2E map[string]*big.Float
	brokerDirectUtilityEst map[string]float64
	brokerB2EProfitHistory map[string][]float64

	// BrokerHub 这一轮的收益
	brokerhubEpochProfit       map[string]*big.Float
	brokerhubEpochGrossRevenue map[string]*big.Float

	// taxOptimizer map[string]*optimizer 替换为新的优化器
	feeOptimizers          map[string]optimizerPkg.FeeOptimizer
	brokerCompetitionState map[string]*competitionDecisionState
	hubObservedFeeHistory  map[string][]float64

	epochCrossTxSamples     []optimizerPkg.TransactionSample
	epochCrossTxSamplesLock sync.Mutex
	blockInfoProgressLock   sync.Mutex
	nonEmptyBlockInfoCount  uint64
	lastBlockInfoSnapshot   uint64

	feeOptimizerMode  string
	simSeed           int64
	rng               *rand.Rand
	competitionTuning competitionDecisionTuning

	hubParams simulation_param
}

func NewBrokerhubCommitteeMod(Ip_nodeTable map[uint64]map[uint64]string, Ss *signal.StopSignal, sl *supervisor_log.SupervisorLog, csvFilePath string, dataNum, batchNum int, exchangeMode, feeOptimizerMode string, simSeed int64) *BrokerhubCommitteeMod {
	fmt.Println("Using Brokerhub Supervisor")
	normalizedExchangeMode, err := params.NormalizeExchangeMode(exchangeMode)
	if err != nil {
		log.Panic(err)
	}
	normalizedFeeOptimizerMode, err := params.NormalizeFeeOptimizerMode(feeOptimizerMode)
	if err != nil {
		log.Panic(err)
	}
	seedToUse := simSeed
	if seedToUse == 0 {
		seedToUse = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seedToUse))
	Broker2Earn.SetRandomSeed(seedToUse)
	broker := new(broker.Broker)
	broker.NewBroker(nil)
	result_lockBalance := make(map[string][]string)
	result_brokerBalance := make(map[string][]string)
	result_Profit := make(map[string][]string)
	block_txs := make(map[uint64][]string)

	for _, brokeraddress := range broker.BrokerAddress {
		result_lockBalance[brokeraddress] = make([]string, 0)
		result_brokerBalance[brokeraddress] = make([]string, 0)
		result_Profit[brokeraddress] = make([]string, 0)

		a := ""
		b := ""
		title := ""
		for i := 0; i < params.ShardNum; i++ {
			title += "shard" + strconv.Itoa(i) + ","
			a += params.Init_broker_Balance.String() + ","
			b += "0,"
		}
		result_lockBalance[brokeraddress] = append(result_lockBalance[brokeraddress], title)
		result_brokerBalance[brokeraddress] = append(result_brokerBalance[brokeraddress], title)
		result_Profit[brokeraddress] = append(result_Profit[brokeraddress], title)

		result_lockBalance[brokeraddress] = append(result_lockBalance[brokeraddress], b)
		result_brokerBalance[brokeraddress] = append(result_brokerBalance[brokeraddress], a)
		result_Profit[brokeraddress] = append(result_Profit[brokeraddress], b)
	}
	for i := 0; i < params.ShardNum; i++ {
		block_txs[uint64(i)] = make([]string, 0)
		block_txs[uint64(i)] = append(block_txs[uint64(i)], "txExcuted, broker1Txs, broker2Txs, allocatedTxs")
	}

	brokerhub_account_list := []string{
		"d15e634876c991990542b8d75a3e94eaacdf840e",
		"c00eb36ed0dac15d7fb4c0ff92580be24074a14d",
	}

	broker_info_list_in_hub := make(map[string][]*message.BrokerInfoInBrokerhub)

	for _, val := range brokerhub_account_list {
		broker_info_list_in_hub[val] = make([]*message.BrokerInfoInBrokerhub, 0)
	}

	var simulation_parameters simulation_param
	simulation_parameters.hubHigherRate = 10
	simulation_parameters.txsPerEpoch = 1000
	simulation_parameters.endedEpoch = params.ExchangeModeEpochLimit(normalizedExchangeMode)
	simulation_parameters.currentEpoch = 0
	simulation_parameters.exchangeMode = normalizedExchangeMode

	fee_list := make([]*big.Int, 0)
	value_list := make([]*big.Int, 0)
	// hub_higher_ratio := int64(10)
	// num_of_special_tx := 0
	for i := 0; i < simulation_parameters.txsPerEpoch; i++ {
		fee := new(big.Int).SetUint64(uint64(10 + rng.Intn(10)))
		broker_num_bios := 4 + len(brokerhub_account_list) + params.BrokerNum
		min_balance := int(params.Init_broker_Balance.Int64()) * broker_num_bios * params.ShardNum * 2 / simulation_parameters.txsPerEpoch
		value := new(big.Int).SetUint64(uint64(min_balance + rng.Intn(min_balance)))
		// if i%(simulation_parameters.txsPerEpoch/2) == 0 {
		// 	fee.SetUint64(7500)
		// 	value.SetUint64(500)
		// }
		// if num_of_special_tx < 3 {
		// 	fee.SetUint64(7500)
		// 	value.SetUint64(100)
		// 	num_of_special_tx++
		// }
		fee_list = append(fee_list, fee)
		value_list = append(value_list, value)
	}

	return &BrokerhubCommitteeMod{
		csvPath:                    csvFilePath,
		dataTotalNum:               dataNum,
		batchDataNum:               batchNum,
		nowDataNum:                 0,
		dataTxNums:                 0,
		brokerConfirm1Pool:         make(map[string]*message.Mag1Confirm),
		brokerConfirm2Pool:         make(map[string]*message.Mag2Confirm),
		restBrokerRawMegPool:       make([]*message.BrokerRawMeg, 0),
		restBrokerRawMegPool2:      make([]*message.BrokerRawMeg, 0),
		brokerTxPool:               make([]*core.Transaction, 0),
		Broker:                     broker,
		IpNodeTable:                Ip_nodeTable,
		Ss:                         Ss,
		sl:                         sl,
		Result_lockBalance:         result_lockBalance,
		Result_brokerBalance:       result_brokerBalance,
		Result_Profit:              result_Profit,
		LastInvokeTime:             make(map[string]time.Time),
		transaction_fee_list:       fee_list,
		transaction_value_list:     value_list,
		brokerInfoListInBrokerHub:  broker_info_list_in_hub,
		BrokerHubAccountList:       brokerhub_account_list,
		brokerJoinBrokerHubState:   make(map[string]string),
		brokerEpochProfitInB2E:     make(map[string]*big.Float),
		brokerDirectUtilityEst:     make(map[string]float64),
		brokerB2EProfitHistory:     make(map[string][]float64),
		brokerhubEpochProfit:       make(map[string]*big.Float),
		brokerhubEpochGrossRevenue: make(map[string]*big.Float),
		feeOptimizers:              make(map[string]optimizerPkg.FeeOptimizer),
		brokerCompetitionState:     make(map[string]*competitionDecisionState),
		hubObservedFeeHistory:      make(map[string][]float64),
		epochCrossTxSamples:        make([]optimizerPkg.TransactionSample, 0),
		feeOptimizerMode:           normalizedFeeOptimizerMode,
		simSeed:                    simSeed,
		rng:                        rng,
		competitionTuning:          defaultCompetitionDecisionTuning(),
		hubParams:                  simulation_parameters,
	}

}

func (bcm *BrokerhubCommitteeMod) hasFiniteEpochLimit() bool {
	return bcm.hubParams.endedEpoch < params.ExchangeModeEpochLimit(params.ExchangeModeInfinite)
}

func (bcm *BrokerhubCommitteeMod) reachedEpochLimit() bool {
	return bcm.hasFiniteEpochLimit() && bcm.hubParams.currentEpoch >= bcm.hubParams.endedEpoch
}

func (bcm *BrokerhubCommitteeMod) HandleOtherMessage([]byte) {}
func (bcm *BrokerhubCommitteeMod) fetchModifiedMap(key string) uint64 {
	return uint64(utils.Addr2Shard(key))
}

func (bcm *BrokerhubCommitteeMod) txSending(txlist []*core.Transaction) {
	// the txs will be sent
	sendToShard := make(map[uint64][]*core.Transaction)

	for idx := 0; idx <= len(txlist); idx++ {
		if idx > 0 && (idx%params.InjectSpeed == 0 || idx == len(txlist)) {
			// send to shard
			for sid := uint64(0); sid < uint64(params.ShardNum); sid++ {
				it := message.InjectTxs{
					Txs:       sendToShard[sid],
					ToShardID: sid,
				}
				itByte, err := json.Marshal(it)
				if err != nil {
					log.Panic(err)
				}
				send_msg := message.MergeMessage(message.CInject, itByte)
				go networks.TcpDial(send_msg, bcm.IpNodeTable[sid][0])
			}
			sendToShard = make(map[uint64][]*core.Transaction)
			//time.Sleep(time.Second)
		}
		if idx == len(txlist) {
			break
		}
		tx := txlist[idx]
		sendersid := bcm.fetchModifiedMap(tx.Sender)

		if tx.Isbrokertx2 {
			sendersid = bcm.fetchModifiedMap(tx.Recipient)
		}
		sendToShard[sendersid] = append(sendToShard[sendersid], tx)
	}
}

func (bcm *BrokerhubCommitteeMod) calculateTotalBalance(addr string) *big.Int {
	BrokerBalance := big.NewInt(0)
	for _, balance := range bcm.Broker.BrokerBalance[addr] {
		BrokerBalance.Add(BrokerBalance, balance)
	}
	return BrokerBalance
}

func (bcm *BrokerhubCommitteeMod) initialHubFundsFloat64() float64 {
	initialFunds := new(big.Int).Mul(
		new(big.Int).Set(params.Init_broker_Balance),
		big.NewInt(int64(params.ShardNum)),
	)
	value, _ := new(big.Float).SetInt(initialFunds).Float64()
	return value
}

func (bcm *BrokerhubCommitteeMod) randomHexString(length int) string {
	bytesNeeded := length / 2
	if length%2 != 0 {
		bytesNeeded++
	}
	randomBytes := make([]byte, bytesNeeded)
	for idx := range randomBytes {
		randomBytes[idx] = byte(bcm.rng.Intn(256))
	}
	hexString := hex.EncodeToString(randomBytes)
	if len(hexString) > length {
		return hexString[:length]
	}
	return hexString
}

func (bcm *BrokerhubCommitteeMod) newFeeOptimizer(hubID string, initialFunds float64) optimizerPkg.FeeOptimizer {
	switch bcm.feeOptimizerMode {
	case params.FeeOptimizerModePaperMonopoly:
		return optimizerPkg.NewPaperMonopolyOptimizer(
			hubID,
			optimizerPkg.DefaultPaperMonopolyConfig(initialFunds, bcm.simSeed),
		)
	default:
		return optimizerPkg.NewTaxRateOptimizer(
			hubID,
			optimizerPkg.DefaultTaxOptimizerConfig(initialFunds),
		)
	}
}

func (bcm *BrokerhubCommitteeMod) appendEpochCrossTxSamples(brokerRawMegs []*message.BrokerRawMeg) {
	if len(brokerRawMegs) == 0 {
		return
	}

	samples := make([]optimizerPkg.TransactionSample, 0, len(brokerRawMegs))
	for _, brokerRawMeg := range brokerRawMegs {
		if brokerRawMeg == nil || brokerRawMeg.Tx == nil {
			continue
		}
		fee, _ := new(big.Float).SetInt(brokerRawMeg.Tx.Fee).Float64()
		amount, _ := new(big.Float).SetInt(brokerRawMeg.Tx.Value).Float64()
		samples = append(samples, optimizerPkg.TransactionSample{
			Fee:      fee,
			Amount:   amount,
			Sender:   brokerRawMeg.Tx.Sender,
			Receiver: brokerRawMeg.Tx.Recipient,
		})
	}

	if len(samples) == 0 {
		return
	}

	bcm.epochCrossTxSamplesLock.Lock()
	bcm.epochCrossTxSamples = append(bcm.epochCrossTxSamples, samples...)
	bcm.epochCrossTxSamplesLock.Unlock()
}

func (bcm *BrokerhubCommitteeMod) snapshotEpochCrossTxSamples() []optimizerPkg.TransactionSample {
	bcm.epochCrossTxSamplesLock.Lock()
	defer bcm.epochCrossTxSamplesLock.Unlock()

	snapshot := append([]optimizerPkg.TransactionSample(nil), bcm.epochCrossTxSamples...)
	bcm.epochCrossTxSamples = bcm.epochCrossTxSamples[:0]
	return snapshot
}

func (bcm *BrokerhubCommitteeMod) markNonEmptyBlockInfo() {
	bcm.blockInfoProgressLock.Lock()
	bcm.nonEmptyBlockInfoCount++
	bcm.blockInfoProgressLock.Unlock()
}

func (bcm *BrokerhubCommitteeMod) snapshotBlockInfoProgress() uint64 {
	bcm.blockInfoProgressLock.Lock()
	defer bcm.blockInfoProgressLock.Unlock()

	current := bcm.nonEmptyBlockInfoCount
	delta := current - bcm.lastBlockInfoSnapshot
	bcm.lastBlockInfoSnapshot = current
	return delta
}

func bigIntToFloat64(value *big.Int) float64 {
	if value == nil {
		return 0
	}
	floatValue, _ := new(big.Float).SetInt(value).Float64()
	return floatValue
}

func (bcm *BrokerhubCommitteeMod) init_brokerhub() {
	BrokerHubInitialBalance := new(big.Int).Set(params.Init_broker_Balance)
	initialFunds := bcm.initialHubFundsFloat64()
	for _, brokerhub_id := range bcm.BrokerHubAccountList {
		bcm.Broker.BrokerAddress = append(bcm.Broker.BrokerAddress, brokerhub_id)

		bcm.Broker.BrokerBalance[brokerhub_id] = make(map[uint64]*big.Int)
		for sid := uint64(0); sid < uint64(params.ShardNum); sid++ {
			bcm.Broker.BrokerBalance[brokerhub_id][sid] = new(big.Int).Set(BrokerHubInitialBalance)
		}
		bcm.Broker.LockBalance[brokerhub_id] = make(map[uint64]*big.Int)
		for sid := uint64(0); sid < uint64(params.ShardNum); sid++ {
			bcm.Broker.LockBalance[brokerhub_id][sid] = new(big.Int).Set(big.NewInt(0))
		}

		bcm.Broker.ProfitBalance[brokerhub_id] = make(map[uint64]*big.Float)
		for sid := uint64(0); sid < uint64(params.ShardNum); sid++ {
			bcm.Broker.ProfitBalance[brokerhub_id][sid] = new(big.Float).Set(big.NewFloat(0))
		}
		bcm.brokerhubEpochProfit[brokerhub_id] = big.NewFloat(0)
		bcm.brokerhubEpochGrossRevenue[brokerhub_id] = big.NewFloat(0)

		// 初始化新版优化器 (初始资金设为0，初始费率为0.2)
		bcm.feeOptimizers[brokerhub_id] = bcm.newFeeOptimizer(brokerhub_id, initialFunds)
	}
}

func (bcm *BrokerhubCommitteeMod) judgeBrokerhubInfo(broker_id string, brokerhub_id string) (string, bool) {
	if !slices.Contains(bcm.BrokerHubAccountList, brokerhub_id) {
		return "hub not exist", false
	}
	if _, exist := bcm.Broker.BrokerBalance[brokerhub_id]; !exist {
		return "hub not init", false
	}
	if _, exist := bcm.Broker.BrokerBalance[broker_id]; !exist {
		return "not broker", false
	}
	return "", true
}

func (bcm *BrokerhubCommitteeMod) JoiningToBrokerhubOrstackMore(broker_id string, brokerhub_id string, token *big.Int) string {
	bcm.BrokerBalanceLock.Lock()
	defer bcm.BrokerBalanceLock.Unlock()
	if !slices.Contains(bcm.BrokerHubAccountList, brokerhub_id) {
		return "hub not exist"
	}
	if _, exist := bcm.Broker.BrokerBalance[brokerhub_id]; !exist {
		return "hub not init"
	}
	if _, exist := bcm.brokerJoinBrokerHubState[broker_id]; exist {
		for _, broker_info := range bcm.brokerInfoListInBrokerHub[brokerhub_id] {
			if broker_info.BrokerAddr == broker_id {
				broker_info.BrokerBalance.Add(broker_info.BrokerBalance, token)
				bcm.Broker.BrokerBalance[brokerhub_id][0].Add(
					bcm.Broker.BrokerBalance[brokerhub_id][0],
					token,
				)
				return "satck more done"
			}
		}
		return "none info"
	}
	if bcm.Broker.IsBroker(broker_id) {
		return "is broker"
	}

	bcm.Broker.BrokerBalance[brokerhub_id][0].Add(
		bcm.Broker.BrokerBalance[brokerhub_id][0],
		token,
	)
	brokerinfo := new(message.BrokerInfoInBrokerhub)
	brokerinfo.BrokerAddr = broker_id
	brokerinfo.BrokerBalance = new(big.Int).Set(token)
	brokerinfo.BrokerProfit = big.NewFloat(0)
	bcm.brokerInfoListInBrokerHub[brokerhub_id] = append(
		bcm.brokerInfoListInBrokerHub[brokerhub_id],
		brokerinfo,
	)
	bcm.brokerJoinBrokerHubState[broker_id] = brokerhub_id

	return "done"
}

func (bcm *BrokerhubCommitteeMod) WithdrawBrokerhubDirectly(broker_id string, brokerhub_id string) (string, float64, uint64) {
	bcm.BrokerBalanceLock.Lock()
	defer bcm.BrokerBalanceLock.Unlock()
	if !slices.Contains(bcm.BrokerHubAccountList, brokerhub_id) {
		return "hub not exist", 0, 0
	}
	if _, exist := bcm.Broker.BrokerBalance[brokerhub_id]; !exist {
		return "hub not init", 0, 0
	}
	{
		hub_id, exist := bcm.brokerJoinBrokerHubState[broker_id]
		if !exist {
			return "not in hub", 0, 0
		}
		if hub_id != brokerhub_id {
			return "hub id error", 0, 0
		}
	}

	for _, brokerinfo := range bcm.brokerInfoListInBrokerHub[brokerhub_id] {
		if brokerinfo.BrokerAddr == broker_id {
			bcm.brokerInfoListInBrokerHub[brokerhub_id] = slices.DeleteFunc(
				bcm.brokerInfoListInBrokerHub[brokerhub_id],
				func(x *message.BrokerInfoInBrokerhub) bool {
					return x.BrokerAddr == broker_id
				},
			)
			delete(bcm.brokerJoinBrokerHubState, broker_id)
			profit_in_hub, _ := brokerinfo.BrokerProfit.Float64()
			fund_in_hub := brokerinfo.BrokerBalance.Uint64()
			fund_in_hub += uint64(profit_in_hub)
			return "done", profit_in_hub, fund_in_hub
		}
	}
	return "not in hub", 0, 0
}

func (bcm *BrokerhubCommitteeMod) JoiningToBrokerhub(broker_id string, brokerhub_id string) string {
	bcm.BrokerBalanceLock.Lock()
	defer bcm.BrokerBalanceLock.Unlock()
	if res, ok := bcm.judgeBrokerhubInfo(broker_id, brokerhub_id); !ok {
		return res
	}

	if _, exist := bcm.brokerJoinBrokerHubState[broker_id]; exist {
		return "already in"
	}

	for i := uint64(0); i < uint64(params.ShardNum); i++ {
		bcm.Broker.BrokerBalance[brokerhub_id][i].Add(
			bcm.Broker.BrokerBalance[brokerhub_id][i],
			bcm.Broker.BrokerBalance[broker_id][i],
		)
	}

	// 更新账户信息
	brokerinfo := new(message.BrokerInfoInBrokerhub)
	brokerinfo.BrokerAddr = broker_id
	brokerinfo.BrokerBalance = new(big.Int).Set(bcm.calculateTotalBalance(broker_id))
	brokerinfo.BrokerProfit = big.NewFloat(0)
	bcm.brokerInfoListInBrokerHub[brokerhub_id] = append(
		bcm.brokerInfoListInBrokerHub[brokerhub_id],
		brokerinfo,
	)

	bcm.brokerJoinBrokerHubState[broker_id] = brokerhub_id
	return "done"
}

func (bcm *BrokerhubCommitteeMod) ExitingBrokerHub(broker_id string, brokerhub_id string) string {
	bcm.BrokerBalanceLock.Lock()
	defer bcm.BrokerBalanceLock.Unlock()
	if res, ok := bcm.judgeBrokerhubInfo(broker_id, brokerhub_id); !ok {
		return res
	}
	{
		hub_id, exist := bcm.brokerJoinBrokerHubState[broker_id]
		if !exist {
			return "not in hub"
		}
		if hub_id != brokerhub_id {
			return "hub id error"
		}
	}
	for _, brokerinfo := range bcm.brokerInfoListInBrokerHub[brokerhub_id] {
		if brokerinfo.BrokerAddr == broker_id {
			remained_balance := new(big.Int).Set(brokerinfo.BrokerBalance)
			profit_in_hub := new(big.Float).Set(brokerinfo.BrokerProfit)
			if bcm.calculateTotalBalance(brokerhub_id).Cmp(remained_balance) == -1 {
				return "fund lock"
			}
			for i := uint64(0); i < uint64(params.ShardNum); i++ {
				if bcm.Broker.BrokerBalance[brokerhub_id][i].Cmp(remained_balance) == 1 {
					bcm.Broker.BrokerBalance[brokerhub_id][i].Sub(
						bcm.Broker.BrokerBalance[brokerhub_id][i],
						remained_balance,
					)
					remained_balance = new(big.Int).SetInt64(0)
					break
				} else {
					remained_balance.Sub(remained_balance, bcm.Broker.BrokerBalance[brokerhub_id][i])
					bcm.Broker.BrokerBalance[brokerhub_id][i] = new(big.Int).SetInt64(0)
				}
			}
			if remained_balance.Cmp(new(big.Int).SetInt64(0)) == 1 {
				log.Panic()
			}
			for _, val := range bcm.Broker.ProfitBalance[broker_id] {
				val.Add(
					val,
					new(big.Float).Quo(profit_in_hub, new(big.Float).SetFloat64(float64(params.ShardNum))),
				)
			}
			break
		}
	}
	bcm.brokerInfoListInBrokerHub[brokerhub_id] = slices.DeleteFunc(
		bcm.brokerInfoListInBrokerHub[brokerhub_id],
		func(x *message.BrokerInfoInBrokerhub) bool {
			return x.BrokerAddr == broker_id
		},
	)

	delete(bcm.brokerJoinBrokerHubState, broker_id)
	return "done"
}

func (bcm *BrokerhubCommitteeMod) calManagementExpanseRatio(epochTxSamples []optimizerPkg.TransactionSample) {
	for _, brokerhub_id := range bcm.BrokerHubAccountList {
		if bcm.hubParams.currentEpoch > bcm.hubParams.endedEpoch {
			continue
		}

		currentFunds := bigIntToFloat64(bcm.calculateTotalBalance(brokerhub_id))
		participationRate := 0.0
		if params.BrokerNum > 0 {
			participationRate = float64(len(bcm.brokerInfoListInBrokerHub[brokerhub_id])) / float64(params.BrokerNum)
		}

		currentEarn := 0.0
		if profit, ok := bcm.brokerhubEpochProfit[brokerhub_id]; ok {
			currentEarn, _ = profit.Float64()
		}

		/*

			// 4. 获取对手的收益（对应最大资金者，或任意对手平均均可，此处用第一个存在的）
			var competitorEarn float64 = 0.0
			for key := range bcm.brokerInfoListInBrokerHub {
				if key != brokerhub_id {
					if profit, ok := bcm.brokerhubEpochProfit[key]; ok {
						competitorEarn, _ = profit.Float64()
					}
					break
				}
			}

			// 5. 执行基于 Go 版本的优化算法
			opt := bcm.feeOptimizers[brokerhub_id]
			iteration := bcm.hubParams.currentEpoch

			opt.Optimize(iteration, myFunds, competitorFunds, currentEarn, competitorEarn)
		*/
		strongestCompetitorFunds, strongestCompetitorEarn := bcm.strongestCompetitorSnapshot(brokerhub_id)
		opt := bcm.feeOptimizers[brokerhub_id]
		if opt == nil {
			continue
		}
		opt.Optimize(optimizerPkg.EpochMetrics{
			Iteration:                bcm.hubParams.currentEpoch,
			ParticipationRate:        participationRate,
			BrokerCount:              len(bcm.brokerInfoListInBrokerHub[brokerhub_id]),
			CurrentFunds:             currentFunds,
			CurrentEarn:              currentEarn,
			StrongestCompetitorFunds: strongestCompetitorFunds,
			StrongestCompetitorEarn:  strongestCompetitorEarn,
			Transactions:             epochTxSamples,
		})
		bcm.recordObservedHubFee(brokerhub_id, opt.FeeRate())
		debugState := opt.DebugState()
		if debugState.Mode == params.FeeOptimizerModePaperMonopoly {
			shockTriggered := debugState.OptimizerPhase == "shock"
			criticalCapUpdated := debugState.HasCriticalMERCap && debugState.CriticalMEREpoch == bcm.hubParams.currentEpoch
			bcm.sl.Slog.Printf(
				"hub %s optimizer=%s phase=%s fee=%.6f upper_bound=%.6f fund_share=%.4f success=%d failure=%d dominance=%d critical_cap=%.6f shock_triggered=%t critical_cap_updated=%t shock_exit_count=%d shock_fund_drop=%.4f strongest_competitor_funds=%.2f strongest_competitor_earn=%.2f",
				brokerhub_id[:5],
				debugState.Mode,
				debugState.OptimizerPhase,
				debugState.CurrentFeeRate,
				debugState.DynamicUpperBound,
				debugState.FundShare,
				debugState.ConsecutiveSuccess,
				debugState.ConsecutiveFailure,
				debugState.DominanceStreak,
				debugState.CriticalMERCap,
				shockTriggered,
				criticalCapUpdated,
				debugState.ShockExitCount,
				debugState.ShockFundDrop,
				debugState.StrongestCompetitorFunds,
				debugState.StrongestCompetitorEarn,
			)
		}
	}
}

func (bcm *BrokerhubCommitteeMod) allocateBrokerhubRevenue(addr string, ssid uint64, fee *big.Float) {
	// if slices.Contains(bcm.BrokerHubAccountList, addr) {
	// 	brokerhub_bios := make(map[string]int)
	// 	total_bios := 0
	// 	for _, hub_id := range bcm.BrokerHubAccountList {
	// 		brokerhub_bios[hub_id] = 1 + len(bcm.brokerInfoListInBrokerHub)
	// 		total_bios += 1 + len(bcm.brokerInfoListInBrokerHub)
	// 	}
	// 	bios := float64(brokerhub_bios[addr]) / float64(total_bios)
	// 	earn := new(big.Float).Mul(fee, new(big.Float).SetFloat64(bios))
	// 	bcm.Broker.ProfitBalance[addr][ssid].Add(bcm.Broker.ProfitBalance[addr][ssid], earn)
	// 	bcm.brokerhubEpochProfit[addr].Add(bcm.brokerhubEpochProfit[addr], earn)
	// 	return
	// }
	// bcm.Broker.ProfitBalance[addr][ssid].Add(bcm.Broker.ProfitBalance[addr][ssid], fee)

	// 如果账户不是BrokerHub，直接按照正常流程的增加余额流程
	if !slices.Contains(bcm.BrokerHubAccountList, addr) {
		bcm.Broker.ProfitBalance[addr][ssid].Add(bcm.Broker.ProfitBalance[addr][ssid], fee)
		// 本轮 B2E 收益增加
		if bcm.brokerEpochProfitInB2E[addr] == nil {
			bcm.brokerEpochProfitInB2E[addr] = big.NewFloat(0)
		}
		bcm.brokerEpochProfitInB2E[addr].Add(bcm.brokerEpochProfitInB2E[addr], fee)
		return
	}

	if bcm.brokerhubEpochGrossRevenue[addr] == nil {
		bcm.brokerhubEpochGrossRevenue[addr] = big.NewFloat(0)
	}
	grossFee := new(big.Float).Set(fee)
	bcm.brokerhubEpochGrossRevenue[addr].Add(bcm.brokerhubEpochGrossRevenue[addr], grossFee)

	// When no external broker is managed by the hub, the hub keeps the whole fee pool.
	if len(bcm.brokerInfoListInBrokerHub[addr]) == 0 {
		bcm.Broker.ProfitBalance[addr][ssid].Add(bcm.Broker.ProfitBalance[addr][ssid], grossFee)
		bcm.brokerhubEpochProfit[addr].Add(bcm.brokerhubEpochProfit[addr], grossFee)
		return
	}

	totalManagedFunds := bcm.totalManagedBrokerFunds(addr)
	if totalManagedFunds <= 0 {
		bcm.Broker.ProfitBalance[addr][ssid].Add(bcm.Broker.ProfitBalance[addr][ssid], grossFee)
		bcm.brokerhubEpochProfit[addr].Add(bcm.brokerhubEpochProfit[addr], grossFee)
		return
	}

	feeRate := bcm.feeOptimizers[addr].FeeRate()
	hubNetRevenue := new(big.Float).Mul(new(big.Float).Set(grossFee), new(big.Float).SetFloat64(feeRate))
	brokersRevenuePool := new(big.Float).Sub(new(big.Float).Set(grossFee), hubNetRevenue)
	totalManagedFundsFloat := new(big.Float).SetFloat64(totalManagedFunds)
	for _, brokerinfo := range bcm.brokerInfoListInBrokerHub[addr] {
		brokerRevenue := new(big.Float).Mul(brokersRevenuePool, new(big.Float).SetInt(brokerinfo.BrokerBalance))
		brokerRevenue.Quo(brokerRevenue, totalManagedFundsFloat)
		brokerinfo.BrokerProfit.Add(brokerinfo.BrokerProfit, brokerRevenue)
	}

	bcm.Broker.ProfitBalance[addr][ssid].Add(bcm.Broker.ProfitBalance[addr][ssid], hubNetRevenue)
	bcm.brokerhubEpochProfit[addr].Add(bcm.brokerhubEpochProfit[addr], hubNetRevenue)
}

func (bcm *BrokerhubCommitteeMod) GetBrokerInfomationInHub(broker_id string) (uint64, float64, string) {
	brokerhub_id, exist := bcm.brokerJoinBrokerHubState[broker_id]
	if !exist {
		return 0, 0, ""
	}

	for _, brokerinfo := range bcm.brokerInfoListInBrokerHub[brokerhub_id] {
		if brokerinfo.BrokerAddr == broker_id {
			fund := brokerinfo.BrokerBalance.Uint64()
			earn, _ := brokerinfo.BrokerProfit.Float64()
			return fund, earn, brokerhub_id
		}
	}
	return 0, 0, ""
}

func (bcm *BrokerhubCommitteeMod) generateRandomTxs() []*core.Transaction {
	if len(bcm.transaction_fee_list) != len(bcm.transaction_value_list) {
		log.Panic()
	}
	size := len(bcm.transaction_fee_list)
	if len(AddressSet) == 0 {
		mu.Lock()
		if len(AddressSet) == 0 {
			AddressSet = make([]string, 20000)
			for i := 0; i < 20000; i++ {
				AddressSet[i] = bcm.randomHexString(40)
			}
		}
		mu.Unlock()
	}
	txs := make([]*core.Transaction, 0)
	for i := 0; i < size; i++ {
		sender := AddressSet[bcm.rng.Intn(20000)]
		recever := AddressSet[bcm.rng.Intn(20000)]

		sid := utils.Addr2Shard(sender)
		UUID := strconv.Itoa(sid) + "-" + uuid.New().String()

		tx := core.NewTransaction(
			sender,
			recever,
			bcm.transaction_value_list[i],
			uint64(123),
			bcm.transaction_fee_list[i],
		)
		tx.UUID = UUID
		txs = append(txs, tx)
	}
	return txs
}

func (bcm *BrokerhubCommitteeMod) MsgSendingControl() {
	bcm.init_brokerhub()
	// 开启csv记录
	os.MkdirAll("./hubres", os.ModePerm)
	bcm.writeDataToCsv(true, 0)
	bcm.sl.Slog.Printf(
		"brokerhub exchange_mode=%s, epoch_limit=%d, fee_optimizer=%s, sim_seed=%d",
		bcm.hubParams.exchangeMode,
		bcm.hubParams.endedEpoch,
		bcm.feeOptimizerMode,
		bcm.simSeed,
	)
	epochLoopDone := make(chan struct{})

	go func() {
		defer close(epochLoopDone)
		for {
			if bcm.reachedEpochLimit() {
				bcm.sl.Slog.Printf("exchange_mode=%s reached epoch limit %d, stop generating new epochs", bcm.hubParams.exchangeMode, bcm.hubParams.endedEpoch)
				return
			}

			bcm.hubParams.currentEpoch++
			bcm.sl.Slog.Printf("epoch: %d", bcm.hubParams.currentEpoch)

			/*
				if bcm.hubParams.currentEpoch > bcm.hubParams.endedEpoch {
					bcm.sl.Slog.Println("达到设定的最大 Epoch，停止生成交易。")
					fmt.Println("Simulation ends gracefully at epoch", bcm.hubParams.currentEpoch-1)

					// 发送全局停止信号给所有共识分片节点
					stopmsg := message.MergeMessage(message.CStop, []byte("graceful shutdown"))
					bcm.sl.Slog.Println("Supervisor: sending stop message to all shards to end PBFT loops")
					for sid := uint64(0); sid < uint64(params.ShardNum); sid++ {
						for nid := uint64(0); nid < uint64(params.NodesInShard); nid++ { // 注意：此处使用 params.NodesInShard
							networks.TcpDial(stopmsg, bcm.IpNodeTable[sid][nid])
						}
					}

					time.Sleep(time.Second * 3) // 等待所有节点保存高度和关闭连接

					// 最后安全退出统筹节点
					os.Exit(0)
					break
				}
			*/

			txs := bcm.generateRandomTxs()

			itx := bcm.dealTxByBroker(txs)

			bcm.txSending(itx)

			time.Sleep(time.Second * 2)

			// 每轮处理完调用 broker 跳槽博弈模型并写入记录
			bcm.broker_behaviour_simulator(true)

		}
	}()

	for {
		select {
		case <-epochLoopDone:
			return
		default:
		}

		time.Sleep(time.Millisecond * 100)

		mytool.Mutex1.Lock()
		if len(mytool.UserRequestB2EQueue) == 0 {
			mytool.Mutex1.Unlock()
			continue
		}

		queueCopy := make([]*core.Transaction, len(mytool.UserRequestB2EQueue))
		copy(queueCopy, mytool.UserRequestB2EQueue)
		mytool.UserRequestB2EQueue = mytool.UserRequestB2EQueue[:0]

		mytool.Mutex1.Unlock()

		//bcm.BrokerModuleLock.Lock()
		itx := bcm.dealTxByBroker2(queueCopy)
		//bcm.BrokerModuleLock.Unlock()
		bcm.txSending(itx)

	}
}
func (bcm *BrokerhubCommitteeMod) HandleBlockInfo(b *message.BlockInfoMsg) {

	// bcm.sl.Slog.Printf("received from shard %d in epoch %d.\n", b.SenderShardID, b.Epoch)
	if b.BlockBodyLength == 0 {
		return
	}
	bcm.markNonEmptyBlockInfo()
	//fmt.Println("HandleBlockInfo.... ", b.BlockBodyLength)

	// add createConfirm
	txs := make([]*core.Transaction, 0)
	txs = append(txs, b.Broker1Txs...)
	txs = append(txs, b.Broker2Txs...)
	bcm.BrokerModuleLock.Lock()
	// when accept ctx1, update all accounts
	bcm.BrokerBalanceLock.Lock()
	//println("block length is ", len(b.ExcutedTxs))
	for _, tx := range b.Broker1Txs {
		brokeraddress, sSid, rSid := tx.Recipient, bcm.fetchModifiedMap(tx.OriginalSender), bcm.fetchModifiedMap(tx.FinalRecipient)

		if !bcm.Broker.IsBroker(brokeraddress) {
			continue
		}

		if bcm.Broker.LockBalance[brokeraddress][rSid].Cmp(tx.Value) < 0 {
			continue
		}
		bcm.Broker.LockBalance[brokeraddress][rSid].Sub(bcm.Broker.LockBalance[brokeraddress][rSid], tx.Value)
		bcm.Broker.BrokerBalance[brokeraddress][sSid].Add(bcm.Broker.BrokerBalance[brokeraddress][sSid], tx.Value)

		fee := new(big.Float).SetInt64(tx.Fee.Int64())

		fee = fee.Mul(fee, bcm.Broker.Brokerage)

		bcm.allocateBrokerhubRevenue(brokeraddress, sSid, fee)

	}
	//bcm.add_result()
	bcm.BrokerBalanceLock.Unlock()
	bcm.BrokerModuleLock.Unlock()
	bcm.createConfirm(txs)
}

func (bcm *BrokerhubCommitteeMod) calculateBrokerhubRank(brokerhub_id string, mer float64) int {
	broker_num := len(bcm.brokerInfoListInBrokerHub[brokerhub_id])
	if broker_num == 20 {
		return 1
	}
	if broker_num == 0 && math.Abs(mer-bcm.feeOptimizers[brokerhub_id].MinFee()) < 0.01 {
		return 2
	}
	if broker_num > 10 {
		return 1
	}
	if broker_num <= 10 && broker_num > 6 {
		return 8 - broker_num/2
	}
	if broker_num <= 6 {
		return 13 - broker_num
	}
	return 2
}

func (bcm *BrokerhubCommitteeMod) writeDataToCsv(is_first bool, sampledCrossTxs int) {
	for index, hub_id := range bcm.BrokerHubAccountList {
		openFlags := os.O_APPEND | os.O_CREATE | os.O_WRONLY
		if is_first {
			openFlags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		}
		file, err := os.OpenFile("./hubres/hub"+strconv.Itoa(index)+".csv", openFlags, 0644)
		if err != nil {
			log.Panic()
		}
		defer file.Close()
		writer := csv.NewWriter(file)
		if is_first {
			err = writer.Write([]string{
				"epoch",
				"revenue",
				"broker_num",
				"mer",
				"fund",
				"Rank",
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
			})
		} else {
			revenue, _ := bcm.brokerhubEpochProfit[hub_id].Float64()
			opt := bcm.feeOptimizers[hub_id]
			currentFeeRate := 0.0
			minFeeRate := 0.0
			currentFund := bigIntToFloat64(bcm.calculateTotalBalance(hub_id))
			currentInvestment := 0.0
			predictedInvestment := 0.0
			fundShare := 0.0
			dominanceStreak := 0
			criticalMERCap := 0.0
			shockExitCount := 0
			shockFundDrop := 0.0
			optimizerPhase := ""
			if opt != nil {
				debugState := opt.DebugState()
				currentFeeRate = opt.FeeRate()
				minFeeRate = opt.MinFee()
				currentInvestment = currentFund
				predictedInvestment = debugState.LastPredictedInvestment
				fundShare = debugState.FundShare
				dominanceStreak = debugState.DominanceStreak
				criticalMERCap = debugState.CriticalMERCap
				shockExitCount = debugState.ShockExitCount
				shockFundDrop = debugState.ShockFundDrop
				optimizerPhase = debugState.OptimizerPhase
			}
			participationRate := 0.0
			if params.BrokerNum > 0 {
				participationRate = float64(len(bcm.brokerInfoListInBrokerHub[hub_id])) / float64(params.BrokerNum)
			}
			err = writer.Write([]string{
				strconv.Itoa(bcm.hubParams.currentEpoch),
				strconv.FormatFloat(revenue, 'f', 6, 64),
				strconv.Itoa(len(bcm.brokerInfoListInBrokerHub[hub_id])),
				strconv.FormatFloat(currentFeeRate, 'f', 6, 64),
				strconv.FormatUint(bcm.calculateTotalBalance(hub_id).Uint64(), 10),
				strconv.Itoa(bcm.calculateBrokerhubRank(hub_id, math.Max(currentFeeRate, minFeeRate))),
				strconv.FormatFloat(participationRate, 'f', 6, 64),
				strconv.FormatFloat(currentInvestment, 'f', 6, 64),
				strconv.Itoa(sampledCrossTxs),
				strconv.FormatFloat(predictedInvestment, 'f', 6, 64),
				strconv.FormatFloat(fundShare, 'f', 6, 64),
				strconv.Itoa(dominanceStreak),
				strconv.FormatFloat(criticalMERCap, 'f', 6, 64),
				strconv.Itoa(shockExitCount),
				strconv.FormatFloat(shockFundDrop, 'f', 6, 64),
				optimizerPhase,
			})
		}
		if err != nil {
			log.Panic()
		}
		writer.Flush()
	}
}

func (bcm *BrokerhubCommitteeMod) handleBrokerB2EBalance() (temp_map map[string]map[uint64]*big.Int) {
	temp_map = make(map[string]map[uint64]*big.Int)
	for key, val := range bcm.Broker.BrokerBalance {
		if _, exist := bcm.brokerJoinBrokerHubState[key]; !exist {
			temp_map[key] = val
		}
	}
	for _, val := range bcm.BrokerHubAccountList {
		temp_map[val] = bcm.Broker.BrokerBalance[val]
	}
	return temp_map
}

func (bcm *BrokerhubCommitteeMod) init_broker_revenue_in_epoch() {
	for _, broker_id := range bcm.Broker.BrokerAddress {
		if slices.Contains(bcm.BrokerHubAccountList, broker_id) {
			continue
		}
		if _, is_in_hub := bcm.brokerJoinBrokerHubState[broker_id]; !is_in_hub {
			bcm.brokerEpochProfitInB2E[broker_id] = big.NewFloat(0)
		}
	}
	for _, brokerhub_id := range bcm.BrokerHubAccountList {
		bcm.brokerhubEpochProfit[brokerhub_id] = big.NewFloat(0)
		bcm.brokerhubEpochGrossRevenue[brokerhub_id] = big.NewFloat(0)
	}
}

func (bcm *BrokerhubCommitteeMod) commitCurrentB2EProfits() {
	if bcm.brokerB2EProfitHistory == nil {
		bcm.brokerB2EProfitHistory = make(map[string][]float64)
	}
	for _, brokerID := range bcm.Broker.BrokerAddress {
		if slices.Contains(bcm.BrokerHubAccountList, brokerID) {
			continue
		}
		profit, ok := bcm.brokerEpochProfitInB2E[brokerID]
		if !ok || profit == nil {
			continue
		}
		value, _ := profit.Float64()
		history := append(bcm.brokerB2EProfitHistory[brokerID], value)
		if len(history) > 6 {
			history = history[len(history)-6:]
		}
		bcm.brokerB2EProfitHistory[brokerID] = history
	}
}

func trailingMean(values []float64, window int) float64 {
	if len(values) == 0 {
		return 0
	}
	if window <= 0 || len(values) < window {
		window = len(values)
	}
	return optimizerPkg.Mean(values[len(values)-window:])
}

func (bcm *BrokerhubCommitteeMod) currentB2EProfitFloat64(brokerID string) (float64, bool) {
	profit, ok := bcm.brokerEpochProfitInB2E[brokerID]
	if !ok || profit == nil {
		return 0, false
	}
	value, _ := profit.Float64()
	return value, true
}

func (bcm *BrokerhubCommitteeMod) brokerTrailingDirectUtility(brokerID string, includeCurrent bool) (float64, bool) {
	history := append([]float64(nil), bcm.brokerB2EProfitHistory[brokerID]...)
	if includeCurrent {
		if currentProfit, ok := bcm.currentB2EProfitFloat64(brokerID); ok {
			history = append(history, currentProfit)
		}
	}
	if len(history) == 0 {
		return 0, false
	}
	return trailingMean(history, 3), true
}

func (bcm *BrokerhubCommitteeMod) hubGrossRevenueRate(hubID string) float64 {
	grossRevenue := 0.0
	if value, ok := bcm.brokerhubEpochGrossRevenue[hubID]; ok && value != nil {
		grossRevenue, _ = value.Float64()
	}
	currentFunds := bcm.brokerFundsFloat64(hubID)
	if currentFunds <= 0 {
		return 0
	}
	ownRate := math.Max(grossRevenue, 0) / currentFunds
	marketRate := bcm.marketGrossRevenueRate()
	if marketRate <= 0 {
		return ownRate
	}
	managedFunds := bcm.totalManagedBrokerFunds(hubID)
	managedShare := clampFloat64(managedFunds/math.Max(currentFunds, 1), 0, 1)
	confidence := 0.2 + 0.8*managedShare
	return confidence*ownRate + (1-confidence)*marketRate
}

func (bcm *BrokerhubCommitteeMod) marketGrossRevenueRate() float64 {
	totalGrossRevenue := 0.0
	totalFunds := 0.0
	for _, hubID := range bcm.BrokerHubAccountList {
		if value, ok := bcm.brokerhubEpochGrossRevenue[hubID]; ok && value != nil {
			grossRevenue, _ := value.Float64()
			totalGrossRevenue += math.Max(grossRevenue, 0)
		}
		totalFunds += bcm.brokerFundsFloat64(hubID)
	}
	if totalFunds <= 0 {
		return 0
	}
	return totalGrossRevenue / totalFunds
}

func (bcm *BrokerhubCommitteeMod) brokerFundsFloat64(brokerID string) float64 {
	return bigIntToFloat64(bcm.calculateTotalBalance(brokerID))
}

func (bcm *BrokerhubCommitteeMod) totalManagedBrokerFunds(hubID string) float64 {
	total := 0.0
	for _, brokerinfo := range bcm.brokerInfoListInBrokerHub[hubID] {
		if brokerinfo == nil || brokerinfo.BrokerBalance == nil {
			continue
		}
		total += bigIntToFloat64(brokerinfo.BrokerBalance)
	}
	return total
}

func (bcm *BrokerhubCommitteeMod) strongestCompetitorSnapshot(hubID string) (float64, float64) {
	_, strongestCompetitorFunds, strongestCompetitorEarn := bcm.strongestCompetitorDetails(hubID)
	return strongestCompetitorFunds, strongestCompetitorEarn
}

func (bcm *BrokerhubCommitteeMod) refreshDirectUtilityEstimates() {
	type candidateSample struct {
		id     string
		funds  float64
		profit float64
	}

	candidates := make([]candidateSample, 0)
	totalFunds := 0.0
	totalProfit := 0.0
	for _, otherBrokerID := range bcm.Broker.BrokerAddress {
		if slices.Contains(bcm.BrokerHubAccountList, otherBrokerID) {
			continue
		}
		if _, joined := bcm.brokerJoinBrokerHubState[otherBrokerID]; joined {
			continue
		}
		profitFloat, ok := bcm.brokerTrailingDirectUtility(otherBrokerID, true)
		if !ok {
			continue
		}
		funds := bcm.brokerFundsFloat64(otherBrokerID)
		if funds <= 0 {
			continue
		}
		candidates = append(candidates, candidateSample{id: otherBrokerID, funds: funds, profit: profitFloat})
		totalFunds += funds
		totalProfit += profitFloat
	}

	for _, brokerID := range bcm.Broker.BrokerAddress {
		if slices.Contains(bcm.BrokerHubAccountList, brokerID) {
			continue
		}
		ownFunds := bcm.brokerFundsFloat64(brokerID)
		if ownFunds <= 0 {
			bcm.brokerDirectUtilityEst[brokerID] = 0
			continue
		}
		if _, joined := bcm.brokerJoinBrokerHubState[brokerID]; !joined {
			if directUtility, ok := bcm.brokerTrailingDirectUtility(brokerID, true); ok {
				bcm.brokerDirectUtilityEst[brokerID] = directUtility
				continue
			}
		}
		if directUtility, ok := bcm.brokerTrailingDirectUtility(brokerID, false); ok && len(candidates) == 0 {
			bcm.brokerDirectUtilityEst[brokerID] = directUtility
			continue
		}
		if len(candidates) >= 2 {
			sorted := append([]candidateSample(nil), candidates...)
			slices.SortFunc(sorted, func(left, right candidateSample) int {
				leftDiff := math.Abs(left.funds - ownFunds)
				rightDiff := math.Abs(right.funds - ownFunds)
				switch {
				case leftDiff < rightDiff:
					return -1
				case leftDiff > rightDiff:
					return 1
				default:
					return 0
				}
			})
			first := sorted[0]
			second := sorted[1]
			w1 := 1.0 / math.Max(math.Abs(ownFunds-first.funds), 1e-9)
			w2 := 1.0 / math.Max(math.Abs(ownFunds-second.funds), 1e-9)
			bcm.brokerDirectUtilityEst[brokerID] = (w1*first.profit + w2*second.profit) / (w1 + w2)
			continue
		}
		if totalFunds > 0 {
			bcm.brokerDirectUtilityEst[brokerID] = ownFunds * totalProfit / totalFunds
			continue
		}
		if _, exists := bcm.brokerDirectUtilityEst[brokerID]; !exists {
			bcm.brokerDirectUtilityEst[brokerID] = 0
		}
	}
}

func (bcm *BrokerhubCommitteeMod) estimateDirectUtility(brokerID string) float64 {
	if _, joined := bcm.brokerJoinBrokerHubState[brokerID]; !joined {
		if directUtility, ok := bcm.brokerTrailingDirectUtility(brokerID, true); ok {
			return directUtility
		}
	}
	if directUtility, ok := bcm.brokerDirectUtilityEst[brokerID]; ok {
		return directUtility
	}

	ownFunds := bcm.brokerFundsFloat64(brokerID)
	if ownFunds <= 0 {
		return 0
	}

	if len(bcm.brokerDirectUtilityEst) == 0 {
		return 0
	}

	totalFunds := 0.0
	totalUtility := 0.0
	for brokerID, utility := range bcm.brokerDirectUtilityEst {
		funds := bcm.brokerFundsFloat64(brokerID)
		if funds <= 0 {
			continue
		}
		totalFunds += funds
		totalUtility += utility
	}
	if totalFunds > 0 {
		return ownFunds * totalUtility / totalFunds
	}
	return 0
}

func clampFloat64(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

const (
	competitionPendingModeHub = "hub"
	competitionPendingModeB2E = "b2e"

	competitionPhaseCompetition = "competition"
	competitionPhaseShock       = "shock"
)

func utilityTieTolerance(left, right float64) float64 {
	return 1e-6
}

func (bcm *BrokerhubCommitteeMod) recordObservedHubFee(hubID string, feeRate float64) {
	if bcm.hubObservedFeeHistory == nil {
		bcm.hubObservedFeeHistory = make(map[string][]float64)
	}
	history := append(bcm.hubObservedFeeHistory[hubID], feeRate)
	if len(history) > bcm.competitionTuning.FeeHistoryWindow {
		history = append([]float64(nil), history[len(history)-bcm.competitionTuning.FeeHistoryWindow:]...)
	}
	bcm.hubObservedFeeHistory[hubID] = history
}

func (bcm *BrokerhubCommitteeMod) hubOptimizerPhase(hubID string) string {
	opt := bcm.feeOptimizers[hubID]
	if opt == nil {
		return ""
	}
	return opt.DebugState().OptimizerPhase
}

func (bcm *BrokerhubCommitteeMod) strongestCompetitorDetails(hubID string) (string, float64, float64) {
	strongestCompetitorID := ""
	strongestCompetitorFunds := 0.0
	strongestCompetitorEarn := 0.0
	for _, competitorID := range bcm.BrokerHubAccountList {
		if competitorID == hubID {
			continue
		}
		competitorFunds := bcm.brokerFundsFloat64(competitorID)
		if competitorFunds < strongestCompetitorFunds {
			continue
		}
		strongestCompetitorID = competitorID
		strongestCompetitorFunds = competitorFunds
		if profit, ok := bcm.brokerhubEpochProfit[competitorID]; ok && profit != nil {
			strongestCompetitorEarn, _ = profit.Float64()
		} else {
			strongestCompetitorEarn = 0
		}
	}
	return strongestCompetitorID, strongestCompetitorFunds, strongestCompetitorEarn
}

func (bcm *BrokerhubCommitteeMod) strongestCompetitorFeeRate(hubID string, currentFeeRate float64) float64 {
	competitorID, _, _ := bcm.strongestCompetitorDetails(hubID)
	if competitorID == "" {
		return currentFeeRate
	}
	opt := bcm.feeOptimizers[competitorID]
	if opt == nil {
		return currentFeeRate
	}
	return opt.FeeRate()
}

func (bcm *BrokerhubCommitteeMod) competitionSwitchScales() map[string]float64 {
	type brokerRank struct {
		id    string
		funds float64
	}

	brokerRanks := make([]brokerRank, 0, len(bcm.Broker.BrokerAddress))
	for _, brokerID := range bcm.Broker.BrokerAddress {
		if slices.Contains(bcm.BrokerHubAccountList, brokerID) {
			continue
		}
		brokerRanks = append(brokerRanks, brokerRank{
			id:    brokerID,
			funds: bcm.brokerFundsFloat64(brokerID),
		})
	}
	sort.Slice(brokerRanks, func(i, j int) bool {
		if math.Abs(brokerRanks[i].funds-brokerRanks[j].funds) <= 1e-9 {
			return brokerRanks[i].id < brokerRanks[j].id
		}
		return brokerRanks[i].funds < brokerRanks[j].funds
	})
	switchScales := make(map[string]float64, len(brokerRanks))
	if len(brokerRanks) == 0 {
		return switchScales
	}
	for idx, rankedBroker := range brokerRanks {
		normalizedRank := 0.0
		if len(brokerRanks) > 1 {
			normalizedRank = float64(idx) / float64(len(brokerRanks)-1)
		}
		switchScales[rankedBroker.id] = 1 + bcm.competitionTuning.SwitchScaleCap*normalizedRank
	}
	return switchScales
}

func (bcm *BrokerhubCommitteeMod) competitionMargins(bestHubUtility, currentHubUtility, switchScale float64) (float64, float64, float64) {
	maxAbsUtility := math.Max(1.0, math.Max(math.Abs(bestHubUtility), math.Abs(currentHubUtility)))
	joinMargin := math.Max(bcm.competitionTuning.JoinMarginBase, bcm.competitionTuning.JoinSwitchUtilityScale*maxAbsUtility)
	switchMargin := math.Max(bcm.competitionTuning.SwitchMarginBase, bcm.competitionTuning.JoinSwitchUtilityScale*maxAbsUtility)
	exitMargin := math.Max(bcm.competitionTuning.ExitMarginBase, bcm.competitionTuning.ExitUtilityScale*maxAbsUtility)
	return joinMargin * switchScale, switchMargin * switchScale, exitMargin * switchScale
}

func (bcm *BrokerhubCommitteeMod) clearCompetitionDecisionState(brokerID string) {
	if bcm.brokerCompetitionState == nil {
		return
	}
	delete(bcm.brokerCompetitionState, brokerID)
}

func (bcm *BrokerhubCommitteeMod) observeCompetitionDecision(brokerID, target, mode string) int {
	if bcm.brokerCompetitionState == nil {
		bcm.brokerCompetitionState = make(map[string]*competitionDecisionState)
	}
	state, ok := bcm.brokerCompetitionState[brokerID]
	if !ok || state.PendingTarget != target || state.PendingMode != mode {
		bcm.brokerCompetitionState[brokerID] = &competitionDecisionState{
			PendingTarget:     target,
			PendingMode:       mode,
			ConsecutiveEpochs: 1,
		}
		return 1
	}
	state.ConsecutiveEpochs++
	return state.ConsecutiveEpochs
}

func (bcm *BrokerhubCommitteeMod) estimateHubUtility(brokerID, brokerhubID string, directUtility float64) float64 {
	brokerFunds := bcm.brokerFundsFloat64(brokerID)
	if brokerFunds <= 0 {
		return math.Inf(-1)
	}

	opt := bcm.feeOptimizers[brokerhubID]
	if opt == nil {
		return math.Inf(-1)
	}

	currentHubFunds := bcm.brokerFundsFloat64(brokerhubID)
	if currentHubFunds <= 0 {
		return -directUtility
	}

	projectedPoolFunds := currentHubFunds
	if currentHubID, joined := bcm.brokerJoinBrokerHubState[brokerID]; !joined || currentHubID != brokerhubID {
		projectedPoolFunds += brokerFunds
	}
	projectedPoolFunds = math.Max(projectedPoolFunds, brokerFunds)
	projectedBrokerShare := brokerFunds / math.Max(projectedPoolFunds, 1e-9)

	baseRevenueRate := math.Max(
		bcm.hubGrossRevenueRate(brokerhubID),
		bcm.marketGrossRevenueRate(),
	)
	if baseRevenueRate <= 0 {
		return -directUtility
	}

	strongestCompetitorFunds, _ := bcm.strongestCompetitorSnapshot(brokerhubID)
	scaleBoost := 1 + 0.35*math.Log1p(projectedPoolFunds/math.Max(currentHubFunds, 1))
	rankBoost := 1 + 0.25*math.Max(
		0,
		(projectedPoolFunds-strongestCompetitorFunds)/math.Max(strongestCompetitorFunds, 1),
	)
	projectedBoost := clampFloat64(scaleBoost*rankBoost, 1.0, 1.8)
	projectedGrossRevenue := projectedPoolFunds * baseRevenueRate * projectedBoost
	if opt.DebugState().OptimizerPhase == competitionPhaseCompetition {
		currentFeeRate := opt.FeeRate()
		previousFeeRate := currentFeeRate
		if history := bcm.hubObservedFeeHistory[brokerhubID]; len(history) >= 2 {
			previousFeeRate = history[len(history)-2]
		}
		recentFeeCut := math.Max(0, previousFeeRate-currentFeeRate)
		strongestCompetitorFee := bcm.strongestCompetitorFeeRate(brokerhubID, currentFeeRate)
		feeGap := math.Max(0, strongestCompetitorFee-currentFeeRate)
		competitionAttractionBoost := clampFloat64(
			1+bcm.competitionTuning.RecentFeeCutWeight*recentFeeCut+bcm.competitionTuning.FeeGapWeight*feeGap,
			1.0,
			bcm.competitionTuning.AttractionBoostCap,
		)
		projectedGrossRevenue *= competitionAttractionBoost
	}
	expectedPayout := (1 - opt.FeeRate()) * projectedBrokerShare * projectedGrossRevenue
	return expectedPayout - directUtility
}

func (bcm *BrokerhubCommitteeMod) broker_behaviour_simulator(should_simulate bool) {
	bcm.BrokerBalanceLock.Lock()
	if !should_simulate {
		bcm.writeDataToCsv(false, 0)
		bcm.init_broker_revenue_in_epoch()
		bcm.BrokerBalanceLock.Unlock()
		return
	}
	epochTxSamples := bcm.snapshotEpochCrossTxSamples()
	committedBlockInfos := bcm.snapshotBlockInfoProgress()
	if len(epochTxSamples) > 0 && committedBlockInfos == 0 {
		bcm.sl.Slog.Printf(
			"warning: epoch %d sampled %d cross-shard txs but observed 0 committed non-empty block infos; PBFT may be stalled or replicas may have started late",
			bcm.hubParams.currentEpoch,
			len(epochTxSamples),
		)
	}
	for key, val := range bcm.brokerInfoListInBrokerHub {
		bcm.sl.Slog.Printf("hub %s has % d brokers", key[:5], len(val))
	}
	bcm.calManagementExpanseRatio(epochTxSamples)
	bcm.writeDataToCsv(false, len(epochTxSamples))
	bcm.refreshDirectUtilityEstimates()
	brokerSwitchScales := bcm.competitionSwitchScales()
	broker_decision_map := make(map[string]string)
	for _, broker_id := range bcm.Broker.BrokerAddress {
		if slices.Contains(bcm.BrokerHubAccountList, broker_id) {
			continue
		}
		if bcm.brokerEpochProfitInB2E[broker_id] == nil {
			bcm.brokerEpochProfitInB2E[broker_id] = big.NewFloat(0)
			continue
		}
		broker_joined_hub_id, broker_is_in_hub := bcm.brokerJoinBrokerHubState[broker_id]
		directUtility := bcm.estimateDirectUtility(broker_id)
		bestHubID := ""
		bestHubUtility := math.Inf(-1)
		currentHubUtility := math.Inf(-1)
		bestHubTie := false
		for _, brokerhub_id := range bcm.BrokerHubAccountList {
			hubUtility := bcm.estimateHubUtility(
				broker_id,
				brokerhub_id,
				directUtility,
			)
			tieTolerance := utilityTieTolerance(hubUtility, bestHubUtility)
			if hubUtility > bestHubUtility+tieTolerance {
				bestHubID = brokerhub_id
				bestHubUtility = hubUtility
				bestHubTie = false
			} else if math.Abs(hubUtility-bestHubUtility) <= tieTolerance {
				bestHubTie = true
			}
			if broker_is_in_hub && brokerhub_id == broker_joined_hub_id {
				currentHubUtility = hubUtility
			}
		}
		switchScale := brokerSwitchScales[broker_id]
		if switchScale == 0 {
			switchScale = 1
		}
		currentUtilityForMargin := 0.0
		if !math.IsInf(currentHubUtility, 0) && !math.IsNaN(currentHubUtility) {
			currentUtilityForMargin = currentHubUtility
		}
		joinMargin, switchMargin, exitMargin := bcm.competitionMargins(bestHubUtility, currentUtilityForMargin, switchScale)
		switch {
		case bestHubTie && (!broker_is_in_hub || bestHubUtility > 1e-6):
			bcm.clearCompetitionDecisionState(broker_id)
			if broker_is_in_hub {
				broker_decision_map[broker_id] = broker_joined_hub_id
			} else {
				broker_decision_map[broker_id] = competitionPendingModeB2E
			}
		case broker_is_in_hub && bestHubUtility > 1e-6 && math.Abs(bestHubUtility-currentHubUtility) <= utilityTieTolerance(bestHubUtility, currentHubUtility):
			bcm.clearCompetitionDecisionState(broker_id)
			broker_decision_map[broker_id] = broker_joined_hub_id
		case !broker_is_in_hub:
			bestPhase := bcm.hubOptimizerPhase(bestHubID)
			joinThreshold := joinMargin + bcm.competitionTuning.IncumbentRetentionMargin
			if bestHubUtility <= 1e-6 {
				bcm.clearCompetitionDecisionState(broker_id)
				broker_decision_map[broker_id] = competitionPendingModeB2E
				continue
			}
			if bestPhase != competitionPhaseCompetition {
				bcm.clearCompetitionDecisionState(broker_id)
				broker_decision_map[broker_id] = bestHubID
				continue
			}
			if bestHubUtility < joinThreshold {
				bcm.clearCompetitionDecisionState(broker_id)
				broker_decision_map[broker_id] = competitionPendingModeB2E
				continue
			}
			if bcm.observeCompetitionDecision(broker_id, bestHubID, competitionPendingModeHub) >= bcm.competitionTuning.JoinConfirmEpochs {
				bcm.clearCompetitionDecisionState(broker_id)
				broker_decision_map[broker_id] = bestHubID
				continue
			}
			broker_decision_map[broker_id] = competitionPendingModeB2E
		default:
			currentPhase := bcm.hubOptimizerPhase(broker_joined_hub_id)
			bestPhase := bcm.hubOptimizerPhase(bestHubID)
			if currentPhase != competitionPhaseCompetition || bestPhase == competitionPhaseShock {
				bcm.clearCompetitionDecisionState(broker_id)
				if bestHubUtility <= 1e-6 {
					broker_decision_map[broker_id] = competitionPendingModeB2E
				} else {
					broker_decision_map[broker_id] = bestHubID
				}
				continue
			}
			if bestHubID != "" && bestHubID != broker_joined_hub_id && bestPhase != competitionPhaseCompetition {
				bcm.clearCompetitionDecisionState(broker_id)
				broker_decision_map[broker_id] = bestHubID
				continue
			}
			switchThreshold := switchMargin + bcm.competitionTuning.IncumbentRetentionMargin
			exitThreshold := exitMargin + bcm.competitionTuning.IncumbentRetentionMargin
			if bestHubID != "" && bestHubID != broker_joined_hub_id && bestHubUtility-currentHubUtility >= switchThreshold {
				if bcm.observeCompetitionDecision(broker_id, bestHubID, competitionPendingModeHub) >= bcm.competitionTuning.SwitchConfirmEpochs {
					bcm.clearCompetitionDecisionState(broker_id)
					broker_decision_map[broker_id] = bestHubID
					continue
				}
				broker_decision_map[broker_id] = broker_joined_hub_id
				continue
			}
			if -currentHubUtility >= exitThreshold {
				if bcm.observeCompetitionDecision(broker_id, competitionPendingModeB2E, competitionPendingModeB2E) >= bcm.competitionTuning.ExitConfirmEpochs {
					bcm.clearCompetitionDecisionState(broker_id)
					broker_decision_map[broker_id] = competitionPendingModeB2E
					continue
				}
				broker_decision_map[broker_id] = broker_joined_hub_id
				continue
			}
			bcm.clearCompetitionDecisionState(broker_id)
			broker_decision_map[broker_id] = broker_joined_hub_id
		}
	}
	bcm.BrokerBalanceLock.Unlock()

	for broker_id, decision_hub_id := range broker_decision_map {
		broker_joined_hub_id, broker_is_in_hub := bcm.brokerJoinBrokerHubState[broker_id]
		if decision_hub_id == "b2e" && broker_is_in_hub {
			res := bcm.ExitingBrokerHub(broker_id, broker_joined_hub_id)
			if res != "done" && res != "fund lock" {
				log.Panic()
			}
			if res == "done" {
				bcm.sl.Slog.Printf("broker %s exit brokerhub %s", broker_id[:5], broker_joined_hub_id[:5])
			}
		} else if decision_hub_id != "b2e" && !broker_is_in_hub {
			if bcm.JoiningToBrokerhub(broker_id, decision_hub_id) != "done" {
				log.Panic()
			}
			bcm.sl.Slog.Printf("broker %s join brokerhub %s", broker_id[:5], decision_hub_id[:5])
		} else if decision_hub_id != "b2e" && broker_is_in_hub && broker_joined_hub_id != decision_hub_id {
			res := bcm.ExitingBrokerHub(broker_id, broker_joined_hub_id)
			if res != "done" && res != "fund lock" {
				log.Panic()
			}
			if res == "done" {
				if bcm.JoiningToBrokerhub(broker_id, decision_hub_id) != "done" {
					log.Panic()
				}
				bcm.sl.Slog.Printf("broker %s jump to brokerhub %s", broker_id[:5], broker_joined_hub_id[:5])
			}
		}
	}
	bcm.commitCurrentB2EProfits()
	bcm.init_broker_revenue_in_epoch()
}

func (bcm *BrokerhubCommitteeMod) createConfirm(txs []*core.Transaction) {
	confirm1s := make([]*message.Mag1Confirm, 0)
	confirm2s := make([]*message.Mag2Confirm, 0)
	bcm.BrokerModuleLock.Lock()
	for _, tx := range txs {
		if confirm1, ok := bcm.brokerConfirm1Pool[string(tx.TxHash)]; ok {
			confirm1s = append(confirm1s, confirm1)
		}
		if confirm2, ok := bcm.brokerConfirm2Pool[string(tx.TxHash)]; ok {
			confirm2s = append(confirm2s, confirm2)
		}
	}
	bcm.BrokerModuleLock.Unlock()

	if len(confirm1s) != 0 {
		bcm.handleTx1ConfirmMag(confirm1s)
	}

	if len(confirm2s) != 0 {
		bcm.handleTx2ConfirmMag(confirm2s)
	}
}

func (bcm *BrokerhubCommitteeMod) dealTxByBroker(txs []*core.Transaction) (itxs []*core.Transaction) {
	bcm.BrokerBalanceLock.Lock()
	fmt.Println("dealTxByBroker:", len(txs))
	itxs = make([]*core.Transaction, 0)
	brokerRawMegs := make([]*message.BrokerRawMeg, 0)
	brokerRawMegs = append(brokerRawMegs, bcm.restBrokerRawMegPool...)
	bcm.restBrokerRawMegPool = make([]*message.BrokerRawMeg, 0)

	//println("0brokerSize ", len(brokerRawMegs))
	for _, tx := range txs {

		tx.Recipient = FormatStringToLength(tx.Recipient, 40)
		if tx.Recipient == "error" {
			continue
		}

		tx.Sender = FormatStringToLength(tx.Sender, 40)
		if tx.Sender == "error" {
			continue
		}

		if tx.Recipient == tx.Sender {
			continue
		}

		rSid := bcm.fetchModifiedMap(tx.Recipient)
		sSid := bcm.fetchModifiedMap(tx.Sender)

		if rSid != sSid {
			brokerRawMeg := &message.BrokerRawMeg{
				Tx:     tx,
				Broker: bcm.Broker.BrokerAddress[0],
			}
			brokerRawMegs = append(brokerRawMegs, brokerRawMeg)
		} else {
			if bcm.Broker.IsBroker(tx.Recipient) || bcm.Broker.IsBroker(tx.Sender) {
				tx.HasBroker = true
				tx.SenderIsBroker = bcm.Broker.IsBroker(tx.Sender)
			}
			itxs = append(itxs, tx)
		}
	}

	bcm.appendEpochCrossTxSamples(brokerRawMegs)
	if len(brokerRawMegs) > 1000 {
		brokerRawMegs = brokerRawMegs[:1000]
	}
	now := time.Now()
	alloctedBrokerRawMegs, restBrokerRawMeg := Broker2Earn.B2E(brokerRawMegs, bcm.handleBrokerB2EBalance())
	println("b2e consume time(millsec.) ", time.Since(now).Milliseconds())
	bcm.restBrokerRawMegPool = append(bcm.restBrokerRawMegPool, restBrokerRawMeg...)

	allocatedTxs := bcm.GenerateAllocatedTx(alloctedBrokerRawMegs)
	if len(alloctedBrokerRawMegs) != 0 {
		bcm.handleAllocatedTx(allocatedTxs)
		bcm.lockToken(alloctedBrokerRawMegs)
		bcm.BrokerBalanceLock.Unlock()
		bcm.handleBrokerRawMag(alloctedBrokerRawMegs)
	} else {
		bcm.BrokerBalanceLock.Unlock()
	}
	return itxs
}
func (bcm *BrokerhubCommitteeMod) dealTxByBroker2(txs []*core.Transaction) (itxs []*core.Transaction) {
	bcm.BrokerBalanceLock.Lock()
	fmt.Println("dealTxByBroker:", len(txs))
	itxs = make([]*core.Transaction, 0)
	brokerRawMegs := make([]*message.BrokerRawMeg, 0)
	brokerRawMegs = append(brokerRawMegs, bcm.restBrokerRawMegPool2...)
	bcm.restBrokerRawMegPool2 = make([]*message.BrokerRawMeg, 0)

	//println("0brokerSize ", len(brokerRawMegs))
	for _, tx := range txs {

		tx.Recipient = FormatStringToLength(tx.Recipient, 40)
		if tx.Recipient == "error" {
			continue
		}

		tx.Sender = FormatStringToLength(tx.Sender, 40)
		if tx.Sender == "error" {
			continue
		}

		if tx.Recipient == tx.Sender {
			continue
		}

		rSid := bcm.fetchModifiedMap(tx.Recipient)
		sSid := bcm.fetchModifiedMap(tx.Sender)
		if rSid != sSid {
			brokerRawMeg := &message.BrokerRawMeg{
				Tx:     tx,
				Broker: bcm.Broker.BrokerAddress[0],
			}
			brokerRawMegs = append(brokerRawMegs, brokerRawMeg)
		} else {
			if bcm.Broker.IsBroker(tx.Recipient) || bcm.Broker.IsBroker(tx.Sender) {
				tx.HasBroker = true
				tx.SenderIsBroker = bcm.Broker.IsBroker(tx.Sender)
			}
			itxs = append(itxs, tx)
		}
	}

	bcm.appendEpochCrossTxSamples(brokerRawMegs)
	now := time.Now()
	alloctedBrokerRawMegs, restBrokerRawMeg := Broker2Earn.B2E(brokerRawMegs, bcm.handleBrokerB2EBalance())
	println("b2e consume time(millsec.) ", time.Since(now).Milliseconds())
	bcm.restBrokerRawMegPool2 = append(bcm.restBrokerRawMegPool2, restBrokerRawMeg...)

	allocatedTxs := bcm.GenerateAllocatedTx(alloctedBrokerRawMegs)
	if len(alloctedBrokerRawMegs) != 0 {
		bcm.handleAllocatedTx(allocatedTxs)
		bcm.lockToken(alloctedBrokerRawMegs)
		bcm.BrokerBalanceLock.Unlock()
		bcm.handleBrokerRawMag(alloctedBrokerRawMegs)
	} else {
		bcm.BrokerBalanceLock.Unlock()
	}
	return itxs
}

func (bcm *BrokerhubCommitteeMod) lockToken(alloctedBrokerRawMegs []*message.BrokerRawMeg) {
	//bcm.BrokerBalanceLock.Lock()

	for _, brokerRawMeg := range alloctedBrokerRawMegs {
		tx := brokerRawMeg.Tx
		brokerAddress := brokerRawMeg.Broker
		rSid := bcm.fetchModifiedMap(tx.Recipient)

		if !bcm.Broker.IsBroker(brokerAddress) {
			continue
		}

		bcm.Broker.LockBalance[brokerAddress][rSid].Add(bcm.Broker.LockBalance[brokerAddress][rSid], tx.Value)
		bcm.Broker.BrokerBalance[brokerAddress][rSid].Sub(bcm.Broker.BrokerBalance[brokerAddress][rSid], tx.Value)
	}

	//bcm.BrokerBalanceLock.Unlock()
}
func (bcm *BrokerhubCommitteeMod) handleAllocatedTx(alloctedTx map[uint64][]*core.Transaction) {

	//bcm.BrokerBalanceLock.Lock()

	for shardId, txs := range alloctedTx {
		for _, tx := range txs {
			if tx.IsAllocatedSender {
				bcm.Broker.BrokerBalance[tx.Sender][shardId].Sub(bcm.Broker.BrokerBalance[tx.Sender][shardId], tx.Value)
			}
			if tx.IsAllocatedRecipent {
				bcm.Broker.BrokerBalance[tx.Recipient][shardId].Add(bcm.Broker.BrokerBalance[tx.Recipient][shardId], tx.Value)
			}
		}
	}
	//bcm.BrokerBalanceLock.Unlock()

}

func (bcm *BrokerhubCommitteeMod) GenerateAllocatedTx(alloctedBrokerRawMegs []*message.BrokerRawMeg) map[uint64][]*core.Transaction {
	//bcm.Broker.BrokerBalance
	brokerNewBalance := make(map[string]map[uint64]*big.Int)
	brokerChange := make(map[string]map[uint64]*big.Int)
	brokerPeekChange := make(map[string]map[uint64]*big.Int)

	// 1. init
	alloctedTxs := make(map[uint64][]*core.Transaction)
	for i := 0; i < params.ShardNum; i++ {
		alloctedTxs[uint64(i)] = make([]*core.Transaction, 0)
	}

	//bcm.BrokerBalanceLock.Lock()
	for brokerAddress, shardMap := range bcm.Broker.BrokerBalance {
		brokerNewBalance[brokerAddress] = make(map[uint64]*big.Int)
		brokerChange[brokerAddress] = make(map[uint64]*big.Int)
		brokerPeekChange[brokerAddress] = make(map[uint64]*big.Int)
		for shardId, balance := range shardMap {
			brokerNewBalance[brokerAddress][shardId] = new(big.Int).Set(balance)
			brokerChange[brokerAddress][shardId] = big.NewInt(0)
			brokerPeekChange[brokerAddress][shardId] = new(big.Int).Set(balance)
		}

	}
	//bcm.BrokerBalanceLock.Unlock()

	for _, brokerRawMeg := range alloctedBrokerRawMegs {
		sSid := bcm.fetchModifiedMap(brokerRawMeg.Tx.Sender)
		rSid := bcm.fetchModifiedMap(brokerRawMeg.Tx.Recipient)
		brokerAddress := brokerRawMeg.Broker

		brokerNewBalance[brokerAddress][sSid].Add(brokerNewBalance[brokerAddress][sSid], brokerRawMeg.Tx.Value)
		brokerNewBalance[brokerAddress][rSid].Sub(brokerNewBalance[brokerAddress][rSid], brokerRawMeg.Tx.Value)

		brokerPeekChange[brokerAddress][rSid].Sub(brokerPeekChange[brokerAddress][rSid], brokerRawMeg.Tx.Value)
	}

	for brokerAddress, shardMap := range brokerPeekChange {
		for shardId := range shardMap {

			peekBalance := brokerPeekChange[brokerAddress][shardId]

			if peekBalance.Cmp(big.NewInt(0)) < 0 {
				// If FromShard does not have enough balance, find another shard to cover the deficit

				deficit := new(big.Int).Set(peekBalance)
				deficit.Abs(deficit)
				for id, balance := range brokerPeekChange[brokerAddress] {
					if deficit.Cmp(big.NewInt(0)) == 0 {
						break
					}
					if id != shardId && balance.Cmp(big.NewInt(0)) > 0 {
						tmpValue := new(big.Int).Set(deficit)
						if balance.Cmp(deficit) < 0 {
							tmpValue.Set(balance)
							deficit.Sub(deficit, balance)
						} else {
							deficit.SetInt64(0)
						}
						brokerNewBalance[brokerAddress][id].Sub(brokerNewBalance[brokerAddress][id], tmpValue)
						brokerNewBalance[brokerAddress][shardId].Add(brokerNewBalance[brokerAddress][shardId], tmpValue)

						brokerPeekChange[brokerAddress][id].Sub(brokerPeekChange[brokerAddress][id], tmpValue)
						brokerPeekChange[brokerAddress][shardId].Add(brokerPeekChange[brokerAddress][shardId], tmpValue)

						brokerChange[brokerAddress][id].Sub(brokerChange[brokerAddress][id], tmpValue)
						brokerChange[brokerAddress][shardId].Add(brokerChange[brokerAddress][shardId], tmpValue)
					}
				}
			}
		}

	}
	// generate allocated tx

	for brokerAddress, shardMap := range brokerChange {
		for shardId := range shardMap {

			diff := brokerChange[brokerAddress][shardId]

			if diff.Cmp(big.NewInt(0)) == 0 {
				continue
			}
			tx := core.NewTransaction(brokerAddress, brokerAddress, new(big.Int).Abs(diff), uint64(bcm.nowDataNum), big.NewInt(0))

			bcm.nowDataNum++
			if diff.Cmp(big.NewInt(0)) < 0 {
				tx.IsAllocatedSender = true
			} else {
				tx.IsAllocatedRecipent = true
			}
			alloctedTxs[shardId] = append(alloctedTxs[shardId], tx)
		}

	}

	//bcm.BrokerBalanceLock.Unlock()
	return alloctedTxs
}

func (bcm *BrokerhubCommitteeMod) handleBrokerType1Mes(brokerType1Megs []*message.BrokerType1Meg) {
	tx1s := make([]*core.Transaction, 0)
	for _, brokerType1Meg := range brokerType1Megs {
		ctx := brokerType1Meg.RawMeg.Tx
		tx1 := core.NewTransaction(ctx.Sender, brokerType1Meg.Broker, ctx.Value, ctx.Nonce, ctx.Fee)
		tx1.OriginalSender = ctx.Sender
		tx1.FinalRecipient = ctx.Recipient
		tx1.RawTxHash = make([]byte, len(ctx.TxHash))
		tx1.Isbrokertx1 = true
		tx1.Isbrokertx2 = false
		copy(tx1.RawTxHash, ctx.TxHash)
		tx1s = append(tx1s, tx1)
		confirm1 := &message.Mag1Confirm{
			RawMeg:  brokerType1Meg.RawMeg,
			Tx1Hash: tx1.TxHash,
		}
		bcm.BrokerModuleLock.Lock()
		bcm.brokerConfirm1Pool[string(tx1.TxHash)] = confirm1
		bcm.BrokerModuleLock.Unlock()
	}
	bcm.txSending(tx1s)
	fmt.Println("BrokerType1Mes received by shard,  add brokerTx1 len ", len(tx1s))
}

func (bcm *BrokerhubCommitteeMod) handleBrokerType2Mes(brokerType2Megs []*message.BrokerType2Meg) {
	tx2s := make([]*core.Transaction, 0)
	for _, mes := range brokerType2Megs {
		ctx := mes.RawMeg.Tx
		tx2 := core.NewTransaction(mes.Broker, ctx.Recipient, ctx.Value, ctx.Nonce, ctx.Fee)
		tx2.OriginalSender = ctx.Sender
		tx2.FinalRecipient = ctx.Recipient
		tx2.RawTxHash = make([]byte, len(ctx.TxHash))
		tx2.Isbrokertx2 = true
		tx2.Isbrokertx1 = false
		copy(tx2.RawTxHash, ctx.TxHash)
		tx2s = append(tx2s, tx2)

		confirm2 := &message.Mag2Confirm{
			RawMeg:  mes.RawMeg,
			Tx2Hash: tx2.TxHash,
		}
		bcm.BrokerModuleLock.Lock()
		bcm.brokerConfirm2Pool[string(tx2.TxHash)] = confirm2
		bcm.BrokerModuleLock.Unlock()
	}
	bcm.txSending(tx2s)
	//fmt.Println("Broker tx2 add to pool len ", len(tx2s))
}

// get the digest of rawMeg
func (bcm *BrokerhubCommitteeMod) getBrokerRawMagDigest(r *message.BrokerRawMeg) []byte {
	b, err := json.Marshal(r)
	if err != nil {
		log.Panic(err)
	}
	hash := sha256.Sum256(b)
	return hash[:]
}

func (bcm *BrokerhubCommitteeMod) handleBrokerRawMag(brokerRawMags []*message.BrokerRawMeg) {
	b := bcm.Broker
	brokerType1Mags := make([]*message.BrokerType1Meg, 0)
	//fmt.Println("Broker receive ctx ", len(brokerRawMags))
	bcm.BrokerModuleLock.Lock()
	for _, meg := range brokerRawMags {
		b.BrokerRawMegs[string(bcm.getBrokerRawMagDigest(meg))] = meg
		brokerType1Mag := &message.BrokerType1Meg{
			RawMeg:   meg,
			Hcurrent: 0,
			Broker:   meg.Broker,
		}
		brokerType1Mags = append(brokerType1Mags, brokerType1Mag)
	}
	bcm.BrokerModuleLock.Unlock()
	bcm.handleBrokerType1Mes(brokerType1Mags)
}

func (bcm *BrokerhubCommitteeMod) handleTx1ConfirmMag(mag1confirms []*message.Mag1Confirm) {
	brokerType2Mags := make([]*message.BrokerType2Meg, 0)
	b := bcm.Broker

	fmt.Println("receive confirm  brokerTx1 len ", len(mag1confirms))
	bcm.BrokerModuleLock.Lock()
	for _, mag1confirm := range mag1confirms {
		RawMeg := mag1confirm.RawMeg
		_, ok := b.BrokerRawMegs[string(bcm.getBrokerRawMagDigest(RawMeg))]
		if !ok {
			fmt.Println("raw message is not exited,tx1 confirms failure !")
			continue
		}
		b.RawTx2BrokerTx[string(RawMeg.Tx.TxHash)] = append(b.RawTx2BrokerTx[string(RawMeg.Tx.TxHash)], string(mag1confirm.Tx1Hash))
		brokerType2Mag := &message.BrokerType2Meg{
			Broker: RawMeg.Broker,
			RawMeg: RawMeg,
		}
		brokerType2Mags = append(brokerType2Mags, brokerType2Mag)
	}
	bcm.BrokerModuleLock.Unlock()
	bcm.handleBrokerType2Mes(brokerType2Mags)
}

func (bcm *BrokerhubCommitteeMod) handleTx2ConfirmMag(mag2confirms []*message.Mag2Confirm) {
	b := bcm.Broker
	fmt.Println("receive confirm  brokerTx2 len ", len(mag2confirms))
	num := 0
	bcm.BrokerModuleLock.Lock()
	for _, mag2confirm := range mag2confirms {
		RawMeg := mag2confirm.RawMeg
		b.RawTx2BrokerTx[string(RawMeg.Tx.TxHash)] = append(b.RawTx2BrokerTx[string(RawMeg.Tx.TxHash)], string(mag2confirm.Tx2Hash))
		if len(b.RawTx2BrokerTx[string(RawMeg.Tx.TxHash)]) == 2 {
			num++
		} else {
			fmt.Println(len(b.RawTx2BrokerTx[string(RawMeg.Tx.TxHash)]))
		}
	}
	bcm.BrokerModuleLock.Unlock()
	//fmt.Println("finish ctx with adding tx1 and tx2 to txpool,len", num)
}

func (bcm *BrokerhubCommitteeMod) Result_save() {

	// write to .csv file
	dirpath := params.DataWrite_path + "brokerRsult/"
	err := os.MkdirAll(dirpath, os.ModePerm)
	if err != nil {
		log.Panic(err)
	}
	for brokerAddress := range bcm.Broker.BrokerBalance {
		targetPath0 := dirpath + brokerAddress + "_lockBalance.csv"
		targetPath1 := dirpath + brokerAddress + "_brokerBalance.csv"
		targetPath2 := dirpath + brokerAddress + "_Profit.csv"
		bcm.Wirte_result(targetPath0, bcm.Result_lockBalance[brokerAddress])
		bcm.Wirte_result(targetPath1, bcm.Result_brokerBalance[brokerAddress])
		bcm.Wirte_result(targetPath2, bcm.Result_Profit[brokerAddress])
	}
}
func (bcm *BrokerhubCommitteeMod) Wirte_result(targetPath string, resultStr []string) {

	f, err := os.Open(targetPath)
	if err != nil && os.IsNotExist(err) {
		file, er := os.Create(targetPath)
		if er != nil {
			panic(er)
		}
		defer file.Close()

		w := csv.NewWriter(file)
		w.Flush()
		for _, str := range resultStr {
			str_arry := strings.Split(str, ",")
			w.Write(str_arry[0 : len(str_arry)-1])
			w.Flush()
		}
	} else {
		file, err := os.OpenFile(targetPath, os.O_APPEND|os.O_RDWR, 0666)

		if err != nil {
			log.Panic(err)
		}
		defer file.Close()
		writer := csv.NewWriter(file)
		err = writer.Write(resultStr)
		if err != nil {
			log.Panic()
		}
		writer.Flush()
	}
	f.Close()
}
