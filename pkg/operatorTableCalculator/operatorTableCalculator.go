package operatorTableCalculator

import (
	"context"
	"fmt"
	"github.com/Layr-Labs/multichain-go/pkg/distribution"
	"github.com/Layr-Labs/multichain-go/pkg/util"
	"go.uber.org/zap"
	"math/big"

	// For prepareSignatureDigest, but that's in the outer generator
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/ICrossChainRegistry"
	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/IOperatorTableCalculator"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/wealdtech/go-merkletree/v2"
	"github.com/wealdtech/go-merkletree/v2/keccak256"
)

// Config holds the configuration for the StakeTableCalculator.
type Config struct {
	CrossChainRegistryAddress      common.Address
	OperatorTableCalculatorAddress common.Address
}

// StakeTableCalculator is responsible for calculating the cloud operator table root.
type StakeTableCalculator struct {
	config                   *Config
	ethClient                *ethclient.Client
	logger                   *zap.Logger
	crossChainRegistryCaller *ICrossChainRegistry.ICrossChainRegistryCaller
}

// NewStakeTableRootCalculator creates a new instance of StakeTableCalculator.
func NewStakeTableRootCalculator(cfg *Config, ec *ethclient.Client, l *zap.Logger) (*StakeTableCalculator, error) {
	registryCaller, err := ICrossChainRegistry.NewICrossChainRegistryCaller(cfg.CrossChainRegistryAddress, ec)
	if err != nil {
		return nil, fmt.Errorf("failed to bind NewICrossChainRegistryCaller: %w", err)
	}

	l.Sugar().Infow("StakeTableCalculator initialized with registry", zap.String("registryAddress", cfg.OperatorTableCalculatorAddress.Hex()))

	return &StakeTableCalculator{
		config:                   cfg,
		ethClient:                ec,
		logger:                   l,
		crossChainRegistryCaller: registryCaller,
	}, nil
}

func (c *StakeTableCalculator) getOpsetCalculatorCaller(opset ICrossChainRegistry.OperatorSet, address common.Address) (*IOperatorTableCalculator.IOperatorTableCalculatorCaller, error) {
	caller, err := IOperatorTableCalculator.NewIOperatorTableCalculatorCaller(address, c.ethClient)
	if err != nil {
		return nil, fmt.Errorf("failed to bind IOperatorTableCalculatorCaller for opset %d at address %s: %w", opset, address.Hex(), err)
	}
	return caller, nil
}

// CalculateStakeTableRoot performs the complete calculation for a given reference block.
func (c *StakeTableCalculator) CalculateStakeTableRoot(ctx context.Context, referenceBlockNumber uint64) ([32]byte, *merkletree.MerkleTree, *distribution.Distribution, error) {
	var zeroRoot [32]byte // Return in case of error or no data

	opsetsWithCalculators, calculatorAddrs, err := c.crossChainRegistryCaller.GetActiveGenerationReservations(&bind.CallOpts{
		Context:     ctx,
		BlockNumber: new(big.Int).SetUint64(referenceBlockNumber),
	})
	if err != nil {
		return zeroRoot, nil, nil, fmt.Errorf("failed to fetch active generation reservations: %w", err)
	}

	c.logger.Sugar().Infow("Fetched active generation reservations",
		zap.Int("opsetCount", len(opsetsWithCalculators)),
		zap.Uint64("referenceBlockNumber", referenceBlockNumber),
	)

	if len(calculatorAddrs) == 0 {
		c.logger.Sugar().Infow("No calculators registered for this block, cloud root will be zero.")
		return zeroRoot, nil, nil, nil // TODO: Define a proper empty tree root
	}

	dist := distribution.NewDistributionWithOperatorSets(util.Map(opsetsWithCalculators, func(opset ICrossChainRegistry.OperatorSet, i uint64) distribution.OperatorSet {
		return distribution.OperatorSet{
			Id:  opset.Id,
			Avs: common.HexToAddress(opset.Avs.Hex()),
		}
	}))

	opsetTableRoots := make([][]byte, len(opsetsWithCalculators))
	for i, opset := range opsetsWithCalculators {
		calcAddr := calculatorAddrs[i]

		calc, err := c.getOpsetCalculatorCaller(opset, calcAddr)
		if err != nil {
			return zeroRoot, nil, nil, fmt.Errorf("failed to get opset calculator caller for opset %d: %w", opset, err)
		}

		tableBytes, err := calc.CalculateOperatorTableBytes(&bind.CallOpts{
			Context:     ctx,
			BlockNumber: new(big.Int).SetUint64(referenceBlockNumber),
		}, IOperatorTableCalculator.OperatorSet(opset))
		if err != nil {
			return zeroRoot, nil, nil, fmt.Errorf("failed to calculate operator table bytes for opset %d: %w", opset, err)
		}
		opsetTableRoots[i] = tableBytes
		err = dist.SetTableData(distribution.OperatorSet{
			Id:  opset.Id,
			Avs: common.HexToAddress(opset.Avs.Hex()),
		}, tableBytes)
		if err != nil {
			return zeroRoot, nil, nil, fmt.Errorf("failed to set table data for opset %d: %w", opset, err)
		}
	}

	tree, err := merkletree.NewTree(
		merkletree.WithData(opsetTableRoots),
		merkletree.WithHashType(keccak256.New()),
	)
	if err != nil {
		return zeroRoot, nil, nil, fmt.Errorf("calculator: failed to create merkle tree: %w", err)
	}

	merkleRoot := tree.Root()

	c.logger.Sugar().Infow("calculated stake table root",
		zap.ByteString("root", merkleRoot[:]),
		zap.Uint64("referenceBlockNumber", referenceBlockNumber),
	)
	return [32]byte(merkleRoot), tree, dist, nil
}
