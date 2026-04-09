package committee

import (
	"io"
	"log"
	"math/big"
	"os"
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

	logger := &supervisor_log.SupervisorLog{
		Slog: log.New(io.Discard, "", 0),
	}
	stopSignal := signal.NewStopSignal(2 * params.ShardNum)
	bcm := NewBrokerhubCommitteeMod(
		map[uint64]map[uint64]string{},
		stopSignal,
		logger,
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

	if _, err := os.Stat("./hubres/hub0.csv"); err != nil {
		t.Fatalf("expected hub0.csv to be written, got %v", err)
	}
	if _, err := os.Stat("./hubres/hub1.csv"); err != nil {
		t.Fatalf("expected hub1.csv to be written, got %v", err)
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
