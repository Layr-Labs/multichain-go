// Package blsSigner provides BLS signature functionality for EigenLayer multichain operations.
// This package defines interfaces and implementations for signing data using BLS signatures
// on the BN254 curve, which are used for cryptographic proofs in the EigenLayer protocol.
package blsSigner

import "github.com/Layr-Labs/crypto-libs/pkg/bn254"

// IBLSSigner defines the interface for BLS signature operations used in EigenLayer.
// Implementations of this interface provide the ability to sign arbitrary data
// and return the associated public key for verification purposes.
type IBLSSigner interface {
	// SignBytes signs the provided data using BLS signature scheme on the BN254 curve.
	// Returns a BN254 signature that can be verified against the signer's public key.
	SignBytes(data [32]byte) (*bn254.Signature, error)

	// GetPublicKey returns the BLS public key associated with this signer.
	// This public key can be used to verify signatures created by SignBytes.
	GetPublicKey() *bn254.PublicKey
}
