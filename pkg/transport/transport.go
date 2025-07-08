package transport

import (
	"context"
	"errors"
	"fmt"
	"github.com/Layr-Labs/crypto-libs/pkg/bn254"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/ICrossChainRegistry"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/IOperatorTableUpdater"
	"github.com/Layr-Labs/multichain-go/pkg/blsSigner"
	"github.com/Layr-Labs/multichain-go/pkg/chainManager"
	"github.com/Layr-Labs/multichain-go/pkg/distribution"
	"github.com/Layr-Labs/multichain-go/pkg/txSigner"
	"github.com/Layr-Labs/multichain-go/pkg/util"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	merkletree "github.com/wealdtech/go-merkletree/v2"
	"go.uber.org/zap"
	"math/big"
	"time"
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
	ctx context.Context,
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
		ignoredChainId := util.Find(ignoreChainIds, func(id *big.Int) bool {
			return chainId.Cmp(id) == 0
		})
		if ignoredChainId != nil {
			t.logger.Sugar().Infow("Skipping transport for ignored chain",
				zap.Uint64("chainId", chainId.Uint64()),
				zap.String("chainAddress", addresses[i].String()),
			)
			continue
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

		messageHash, err := updaterTransactor.GetGlobalTableUpdateMessageHash(&bind.CallOpts{}, root, referenceTimestamp, uint32(referenceBlockHeight))
		if err != nil {
			return fmt.Errorf("failed to get global table update message hash: %w", err)
		}

		sigG1, err := t.generateMessageHashSignature(messageHash)
		if err != nil {
			return err
		}

		previouslyReferencedTimestamp, err := updaterTransactor.GetGeneratorReferenceTimestamp(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("failed to get latest reference timestamp: %w", err)
		}
		t.logger.Sugar().Infow("reference timestamp for global table root",
			zap.Uint32("previouslyReferencedTimestamp", previouslyReferencedTimestamp),
			zap.Uint32("newReferenceTimestamp", referenceTimestamp),
			zap.Uint64("chainId", chainId.Uint64()),
		)

		// Get transaction options from signer
		txOpts, err := t.txSigner.GetNoSendTransactOpts(context.Background(), chainId)
		if err != nil {
			return fmt.Errorf("failed to get transaction options: %w", err)
		}

		cert := IOperatorTableUpdater.IBN254CertificateVerifierTypesBN254Certificate{
			MessageHash:        messageHash,
			ReferenceTimestamp: previouslyReferencedTimestamp,
			Signature:          *sigG1,
			Apk:                *apkG2,
		}

		tx, err := updaterTransactor.ConfirmGlobalTableRoot(
			txOpts,
			cert,
			root,
			referenceTimestamp,
			uint32(referenceBlockHeight),
		)
		t.logger.Sugar().Infow("Created transaction for global table root",
			zap.Uint64("chainId", chainId.Uint64()),
			zap.Uint64("referenceBlockHeight", referenceBlockHeight),
			zap.String("chainAddress", addr.String()),
			zap.String("from", txOpts.From.String()),
		)

		if err != nil {
			t.logger.Sugar().Errorw("Failed to confirm global table root",
				zap.Uint64("chainId", chainId.Uint64()),
				zap.String("chainAddress", addr.String()),
				zap.Error(err),
			)
			t.logger.Error("Failed to confirm global table root", zap.Error(err))
			return err
		}
		r, err := t.estimateGasPriceAndLimitAndSendTx(ctx, txOpts.From, tx, chain.RPCClient, "ConfirmGlobalTableRoot")
		if err != nil {
			t.logger.Error("Failed to ensure transaction evaled for global table root",
				zap.Uint64("chainId", chainId.Uint64()),
				zap.String("chainAddress", addr.String()),
				zap.Error(err),
			)
			return fmt.Errorf("failed to ensure transaction evaled: %w", err)
		}

		t.logger.Info("successfully transported global table root",
			zap.String("transactionHash", r.TxHash.String()),
			zap.String("root", hexutil.Encode(root[:])),
			zap.Uint64("chainId", chainId.Uint64()),
		)
		time.Sleep(time.Second * 3) // Sleep to avoid rate limiting issues
	}

	return nil
}

// SignAndTransportAvsStakeTable signs and transports the AVS stake table
// NOTE: the global root must be updated in the previous block otherwise
// this function will fail
func (t *Transport) SignAndTransportAvsStakeTable(
	ctx context.Context,
	referenceTimestamp uint32,
	referenceBlockHeight uint64,
	operatorSet distribution.OperatorSet,
	root [32]byte,
	tree *merkletree.MerkleTree,
	dist *distribution.Distribution,
	ignoreChainIds []*big.Int,
) error {
	t.logger.Sugar().Infow("starting transport of AVS stake table for opset",
		zap.Any("opset", operatorSet),
	)
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
		zap.Any("opset", operatorSet),
		zap.String("root", hexutil.Encode(root[:])),
		zap.Uint64("blockHeight", referenceBlockHeight),
	)

	chainIds, addresses, err := t.crossChainRegistryCaller.GetSupportedChains(&bind.CallOpts{})
	if err != nil {
		return fmt.Errorf("failed to get supported chains: %w", err)
	}

	// transport the stake table to each supported destination chain
	for i, chainId := range chainIds {
		t.logger.Sugar().Infow("Processing chain for AVS stake table transport",
			zap.Any("opset", operatorSet),
			zap.Uint64("chainId", chainId.Uint64()),
			zap.String("chainAddress", addresses[i].String()),
		)
		ignoredChainId := util.Find(ignoreChainIds, func(id *big.Int) bool {
			return chainId.Cmp(id) == 0
		})
		if ignoredChainId != nil {
			t.logger.Sugar().Infow("Skipping transport for ignored chain",
				zap.Any("opset", operatorSet),
				zap.Uint64("chainId", chainId.Uint64()),
				zap.String("chainAddress", addresses[i].String()),
			)
			continue
		}
		addr := addresses[i]
		// Get transaction options from signer
		txOpts, err := t.txSigner.GetNoSendTransactOpts(context.Background(), chainId)
		if err != nil {
			return fmt.Errorf("failed to get transaction options: %w", err)
		}
		chain, err := t.chainManager.GetChainForId(chainId.Uint64())
		if err != nil {
			return fmt.Errorf("failed to get chain for ID %d: %w", chainId, err)
		}

		t.logger.Info("Transporting AVS stake table to chain",
			zap.Any("opset", operatorSet),
			zap.Uint64("chainId", chainId.Uint64()),
			zap.String("address", addr.String()),
		)
		updaterTransactor, err := getOperatorTableUpdaterForChainClient(addr, chain.RPCClient)
		if err != nil {
			return fmt.Errorf("failed to get operator table updater transactor for chain %d: %w", chainId, err)
		}
		t.logger.Sugar().Debugw("Using operator table updater transactor",
			zap.Any("opset", operatorSet),
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
			t.logger.Error("Failed to update AVS stake table",
				zap.String("avsAddress", operatorSet.Avs.String()),
				zap.Uint64("opsetIndex", opsetIndex),
				zap.Uint64("chainId", chainId.Uint64()),
				zap.Error(err),
			)
			return fmt.Errorf("failed to update AVS stake table: %w", err)
		}
		r, err := t.estimateGasPriceAndLimitAndSendTx(ctx, txOpts.From, tx, chain.RPCClient, "UpdateOperatorTable")
		if err != nil {
			t.logger.Error("Failed to ensure transaction evaled for AVS stake table",
				zap.String("avsAddress", operatorSet.Avs.String()),
				zap.Uint64("opsetIndex", opsetIndex),
				zap.Uint64("chainId", chainId.Uint64()),
				zap.Error(err),
			)
			return fmt.Errorf("failed to ensure transaction evaled: %w", err)
		}
		t.logger.Info("Successfully transported AVS stake table",
			zap.Any("opset", operatorSet),
			zap.Uint32("referenceTimestamp", referenceTimestamp),
			zap.String("transactionHash", r.TxHash.String()),
			zap.String("avsAddress", operatorSet.Avs.String()),
			zap.String("root", hexutil.Encode(root[:])),
			zap.Uint64("blockHeight", referenceBlockHeight),
			zap.Uint64("opsetIndex", opsetIndex),
			zap.Uint64("chainId", chainId.Uint64()),
		)
	}
	return nil
}

func (t *Transport) ensureTransactionEvaled(ctx context.Context, rpcClient *ethclient.Client, tx *types.Transaction, tag string) (*types.Receipt, error) {
	t.logger.Sugar().Infow("ensureTransactionEvaled entered")

	receipt, err := bind.WaitMined(ctx, rpcClient, tx)
	if err != nil {
		return nil, fmt.Errorf("ensureTransactionEvaled: failed to wait for transaction (%s) to mine: %w", tag, err)
	}
	if receipt.Status != 1 {
		t.logger.Sugar().Errorf("ensureTransactionEvaled: transaction (%s) failed: %v", tag, receipt)
		return nil, errors.New("ErrTransactionFailed")
	}
	t.logger.Sugar().Infof("ensureTransactionEvaled: transaction (%s) succeeded: %v", tag, receipt.TxHash.Hex())
	return receipt, nil
}

var (
	FallbackGasTipCap = big.NewInt(15000000000)
)

func addGasBuffer(gasLimit uint64) uint64 {
	return 6 * gasLimit / 5 // add 20% buffer to gas limit
}

func (t *Transport) estimateGasPriceAndLimitAndSendTx(
	ctx context.Context,
	fromAddress common.Address,
	tx *types.Transaction,
	rpcClient *ethclient.Client,
	tag string,
) (*types.Receipt, error) {

	gasTipCap, err := rpcClient.SuggestGasTipCap(ctx)
	if err != nil {
		// If the transaction failed because the backend does not support
		// eth_maxPriorityFeePerGas, fallback to using the default constant.
		// Currently Alchemy is the only backend provider that exposes this
		// method, so in the event their API is unreachable we can fallback to a
		// degraded mode of operation. This also applies to our test
		// environments, as hardhat doesn't support the query either.
		t.logger.Sugar().Debugw("EstimateGasPriceAndLimitAndSendTx: cannot get gasTipCap",
			zap.String("error", err.Error()),
		)

		gasTipCap = FallbackGasTipCap
	}

	header, err := rpcClient.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	// get header basefee * 3/2
	overestimatedBasefee := new(big.Int).Div(new(big.Int).Mul(header.BaseFee, big.NewInt(3)), big.NewInt(2))

	gasFeeCap := new(big.Int).Add(overestimatedBasefee, gasTipCap)

	// The estimated gas limits performed by RawTransact fail semi-regularly
	// with out of gas exceptions. To remedy this we extract the internal calls
	// to perform gas price/gas limit estimation here and add a buffer to
	// account for any network variability.
	gasLimit, err := rpcClient.EstimateGas(ctx, ethereum.CallMsg{
		From:      fromAddress,
		To:        tx.To(),
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Value:     nil,
		Data:      tx.Data(),
	})

	if err != nil {
		return nil, err
	}

	opts, err := t.txSigner.GetTransactOpts(ctx, tx.ChainId())
	if err != nil {
		return nil, fmt.Errorf("EstimateGasPriceAndLimitAndSendTx: cannot create transactOpts: %w", err)
	}
	opts.Nonce = new(big.Int).SetUint64(tx.Nonce())
	opts.GasTipCap = gasTipCap
	opts.GasFeeCap = gasFeeCap
	opts.GasLimit = addGasBuffer(gasLimit)

	contract := bind.NewBoundContract(*tx.To(), abi.ABI{}, rpcClient, rpcClient, rpcClient)

	t.logger.Sugar().Infof("EstimateGasPriceAndLimitAndSendTx: sending txn (%s) with gasTipCap=%v gasFeeCap=%v gasLimit=%v", tag, gasTipCap, gasFeeCap, opts.GasLimit)

	tx, err = contract.RawTransact(opts, tx.Data())
	if err != nil {
		return nil, fmt.Errorf("EstimateGasPriceAndLimitAndSendTx: failed to send txn (%s): %w", tag, err)
	}

	t.logger.Sugar().Infof("EstimateGasPriceAndLimitAndSendTx: sent txn (%s) with hash=%s", tag, tx.Hash().Hex())

	receipt, err := t.ensureTransactionEvaled(ctx, rpcClient, tx, tag)
	if err != nil {
		return nil, err
	}

	return receipt, err
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
	pubKey, err := t.blsSigner.GetPublicKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}
	g2Point := bn254.NewZeroG2Point().AddPublicKey(pubKey)

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
