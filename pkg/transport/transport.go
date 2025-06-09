package transport

import (
	"context"
	"fmt"
	"github.com/Layr-Labs/crypto-libs/pkg/bn254"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/ICrossChainRegistry"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/IOperatorTableUpdater"
	"github.com/Layr-Labs/multichain-go/pkg/blsSigner"
	"github.com/Layr-Labs/multichain-go/pkg/chainManager"
	"github.com/Layr-Labs/multichain-go/pkg/distribution"
	"github.com/Layr-Labs/multichain-go/pkg/operatorTableCalculator"
	"github.com/Layr-Labs/multichain-go/pkg/txSigner"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/wealdtech/go-merkletree/v2"
	"go.uber.org/zap"
	"math/big"
)

type TransportConfig struct {
	L1OperatorTableUpdaterAddress common.Address
	L1CrossChainRegistryAddress   common.Address
}

type Transport struct {
	config                   *TransportConfig
	logger                   *zap.Logger
	stakeTableCalc           *operatorTableCalculator.StakeTableCalculator
	crossChainRegistryCaller *ICrossChainRegistry.ICrossChainRegistryCaller
	blsSigner                blsSigner.IBLSSigner
	txSigner                 txSigner.ITransactionSigner
	ethClient                *ethclient.Client
	chainManager             chainManager.IChainManager
}

func NewTransport(
	cfg *TransportConfig,
	client *ethclient.Client,
	blsSig blsSigner.IBLSSigner,
	txSig txSigner.ITransactionSigner,
	cm chainManager.IChainManager,
	logger *zap.Logger,
) (*Transport, error) {
	ccRegistryCaller, err := ICrossChainRegistry.NewICrossChainRegistryCaller(cfg.L1CrossChainRegistryAddress, client)
	if err != nil {
		return nil, fmt.Errorf("failed to bind NewICrossChainRegistryCaller: %w", err)
	}

	return &Transport{
		logger:                   logger,
		config:                   cfg,
		blsSigner:                blsSig,
		txSigner:                 txSig,
		ethClient:                client,
		chainManager:             cm,
		crossChainRegistryCaller: ccRegistryCaller,
	}, nil
}

func (t *Transport) GenerateSignAndTransportGlobalTableRoot(referenceTimestamp uint32, referenceBlockHeight uint64) error {
	root, _, _, _, err := t.stakeTableCalc.CalculateStakeTableRoot(context.Background(), referenceBlockHeight)
	if err != nil {
		t.logger.Error("failed to calculate stake table root", zap.Error(err))
		return err
	}

	t.logger.Info("Successfully calculated stake table root",
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	return t.SignAndTransportGlobalTableRoot(root, referenceTimestamp, referenceBlockHeight)
}

func (t *Transport) SignAndTransportGlobalTableRoot(
	root [32]byte,
	referenceTimestamp uint32,
	referenceBlockHeight uint64,
) error {
	t.logger.Info("Signing and transporting global table root",
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	sigG1, err := t.generateMessageHashSignature(root)
	if err != nil {
		return err
	}

	apkG2, err := t.getApkFromPrivateKey()
	if err != nil {
		return fmt.Errorf("failed to get APK from private key: %w", err)
	}

	chainIds, err := t.crossChainRegistryCaller.GetSupportedChains(&bind.CallOpts{})
	if err != nil {
		return fmt.Errorf("failed to get supported chains: %w", err)
	}

	if len(chainIds) == 0 {
		return fmt.Errorf("no supported chains found in cross-chain registry")
	}

	for _, chainId := range chainIds {
		chain, err := t.chainManager.GetChainForId(chainId)
		if err != nil {
			return fmt.Errorf("failed to get chain for ID %d: %w", chainId, err)
		}

		// TODO(seanmcgary): need this address from GetSupportedChains
		updaterTransactor, err := getOperatorTableUpdaterTransactorForChainClient(common.Address{}, chain.RPCClient)
		if err != nil {
			return fmt.Errorf("failed to get operator table updater transactor for chain %d: %w", chainId, err)
		}

		// Get transaction options from signer
		txOpts, err := t.txSigner.GetTransactOpts(context.Background(), chainId)
		if err != nil {
			return fmt.Errorf("failed to get transaction options: %w", err)
		}

		re, err := updaterTransactor.ConfirmGlobalTableRoot(
			txOpts,
			IOperatorTableUpdater.IBN254CertificateVerifierTypesBN254Certificate{
				MessageHash:        root,
				ReferenceTimestamp: referenceTimestamp,
				Signature:          *sigG1,
				Apk:                *apkG2,
			},
			root,
			referenceTimestamp,
		)
		if err != nil {
			t.logger.Error("Failed to update BN254 operator table", zap.Error(err))
			return err
		}

		t.logger.Info("Successfully signed and transported global table root",
			zap.String("transactionHash", re.Hash().Hex()),
		)
	}

	return nil
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
) error {
	root, tree, dist, opsetCalcAddresses, err := t.stakeTableCalc.CalculateStakeTableRoot(context.Background(), referenceBlockHeight)
	if err != nil {
		t.logger.Error("failed to calculate stake table root", zap.Error(err))
		return err
	}

	proof, opsetIndex, err := t.GenerateOperatorSetProof(tree, dist, operatorSet)
	if err != nil {
		t.logger.Error("failed to generate operator set proof", zap.Error(err))
		return err
	}

	tableInfo, found := dist.GetTableData(operatorSet)
	if !found {
		return fmt.Errorf("operator set %v not found in distribution", operatorSet)
	}

	t.logger.Info("Signing and transporting AVS stake table",
		zap.String("avsAddress", operatorSet.Avs.String()),
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	chainIds, err := t.crossChainRegistryCaller.GetSupportedChains(&bind.CallOpts{})
	if err != nil {
		return fmt.Errorf("failed to get supported chains: %w", err)
	}

	// transport the stake table to each supported destination chain
	for _, chainId := range chainIds {
		// Get transaction options from signer
		txOpts, err := t.txSigner.GetTransactOpts(context.Background(), chainId)
		if err != nil {
			return fmt.Errorf("failed to get transaction options: %w", err)
		}
		chain, err := t.chainManager.GetChainForId(chainId)
		if err != nil {
			return fmt.Errorf("failed to get chain for ID %d: %w", chainId, err)
		}

		// TODO(seanmcgary): need this address from GetSupportedChains
		updaterTransactor, err := getOperatorTableUpdaterTransactorForChainClient(common.Address{}, chain.RPCClient)
		if err != nil {
			return fmt.Errorf("failed to get operator table updater transactor for chain %d: %w", chainId, err)
		}
		tx, err := updaterTransactor.UpdateOperatorTable(
			txOpts,
			referenceTimestamp,
			root,
			uint32(opsetIndex),
			proof,
			tableInfo,
		)
		if err != nil {
			t.logger.Error("Failed to update AVS stake table", zap.Error(err))
			return fmt.Errorf("failed to update AVS stake table: %w", err)
		}
		t.logger.Info("Successfully signed and transported AVS stake table",
			zap.String("transactionHash", tx.Hash().Hex()),
			zap.String("avsAddress", operatorSet.Avs.String()),
			zap.String("root", hexutil.Encode(root[:])),
			zap.Uint64("blockHeight", referenceBlockHeight),
			zap.Uint64("opsetIndex", opsetIndex),
			zap.Uint32("chainId", chainId),
		)
	}
	return nil
}

func getOperatorTableUpdaterTransactorForChainClient(address common.Address, client *ethclient.Client) (*IOperatorTableUpdater.IOperatorTableUpdaterTransactor, error) {
	transactor, err := IOperatorTableUpdater.NewIOperatorTableUpdaterTransactor(address, client)
	if err != nil {
		return nil, fmt.Errorf("failed to bind NewIOperatorTableUpdaterTransactor: %w", err)
	}
	return transactor, nil
}

func (t *Transport) getApkFromPrivateKey() (*IOperatorTableUpdater.BN254G2Point, error) {
	g2Point := bn254.NewZeroG2Point().AddPublicKey(t.blsSigner.GetPublicKey())

	g2Bytes, err := g2Point.ToPrecompileFormat()
	if err != nil {
		return nil, fmt.Errorf("public key not in correct subgroup: %w", err)
	}

	return &IOperatorTableUpdater.BN254G2Point{
		X: [2]*big.Int{
			new(big.Int).SetBytes(g2Bytes[0:32]),
			new(big.Int).SetBytes(g2Bytes[32:64]),
		},
		Y: [2]*big.Int{
			new(big.Int).SetBytes(g2Bytes[64:96]),
			new(big.Int).SetBytes(g2Bytes[96:128]),
		},
	}, nil
}

func (t *Transport) generateMessageHashSignature(root [32]byte) (*IOperatorTableUpdater.BN254G1Point, error) {
	// Sign the message hash using the private key
	signature, err := t.blsSigner.SignBytes(root[:])
	if err != nil {
		t.logger.Error("Failed to sign message hash", zap.Error(err))
		return nil, fmt.Errorf("failed to sign message hash: %w", err)
	}

	g1Point := &bn254.G1Point{
		G1Affine: signature.GetG1Point(),
	}
	g1Bytes, err := g1Point.ToPrecompileFormat()
	if err != nil {
		return nil, fmt.Errorf("failed to convert G1 point to precompile format: %w", err)
	}
	return &IOperatorTableUpdater.BN254G1Point{
		X: new(big.Int).SetBytes(g1Bytes[0:32]),
		Y: new(big.Int).SetBytes(g1Bytes[32:64]),
	}, nil
}
