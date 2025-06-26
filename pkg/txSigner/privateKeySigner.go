package txSigner

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// PrivateKeySigner implements ITransactionSigner using a raw private key
type PrivateKeySigner struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
}

// NewPrivateKeySigner creates a new PrivateKeySigner from a hex-encoded private key.
// The private key can be provided with or without the "0x" prefix.
//
// Parameters:
//   - privateKeyHex: A hex-encoded private key string (with or without 0x prefix)
//
// Returns:
//   - *PrivateKeySigner: A new private key signer instance
//   - error: An error if the private key cannot be parsed
func NewPrivateKeySigner(privateKeyHex string) (*PrivateKeySigner, error) {
	// Remove 0x prefix if present
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Derive the address from the private key
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	return &PrivateKeySigner{
		privateKey: privateKey,
		address:    address,
	}, nil
}

// GetTransactOpts returns bind.TransactOpts configured for the private key signer.
// This method implements the ITransactionSigner interface by creating transaction
// options that use the stored private key for signing operations.
//
// Parameters:
//   - ctx: Context for the transaction operation
//   - chainID: The chain ID for the target blockchain
//
// Returns:
//   - *bind.TransactOpts: Configured transaction options for the private key
//   - error: An error if the transactor cannot be created
func (p *PrivateKeySigner) GetTransactOpts(ctx context.Context, chainID *big.Int) (*bind.TransactOpts, error) {
	auth, err := bind.NewKeyedTransactorWithChainID(p.privateKey, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	auth.Context = ctx

	return auth, nil
}

func (p *PrivateKeySigner) GetNoSendTransactOpts(ctx context.Context, chainID *big.Int) (*bind.TransactOpts, error) {
	auth, err := p.GetTransactOpts(ctx, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transact opts: %w", err)
	}
	auth.NoSend = true

	return auth, nil
}

// GetAddress returns the Ethereum address associated with this private key.
// This method implements the ITransactionSigner interface.
//
// Returns:
//   - common.Address: The Ethereum address derived from the private key
//   - error: Always returns nil for private key signers
func (p *PrivateKeySigner) GetAddress() (common.Address, error) {
	return p.address, nil
}
