package txSigner

import (
	"context"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

// ITransactionSigner defines the interface for signing Ethereum transactions
type ITransactionSigner interface {
	// GetTransactOpts returns bind.TransactOpts configured for the signer
	GetTransactOpts(ctx context.Context, chainID uint32) (*bind.TransactOpts, error)

	// GetAddress returns the address associated with this signer
	GetAddress() (common.Address, error)
}
