package blsSigner

import (
	"fmt"

	"github.com/Layr-Labs/crypto-libs/pkg/bn254"
)

// InMemoryBLSSigner implements IBLSSigner using an in-memory BLS private key.
// This implementation stores the BLS private key in memory and provides
// fast signing operations suitable for development and testing environments.
// For production use with enhanced security, consider using AWS HSM implementations.
type InMemoryBLSSigner struct {
	privateKey *bn254.PrivateKey
	publicKey  *bn254.PublicKey
}

// NewInMemoryBLSSigner creates a new InMemoryBLSSigner from a BN254 private key.
// The provided private key is stored in memory and used for all signing operations.
// The corresponding public key is automatically derived and cached.
//
// Parameters:
//   - privateKey: A BN254 private key from the crypto-libs package
//
// Returns:
//   - *InMemoryBLSSigner: A new signer instance
//   - error: An error if the private key is nil or invalid
func NewInMemoryBLSSigner(privateKey *bn254.PrivateKey) (*InMemoryBLSSigner, error) {
	if privateKey == nil {
		return nil, fmt.Errorf("private key cannot be nil")
	}

	// Derive the public key from the private key
	publicKey := privateKey.Public()

	return &InMemoryBLSSigner{
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// SignBytes signs the given data using the BLS private key.
// This method implements the IBLSSigner interface and provides BLS signature
// functionality for the provided data bytes.
//
// Parameters:
//   - data: The byte array to be signed
//
// Returns:
//   - *bn254.Signature: The BLS signature of the data
//   - error: An error if signing fails or private key is invalid
func (s *InMemoryBLSSigner) SignBytes(data [32]byte) (*bn254.Signature, error) {
	if s.privateKey == nil {
		return nil, fmt.Errorf("private key is nil")
	}

	return s.privateKey.SignSolidityCompatible(data)
}

// GetPublicKey returns the public key associated with this signer.
// This method implements the IBLSSigner interface and returns the BLS public key
// that corresponds to the private key used for signing.
//
// Returns:
//   - *bn254.PublicKey: The BLS public key for signature verification
func (s *InMemoryBLSSigner) GetPublicKey() (*bn254.PublicKey, error) {
	return s.publicKey, nil
}
