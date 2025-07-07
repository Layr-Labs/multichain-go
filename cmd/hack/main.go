package main

import (
	"context"
	"github.com/Layr-Labs/crypto-libs/pkg/bn254"
	"github.com/Layr-Labs/multichain-go/pkg/blsSigner"
	"github.com/Layr-Labs/multichain-go/pkg/chainManager"
	"github.com/Layr-Labs/multichain-go/pkg/logger"
	"github.com/Layr-Labs/multichain-go/pkg/operatorTableCalculator"
	"github.com/Layr-Labs/multichain-go/pkg/transport"
	"github.com/Layr-Labs/multichain-go/pkg/txSigner"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"math/big"
)

var (
	crossChainRegistryAddress = common.HexToAddress("0xe850D8A178777b483D37fD492a476e3E6004C816")
	transporterPrivateKey     = "<key>"
	blsPrivateKey             = "<key>"
)

func main() {
	l, err := logger.NewLogger(&logger.LoggerConfig{Debug: true})
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	cm := chainManager.NewChainManager()

	sepoliaConfig := &chainManager.ChainConfig{
		ChainID: 11155111,
		RPCUrl:  "https://ethereum-sepolia-rpc.publicnode.com",
	}
	if err := cm.AddChain(sepoliaConfig); err != nil {
		l.Sugar().Fatalf("Failed to add chain: %v", err)
	}
	sepoliaClient, err := cm.GetChainForId(sepoliaConfig.ChainID)
	if err != nil {
		l.Sugar().Fatalf("Failed to get chain for ID %d: %v", sepoliaConfig.ChainID, err)
	}

	baseSepoliaConfig := &chainManager.ChainConfig{
		ChainID: 84532,
		RPCUrl:  "https://base-sepolia-rpc.publicnode.com",
	}
	if err := cm.AddChain(baseSepoliaConfig); err != nil {
		l.Sugar().Fatalf("Failed to add chain: %v", err)
	}

	txSign, err := txSigner.NewPrivateKeySigner(transporterPrivateKey)
	if err != nil {
		l.Sugar().Fatalf("Failed to create private key signer: %v", err)
	}

	tableCalc, err := operatorTableCalculator.NewStakeTableRootCalculator(&operatorTableCalculator.Config{
		CrossChainRegistryAddress: crossChainRegistryAddress,
	}, sepoliaClient.RPCClient, l)
	if err != nil {
		l.Sugar().Fatalf("Failed to create StakeTableRootCalculator: %v", err)
	}

	block, err := sepoliaClient.RPCClient.BlockByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		l.Sugar().Fatalf("Failed to get block number: %v", err)
	}

	root, tree, dist, err := tableCalc.CalculateStakeTableRoot(ctx, block.NumberU64())
	if err != nil {
		l.Sugar().Fatalf("Failed to calculate stake table root: %v", err)
	}

	pk, err := bn254.NewPrivateKeyFromHexString(blsPrivateKey)
	if err != nil {
		l.Sugar().Fatalf("Failed to convert BLS private key: %v", err)
	}

	inMemSigner, err := blsSigner.NewInMemoryBLSSigner(pk)
	if err != nil {
		l.Sugar().Fatalf("Failed to create in-memory BLS signer: %v", err)
	}

	stakeTransport, err := transport.NewTransport(
		&transport.TransportConfig{
			L1CrossChainRegistryAddress: crossChainRegistryAddress,
		},
		sepoliaClient.RPCClient,
		inMemSigner,
		txSign,
		cm,
		l,
	)
	if err != nil {
		l.Sugar().Fatalf("Failed to create transport: %v", err)
	}

	referenceTimestamp := uint32(block.Time())

	err = stakeTransport.SignAndTransportGlobalTableRoot(
		ctx,
		root,
		referenceTimestamp,
		block.NumberU64(),
		nil,
	)
	if err != nil {
		l.Sugar().Fatalf("Failed to sign and transport global table root: %v", err)
	}
	l.Sugar().Infow("Successfully signed and transported global table root to all chains")

	opsets := dist.GetOperatorSets()
	if len(opsets) == 0 {
		l.Sugar().Infow("No operator sets found, skipping AVS stake table transport")
		return
	}
	for _, opset := range opsets {
		err = stakeTransport.SignAndTransportAvsStakeTable(
			ctx,
			referenceTimestamp,
			block.NumberU64(),
			opset,
			root,
			tree,
			dist,
			nil,
		)
		if err != nil {
			l.Sugar().Fatalf("Failed to sign and transport AVS stake table for opset %v: %v", opset, err)
		} else {
			l.Sugar().Infof("Successfully signed and transported AVS stake table for opset %v", opset)
		}
	}
}
