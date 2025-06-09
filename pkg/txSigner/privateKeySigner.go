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

// NewPrivateKeySigner creates a new PrivateKeySigner from a hex-encoded private key
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

// GetTransactOpts returns bind.TransactOpts configured for the private key signer
func (p *PrivateKeySigner) GetTransactOpts(ctx context.Context, chainID uint32) (*bind.TransactOpts, error) {
	bigChainId := big.NewInt(int64(chainID))
	auth, err := bind.NewKeyedTransactorWithChainID(p.privateKey, bigChainId)
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	auth.Context = ctx

	return auth, nil
}

// GetAddress returns the address associated with this private key
func (p *PrivateKeySigner) GetAddress() (common.Address, error) {
	return p.address, nil
}
