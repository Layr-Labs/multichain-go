package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/Layr-Labs/crypto-libs/pkg/bn254"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/ICrossChainRegistry"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/IOperatorTableUpdater"
	"github.com/Layr-Labs/multichain-go/pkg/blsSigner"
	"github.com/Layr-Labs/multichain-go/pkg/chainManager"
	"github.com/Layr-Labs/multichain-go/pkg/distribution"
	"github.com/Layr-Labs/multichain-go/pkg/txSigner"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	merkletree "github.com/wealdtech/go-merkletree/v2"
	"go.uber.org/zap"
	"math/big"
)

type TransportConfig struct {
	L1CrossChainRegistryAddress common.Address
}

type Transport struct {
	config                   *TransportConfig
	logger                   *zap.Logger
	crossChainRegistryCaller *ICrossChainRegistry.ICrossChainRegistryCaller
	blsSigner                blsSigner.IBLSSigner
	txSigner                 txSigner.ITransactionSigner
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
		chainManager:             cm,
		crossChainRegistryCaller: ccRegistryCaller,
	}, nil
}

var emptyRoot [32]byte

func (t *Transport) SignAndTransportGlobalTableRoot(
	root [32]byte,
	referenceTimestamp uint32,
	referenceBlockHeight uint64,
	ignoreChainIds []*big.Int,
) error {
	t.logger.Info("Signing and transporting global table root",
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	if root == emptyRoot {
		t.logger.Info("Empty root provided, skipping signing and transport")
		return nil
	}

	// Generate the proper message hash using referenceTimestamp and globalTableRoot
	messageHash, err := t.generateGlobalTableUpdateMessageHash(referenceTimestamp, root)
	if err != nil {
		return fmt.Errorf("failed to generate global table update message hash: %w", err)
	}

	sigG1, err := t.generateMessageHashSignature(messageHash)
	if err != nil {
		return err
	}

	apkG2, err := t.getApkFromPrivateKey()
	if err != nil {
		return fmt.Errorf("failed to get APK from private key: %w", err)
	}

	t.logger.Sugar().Infow("Getting supported chains from cross-chain registry",
		zap.String("crossChainRegistryAddress", t.config.L1CrossChainRegistryAddress.String()),
	)

	chainIds, addresses, err := t.crossChainRegistryCaller.GetSupportedChains(&bind.CallOpts{})
	if err != nil {
		return fmt.Errorf("failed to get supported chains: %w", err)
	}

	if len(chainIds) == 0 {
		return fmt.Errorf("no supported chains found in cross-chain registry")
	}

	for i, chainId := range chainIds {
		for _, ignoreChainId := range ignoreChainIds {
			if chainId.Uint64() == ignoreChainId.Uint64() {
				t.logger.Sugar().Infow("Ignoring chain ID",
					zap.Uint64("chainId", chainId.Uint64()),
				)
				continue
			}
		}
		addr := addresses[i]
		chain, err := t.chainManager.GetChainForId(chainId.Uint64())
		if err != nil {
			return fmt.Errorf("failed to get chain for ID %d: %w", chainId, err)
		}

		t.logger.Sugar().Infow("Transporting global table root to chain",
			zap.Uint64("chainId", chainId.Uint64()),
			zap.String("chainAddress", addr.String()),
		)
		updaterTransactor, err := getOperatorTableUpdaterForChainClient(addr, chain.RPCClient)
		if err != nil {
			return fmt.Errorf("failed to get operator table updater transactor for chain %d: %w", chainId, err)
		}

		previouslyReferencedTimestamp, err := updaterTransactor.GetLatestReferenceTimestamp(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("failed to get latest reference timestamp: %w", err)
		}
		_ = previouslyReferencedTimestamp

		// Get transaction options from signer
		txOpts, err := t.txSigner.GetTransactOpts(context.Background(), chainId)
		if err != nil {
			return fmt.Errorf("failed to get transaction options: %w", err)
		}

		cert := IOperatorTableUpdater.IBN254CertificateVerifierTypesBN254Certificate{
			MessageHash:        messageHash, // Use computed message hash instead of raw root
			ReferenceTimestamp: referenceTimestamp,
			Signature:          *sigG1,
			Apk:                *apkG2,
		}

		re, err := updaterTransactor.ConfirmGlobalTableRoot(
			txOpts,
			cert,
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

// SignAndTransportAvsStakeTable signs and transports the AVS stake table
// NOTE: the global root must be updated in the previous block otherwise
// this function will fail
func (t *Transport) SignAndTransportAvsStakeTable(
	referenceTimestamp uint32,
	referenceBlockHeight uint64,
	operatorSet distribution.OperatorSet,
	root [32]byte,
	tree *merkletree.MerkleTree,
	dist *distribution.Distribution,
) error {
	// generate the proof for the specific operator set
	proof, opsetIndex, err := t.generateOperatorSetProof(tree, dist, operatorSet)
	if err != nil {
		t.logger.Error("failed to generate operator set proof", zap.Error(err))
		return err
	}

	// get the data specific to the operator set
	tableInfo, found := dist.GetTableData(operatorSet)
	if !found {
		return fmt.Errorf("operator set %v not found in distribution", operatorSet)
	}

	t.logger.Info("Signing and transporting AVS stake table",
		zap.String("avsAddress", operatorSet.Avs.String()),
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	chainIds, addresses, err := t.crossChainRegistryCaller.GetSupportedChains(&bind.CallOpts{})
	if err != nil {
		return fmt.Errorf("failed to get supported chains: %w", err)
	}

	// transport the stake table to each supported destination chain
	for i, chainId := range chainIds {
		addr := addresses[i]
		// Get transaction options from signer
		txOpts, err := t.txSigner.GetTransactOpts(context.Background(), chainId)
		if err != nil {
			return fmt.Errorf("failed to get transaction options: %w", err)
		}
		chain, err := t.chainManager.GetChainForId(chainId.Uint64())
		if err != nil {
			return fmt.Errorf("failed to get chain for ID %d: %w", chainId, err)
		}

		t.logger.Info("Transporting AVS stake table to chain",
			zap.Uint64("chainId", chainId.Uint64()),
			zap.String("address", addr.String()),
		)
		updaterTransactor, err := getOperatorTableUpdaterForChainClient(addr, chain.RPCClient)
		if err != nil {
			return fmt.Errorf("failed to get operator table updater transactor for chain %d: %w", chainId, err)
		}
		t.logger.Sugar().Debugw("Using operator table updater transactor",
			zap.Uint64("chainId", chainId.Uint64()),
			zap.Uint64("referenceBlockHeight", referenceBlockHeight),
			zap.Uint32("referenceTimestamp", referenceTimestamp),
			zap.String("root", hexutil.Encode(root[:])),
			zap.Uint64("opsetIndex", opsetIndex),
			zap.String("proof", hexutil.Encode(proof)),
			zap.String("tableInfo", hexutil.Encode(tableInfo)),
		)
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
			zap.Uint64("chainId", chainId.Uint64()),
		)
	}
	return nil
}

func (t *Transport) generateOperatorSetProof(tree *merkletree.MerkleTree, dist *distribution.Distribution, operatorSet distribution.OperatorSet) ([]byte, uint64, error) {
	t.logger.Sugar().Infow("Generating proof for operator set",
		zap.Any("operatorSet", operatorSet),
	)
	opsetIndex, found := dist.GetTableIndex(operatorSet)
	if !found {
		return nil, 0, fmt.Errorf("operator set %v not found in distribution", operatorSet)
	}
	t.logger.Sugar().Infow("Operator set index found",
		zap.Uint64("opsetIndex", opsetIndex),
	)
	tableData, found := dist.GetTableData(operatorSet)
	if !found {
		return nil, 0, fmt.Errorf("failed to get table data for operator set %v", operatorSet)
	}
	t.logger.Sugar().Debugw("Table data for operator set",
		zap.Uint64("opsetIndex", opsetIndex),
		zap.Int("tableDataLength", len(tableData)),
		zap.ByteString("tableData", tableData),
	)

	proof, err := tree.GenerateProofWithIndex(opsetIndex, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to generate proof for operator set %v: %w", operatorSet, err)
	}
	t.logger.Sugar().Infow("Generated proof for operator set",
		zap.Any("proof", proof),
	)

	proofBytes := flattenHashes(proof.Hashes)

	t.logger.Info("Successfully generated proof for operator set",
		zap.Any("operatorSet", operatorSet),
		zap.String("proof", hexutil.Encode(proofBytes)),
	)
	return proofBytes, opsetIndex, nil
}

func getOperatorTableUpdaterForChainClient(address common.Address, client *ethclient.Client) (*IOperatorTableUpdater.IOperatorTableUpdater, error) {
	transactor, err := IOperatorTableUpdater.NewIOperatorTableUpdater(address, client)
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

// generateMessageHashSignature signs the computed message hash
func (t *Transport) generateMessageHashSignature(messageHash [32]byte) (*IOperatorTableUpdater.BN254G1Point, error) {
	// Sign the message hash using the private key
	signature, err := t.blsSigner.SignBytes(messageHash)
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

func flattenHashes(hashes [][]byte) []byte {
	result := make([]byte, 0)
	for i := 0; i < len(hashes); i++ {
		result = append(result, hashes[i]...)
	}
	return result
}

// generateGlobalTableUpdateMessageHash generates the message hash for global table updates
// using the referenceTimestamp and globalTableRoot
func (t *Transport) generateGlobalTableUpdateMessageHash(referenceTimestamp uint32, globalTableRoot [32]byte) ([32]byte, error) {
	timestampBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(timestampBytes, referenceTimestamp)

	// Concatenate globalTableRoot with timestamp
	data := make([]byte, 0, 32+4)
	data = append(data, globalTableRoot[:]...)
	data = append(data, timestampBytes...)

	// Compute hash of the concatenated data
	hash := crypto.Keccak256Hash(data)

	t.logger.Sugar().Debugw("Generated global table update message hash",
		zap.String("globalTableRoot", hexutil.Encode(globalTableRoot[:])),
		zap.Uint32("referenceTimestamp", referenceTimestamp),
		zap.String("messageHash", hexutil.Encode(hash[:])),
	)

	return hash, nil
}
