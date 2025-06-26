// Package txSigner provides Ethereum transaction signing functionality for multichain operations.
// This package defines interfaces and implementations for signing Ethereum transactions
// using various methods including direct private keys and AWS KMS integration.
package txSigner

import (
	"context"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

// ITransactionSigner defines the interface for signing Ethereum transactions.
// Implementations provide the ability to create properly configured transaction
// options for use with go-ethereum contract bindings, supporting different
// signing backends like private keys and hardware security modules.
type ITransactionSigner interface {
	// GetTransactOpts returns bind.TransactOpts configured for the signer.
	// The returned TransactOpts contains the necessary authentication and
	// signing configuration for submitting transactions to the specified chain.
	//
	// Parameters:
	//   - ctx: Context for the operation
	//   - chainID: The chain ID for the target blockchain
	//
	// Returns:
	//   - *bind.TransactOpts: Configured transaction options for the signer
	//   - error: An error if transaction options cannot be created
	GetTransactOpts(ctx context.Context, chainID *big.Int) (*bind.TransactOpts, error)

	GetNoSendTransactOpts(ctx context.Context, chainID *big.Int) (*bind.TransactOpts, error)

	// GetAddress returns the Ethereum address associated with this signer.
	// This address will be used as the 'from' field in transactions.
	//
	// Returns:
	//   - common.Address: The Ethereum address of the signer
	//   - error: An error if the address cannot be determined
	GetAddress() (common.Address, error)
}
