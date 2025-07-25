package chainManager

import (
	"context"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
)

// EthClientInterface defines the methods needed for blockchain operations.
// This interface allows for mocking and testing while maintaining compatibility
// with ethclient.Client and the go-ethereum bind package.
type EthClientInterface interface {
	// Block operations
	BlockNumber(ctx context.Context) (uint64, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)

	// Gas operations
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)

	// Transaction operations
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)

	// Contract binding support (required for go-ethereum's bind package)
	bind.ContractBackend
	bind.ContractCaller
	bind.ContractTransactor
	bind.ContractFilterer
	bind.DeployBackend
}
