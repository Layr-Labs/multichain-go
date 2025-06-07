package signer

import "github.com/Layr-Labs/crypto-libs/pkg/bn254"

type ISigner interface {
	SignBytes(data []byte) (*bn254.Signature, error)
	GetPublicKey() *bn254.PublicKey
}
