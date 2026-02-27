// Package operatorTableCalculator provides stake table root calculation functionality
// for EigenLayer multichain operations. This package handles the calculation of
// Merkle tree roots from operator set data across multiple chains, integrating
// with EigenLayer's CrossChainRegistry to fetch active operator reservations.
package operatorTableCalculator

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Layr-Labs/multichain-go/pkg/chainManager"
	"github.com/Layr-Labs/multichain-go/pkg/distribution"
	"github.com/Layr-Labs/multichain-go/pkg/util"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.uber.org/zap"

	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/ICrossChainRegistry"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	merkletree "github.com/wealdtech/go-merkletree/v2"
	"github.com/wealdtech/go-merkletree/v2/keccak256"
)

// CrossChainRegistryCallerInterface defines the interface for interacting with the CrossChainRegistry contract
type CrossChainRegistryCallerInterface interface {
	GetActiveGenerationReservationCount(opts *bind.CallOpts) (*big.Int, error)
	GetActiveGenerationReservationsByRange(opts *bind.CallOpts, startIndex *big.Int, endIndex *big.Int) ([]ICrossChainRegistry.OperatorSet, error)
	CalculateOperatorTableBytes(opts *bind.CallOpts, operatorSet ICrossChainRegistry.OperatorSet) ([]byte, error)
}

// Config holds the configuration for the StakeTableCalculator.
type Config struct {
	CrossChainRegistryAddress common.Address
}

// StakeTableCalculator is responsible for calculating the cloud operator table root.
type StakeTableCalculator struct {
	config                   *Config
	ethClient                chainManager.EthClientInterface
	logger                   *zap.Logger
	crossChainRegistryCaller CrossChainRegistryCallerInterface
}

// NewStakeTableRootCalculator creates a new instance of StakeTableCalculator.
func NewStakeTableRootCalculator(cfg *Config, ec chainManager.EthClientInterface, l *zap.Logger) (*StakeTableCalculator, error) {
	registryCaller, err := ICrossChainRegistry.NewICrossChainRegistryCaller(cfg.CrossChainRegistryAddress, ec)
	if err != nil {
		return nil, fmt.Errorf("failed to bind NewICrossChainRegistryCaller: %w", err)
	}

	return &StakeTableCalculator{
		config:                   cfg,
		ethClient:                ec,
		logger:                   l,
		crossChainRegistryCaller: registryCaller,
	}, nil
}

// NewStakeTableRootCalculatorWithRegistryCaller creates a new instance of StakeTableCalculator with a pre-bound registry caller.
func NewStakeTableRootCalculatorWithRegistryCaller(cfg *Config, ec chainManager.EthClientInterface, registryCaller CrossChainRegistryCallerInterface, l *zap.Logger) (*StakeTableCalculator, error) {
	return &StakeTableCalculator{
		config:                   cfg,
		ethClient:                ec,
		logger:                   l,
		crossChainRegistryCaller: registryCaller,
	}, nil
}

// CalculateStakeTableRoot performs the complete calculation for a given reference block.
func (c *StakeTableCalculator) CalculateStakeTableRoot(
	ctx context.Context,
	referenceBlockNumber uint64,
) (
	[32]byte,
	*merkletree.MerkleTree,
	*distribution.Distribution,
	error,
) {
	var zeroRoot [32]byte // Return in case of error or no data

	callOpts := &bind.CallOpts{
		Context:     ctx,
		BlockNumber: new(big.Int).SetUint64(referenceBlockNumber),
	}

	opsetsWithCalculators, err := c.fetchActiveGenerationReservationsPaginated(callOpts)
	if err != nil {
		return zeroRoot, nil, nil, fmt.Errorf("failed to fetch active generation reservations: %w", err)
	}

	c.logger.Sugar().Infow("Fetched active generation reservations",
		zap.Any("opsets", opsetsWithCalculators),
		zap.Uint64("referenceBlockNumber", referenceBlockNumber),
	)

	dist := distribution.NewDistribution()

	if len(opsetsWithCalculators) == 0 {
		c.logger.Sugar().Infow("No calculators registered for this block, global table root will be zero.")
		return zeroRoot, nil, dist, nil
	}

	allOpsets := util.Map(opsetsWithCalculators, func(opset ICrossChainRegistry.OperatorSet, i uint64) distribution.OperatorSet {
		return distribution.OperatorSet{
			Id:  opset.Id,
			Avs: opset.Avs,
		}
	})

	// Calculate table bytes for each opset, skipping any whose calculator reverts.
	// A single malicious/broken AVS calculator must not block all other opsets.
	var successfulOpsets []distribution.OperatorSet
	var opsetTableRoots [][]byte
	var opsetTableBytes [][]byte

	for i, opset := range allOpsets {
		c.logger.Sugar().Infow("Calculating operator table bytes for opset",
			zap.Uint32("opsetId", opset.Id),
			zap.String("opsetAvs", opset.Avs.String()),
		)

		tableBytes, err := c.crossChainRegistryCaller.CalculateOperatorTableBytes(&bind.CallOpts{
			Context:     ctx,
			BlockNumber: new(big.Int).SetUint64(referenceBlockNumber),
		}, opsetsWithCalculators[i])
		if err != nil {
			c.logger.Sugar().Errorw("Skipping opset: CalculateOperatorTableBytes reverted",
				zap.Uint32("opsetId", opset.Id),
				zap.String("opsetAvs", opset.Avs.String()),
				zap.Error(err),
			)
			continue
		}
		c.logger.Sugar().Infow("Got operator table bytes for opset",
			zap.Uint32("opsetId", opset.Id),
			zap.String("opsetAvs", opset.Avs.String()),
			zap.String("bytes", hexutil.Encode(tableBytes)),
		)

		encodedLeaf := distribution.EncodeOperatorTableLeaf(tableBytes)
		c.logger.Sugar().Infow("Encoded operator table leaf for opset",
			zap.Uint32("opsetId", opset.Id),
			zap.String("opsetAvs", opset.Avs.String()),
			zap.String("encodedLeaf", hexutil.Encode(encodedLeaf)),
		)

		successfulOpsets = append(successfulOpsets, opset)
		opsetTableRoots = append(opsetTableRoots, encodedLeaf)
		opsetTableBytes = append(opsetTableBytes, tableBytes)
	}

	if len(successfulOpsets) == 0 {
		c.logger.Sugar().Warnw("All operator set calculators failed, global table root will be zero.")
		return zeroRoot, nil, dist, nil
	}

	if len(successfulOpsets) < len(allOpsets) {
		c.logger.Sugar().Warnw("Some operator set calculators failed, proceeding with successful ones only",
			zap.Int("total", len(allOpsets)),
			zap.Int("successful", len(successfulOpsets)),
			zap.Int("failed", len(allOpsets)-len(successfulOpsets)),
		)
	}

	dist.SetOperatorSets(successfulOpsets)
	for i, opset := range successfulOpsets {
		err = dist.SetTableData(opset, opsetTableBytes[i])
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
		zap.String("root", hexutil.Encode(merkleRoot[:])),
		zap.Uint64("referenceBlockNumber", referenceBlockNumber),
	)
	return [32]byte(merkleRoot), tree, dist, nil
}

// fetchActiveGenerationReservationsPaginated fetches active generation reservations using pagination.
func (c *StakeTableCalculator) fetchActiveGenerationReservationsPaginated(
	callOpts *bind.CallOpts,
) ([]ICrossChainRegistry.OperatorSet, error) {
	// Get the total count of generation reservations
	totalCount, err := c.crossChainRegistryCaller.GetActiveGenerationReservationCount(callOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch generation reservation count: %w", err)
	}

	if totalCount.Cmp(big.NewInt(0)) == 0 {
		return []ICrossChainRegistry.OperatorSet{}, nil
	}

	const pageSize = 50
	var allReservations []ICrossChainRegistry.OperatorSet

	for startIndex := uint64(0); startIndex < totalCount.Uint64(); startIndex += pageSize {
		endIndex := startIndex + pageSize
		if endIndex > totalCount.Uint64() {
			endIndex = totalCount.Uint64()
		}

		pageReservations, err := c.crossChainRegistryCaller.GetActiveGenerationReservationsByRange(
			callOpts,
			new(big.Int).SetUint64(startIndex),
			new(big.Int).SetUint64(endIndex),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch active generation reservations for range [%d, %d): %w", startIndex, endIndex, err)
		}

		allReservations = append(allReservations, pageReservations...)
	}

	return allReservations, nil
}
