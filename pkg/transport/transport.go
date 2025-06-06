package transport

import (
	"context"
	"fmt"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/IOperatorTableUpdater"
	"github.com/Layr-Labs/multichain-go/pkg/distribution"
	"github.com/Layr-Labs/multichain-go/pkg/operatorTableCalculator"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/wealdtech/go-merkletree/v2"
	"go.uber.org/zap"
)

type TransportConfig struct {
	OperatorTableUpdaterAddress common.Address
}

type Transport struct {
	config                          *TransportConfig
	logger                          *zap.Logger
	stakeTableCalc                  *operatorTableCalculator.StakeTableCalculator
	operatorTableUpdaterTransaction *IOperatorTableUpdater.IOperatorTableUpdaterTransactor
}

func NewTransport(cfg *TransportConfig, client *ethclient.Client, logger *zap.Logger) (*Transport, error) {
	opTableUpdaterTransactor, err := IOperatorTableUpdater.NewIOperatorTableUpdaterTransactor(cfg.OperatorTableUpdaterAddress, client)
	if err != nil {
		return nil, err
	}

	return &Transport{
		logger:                          logger,
		config:                          cfg,
		operatorTableUpdaterTransaction: opTableUpdaterTransactor,
	}, nil
}

func (t *Transport) GenerateSignAndTransportGlobalTableRoot(referenceTimestamp uint32, referenceBlockHeight uint64) (interface{}, error) {
	root, _, _, err := t.stakeTableCalc.CalculateStakeTableRoot(context.Background(), referenceBlockHeight)
	if err != nil {
		t.logger.Error("failed to calculate stake table root", zap.Error(err))
		return nil, err
	}

	t.logger.Info("Successfully calculated stake table root",
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	return t.SignAndTransportGlobalTableRoot(root, referenceTimestamp, referenceBlockHeight)
}

func (t *Transport) SignAndTransportGlobalTableRoot(root [32]byte, referenceTimestamp uint32, referenceBlockHeight uint64) (interface{}, error) {
	t.logger.Info("Signing and transporting global table root",
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	// TODO(seanmcgary): this needs to be called for the L1 and every destination L2
	// call crossChainReg.GetSupportedChainIds()
	re, err := t.operatorTableUpdaterTransaction.ConfirmGlobalTableRoot(
		nil,
		IOperatorTableUpdater.IBN254CertificateVerifierTypesBN254Certificate{},
		root,
		referenceTimestamp,
	)
	if err != nil {
		t.logger.Error("Failed to update BN254 operator table", zap.Error(err))
		return nil, err
	}

	t.logger.Info("Successfully signed and transported global table root",
		zap.String("transactionHash", re.Hash().Hex()),
	)

	return re, nil
}

func flattenHashes(hashes [][]byte) []byte {
	result := make([]byte, 0)
	for i := 0; i < len(hashes); i++ {
		result = append(result, hashes[i]...)
	}
	return result
}

func (t *Transport) GenerateOperatorSetProof(tree *merkletree.MerkleTree, dist *distribution.Distribution, operatorSet distribution.OperatorSet) ([]byte, uint64, error) {
	opsetIndex, found := dist.GetTableIndex(operatorSet)
	if !found {
		return nil, 0, fmt.Errorf("operator set %v not found in distribution", operatorSet)
	}
	proof, err := tree.GenerateProofWithIndex(opsetIndex, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to generate proof for operator set %v: %w", operatorSet, err)
	}

	proofBytes := flattenHashes(proof.Hashes)

	t.logger.Info("Successfully generated proof for operator set",
		zap.Any("operatorSet", operatorSet),
		zap.String("proof", hexutil.Encode(proofBytes)),
	)
	return proofBytes, opsetIndex, nil
}

// SignAndTransportAvsStakeTable signs and transports the AVS stake table
func (t *Transport) SignAndTransportAvsStakeTable(
	referenceTimestamp uint32,
	referenceBlockHeight uint64,
	operatorSet distribution.OperatorSet,
) (*types.Transaction, error) {
	root, tree, dist, err := t.stakeTableCalc.CalculateStakeTableRoot(context.Background(), referenceBlockHeight)
	if err != nil {
		t.logger.Error("failed to calculate stake table root", zap.Error(err))
		return nil, err
	}

	proof, opsetIndex, err := t.GenerateOperatorSetProof(tree, dist, operatorSet)
	if err != nil {
		t.logger.Error("failed to generate operator set proof", zap.Error(err))
		return nil, err
	}

	tableInfo, found := dist.GetTableData(operatorSet)
	if !found {
		return nil, fmt.Errorf("operator set %v not found in distribution", operatorSet)
	}

	// TODO(seanmcgary): need to get which key type is expected

	t.logger.Info("Signing and transporting AVS stake table",
		zap.String("avsAddress", operatorSet.Avs.String()),
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	return t.operatorTableUpdaterTransaction.UpdateOperatorTable(
		nil,
		referenceTimestamp,
		root,
		uint32(opsetIndex),
		proof,
		tableInfo,
	)
}
