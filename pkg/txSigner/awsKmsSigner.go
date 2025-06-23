package txSigner

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"math/big"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// AWSKMSSigner implements ITransactionSigner using AWS KMS
type AWSKMSSigner struct {
	kmsClient *kms.KMS
	keyID     string
	address   common.Address
}

// KMSTransactor wraps AWS KMS operations for transaction signing
type KMSTransactor struct {
	kmsClient *kms.KMS
	keyID     string
	address   common.Address
	chainID   uint32
}

// NewAWSKMSSigner creates a new AWSKMSSigner with the specified KMS key ID and AWS region.
// This constructor establishes a connection to AWS KMS and derives the Ethereum address
// from the public key associated with the specified KMS key.
//
// Parameters:
//   - keyID: The AWS KMS key ID or ARN for signing operations
//   - region: The AWS region where the KMS key is located
//
// Returns:
//   - *AWSKMSSigner: A new AWS KMS signer instance
//   - error: An error if the AWS session cannot be created or the key is invalid
func NewAWSKMSSigner(keyID, region string) (*AWSKMSSigner, error) {
	// Create AWS session
	sessOptions := session.Options{
		SharedConfigState: session.SharedConfigEnable, // Enable ~/.aws/config parsing
	}
	sess, err := session.NewSessionWithOptions(sessOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Create KMS client
	kmsClient := kms.New(sess)

	// Get the public key to derive the Ethereum address
	address, err := getAddressFromKMSKey(kmsClient, keyID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive address from KMS key: %w", err)
	}

	return &AWSKMSSigner{
		kmsClient: kmsClient,
		keyID:     keyID,
		address:   address,
	}, nil
}

// GetTransactOpts returns bind.TransactOpts configured for AWS KMS signing.
// This method implements the ITransactionSigner interface by creating transaction
// options that use AWS KMS for signing operations.
//
// Parameters:
//   - ctx: Context for the transaction operation
//   - chainID: The chain ID for the target blockchain
//
// Returns:
//   - *bind.TransactOpts: Configured transaction options for AWS KMS signing
//   - error: An error if the transaction options cannot be created
func (a *AWSKMSSigner) GetTransactOpts(ctx context.Context, chainID *big.Int) (*bind.TransactOpts, error) {
	// Create KMS transactor
	kmsTransactor := &KMSTransactor{
		kmsClient: a.kmsClient,
		keyID:     a.keyID,
		address:   a.address,
		chainID:   uint32(chainID.Uint64()),
	}

	// Create transact opts with custom signer
	auth := &bind.TransactOpts{
		From:    a.address,
		Signer:  kmsTransactor.SignerFn,
		Context: ctx,
	}

	return auth, nil
}

// GetAddress returns the Ethereum address associated with this KMS key.
// This method implements the ITransactionSigner interface.
//
// Returns:
//   - common.Address: The Ethereum address derived from the KMS key
//   - error: Always returns nil for AWS KMS signers
func (a *AWSKMSSigner) GetAddress() (common.Address, error) {
	return a.address, nil
}

// SignerFn implements the bind.SignerFn signature for KMS signing
func (k *KMSTransactor) SignerFn(address common.Address, tx *types.Transaction) (*types.Transaction, error) {
	if address != k.address {
		return nil, fmt.Errorf("address mismatch: expected %s, got %s", k.address.Hex(), address.Hex())
	}

	// Create the transaction hash for signing
	bigChainID := big.NewInt(int64(k.chainID))
	signer := types.NewEIP155Signer(bigChainID)
	hash := signer.Hash(tx)

	// Sign with KMS
	signature, err := k.signHashWithKMS(hash.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction with KMS: %w", err)
	}

	// Apply signature to transaction
	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		return nil, fmt.Errorf("failed to apply signature to transaction: %w", err)
	}

	return signedTx, nil
}

// signHashWithKMS signs a hash using AWS KMS
func (k *KMSTransactor) signHashWithKMS(hash []byte) ([]byte, error) {
	// Prepare signing input
	input := &kms.SignInput{
		KeyId:            aws.String(k.keyID),
		Message:          hash,
		MessageType:      aws.String("RAW"),
		SigningAlgorithm: aws.String("ECDSA_SHA_256"),
	}

	// Sign with KMS
	result, err := k.kmsClient.Sign(input)
	if err != nil {
		return nil, fmt.Errorf("KMS signing failed: %w", err)
	}

	// Parse the ASN.1 DER signature into r, s values
	r, s, err := parseASN1Signature(result.Signature)
	if err != nil {
		return nil, fmt.Errorf("failed to parse KMS signature: %w", err)
	}

	// Convert to Ethereum signature format (r || s || v)
	signature := make([]byte, 65)
	copy(signature[0:32], r.Bytes())
	copy(signature[32:64], s.Bytes())

	// Calculate recovery ID (v)
	// This requires trying both recovery values and checking which one recovers the correct address
	for v := 0; v < 2; v++ {
		signature[64] = byte(v)
		recovered, err := crypto.SigToPub(hash, signature)
		if err != nil {
			continue
		}
		if crypto.PubkeyToAddress(*recovered) == k.address {
			signature[64] = byte(v + 27) // Ethereum uses 27/28 for v
			return signature, nil
		}
	}

	return nil, fmt.Errorf("failed to determine recovery ID")
}

// getAddressFromKMSKey derives the Ethereum address from a KMS public key
func getAddressFromKMSKey(kmsClient *kms.KMS, keyID string) (common.Address, error) {
	// Get the public key from KMS
	input := &kms.GetPublicKeyInput{
		KeyId: aws.String(keyID),
	}

	result, err := kmsClient.GetPublicKey(input)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to get public key from KMS: %w", err)
	}

	// The result.PublicKey contains DER-encoded public key
	// For AWS KMS ECDSA keys, we need to extract the raw coordinates
	// Skip the DER wrapper and extract the public key coordinates directly
	pubKeyBytes := result.PublicKey

	// For secp256k1 keys, AWS KMS returns a DER-encoded key
	// We need to parse it and extract the raw public key coordinates
	if len(pubKeyBytes) < 65 {
		return common.Address{}, fmt.Errorf("public key too short: %d bytes", len(pubKeyBytes))
	}

	// Try to parse as standard DER first
	pubKeyInterface, err := x509.ParsePKIXPublicKey(pubKeyBytes)
	if err != nil {
		// If standard parsing fails, try to extract raw key from DER structure
		return extractRawPublicKeyFromDER(pubKeyBytes)
	}

	// Assert that it's an ECDSA public key
	ecdsaPubKey, ok := pubKeyInterface.(*ecdsa.PublicKey)
	if !ok {
		return common.Address{}, fmt.Errorf("public key is not ECDSA")
	}

	// Create uncompressed public key bytes (65 bytes: 0x04 + 32 bytes X + 32 bytes Y)
	uncompressedPubKey := make([]byte, 65)
	uncompressedPubKey[0] = 0x04

	// Pad coordinates to 32 bytes each
	xBytes := ecdsaPubKey.X.Bytes()
	yBytes := ecdsaPubKey.Y.Bytes()

	// Ensure we have exactly 32 bytes for each coordinate
	if len(xBytes) > 32 || len(yBytes) > 32 {
		return common.Address{}, fmt.Errorf("public key coordinates too large")
	}

	copy(uncompressedPubKey[33-len(xBytes):33], xBytes)
	copy(uncompressedPubKey[65-len(yBytes):65], yBytes)

	// Now unmarshal using go-ethereum's crypto package
	pubKey, err := crypto.UnmarshalPubkey(uncompressedPubKey)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unmarshal public key: %w", err)
	}

	// Derive Ethereum address
	address := crypto.PubkeyToAddress(*pubKey)
	return address, nil
}

// extractRawPublicKeyFromDER extracts raw public key coordinates from DER when standard parsing fails
func extractRawPublicKeyFromDER(derBytes []byte) (common.Address, error) {
	// AWS KMS returns DER-encoded keys in SubjectPublicKeyInfo format
	// We need to parse the ASN.1 structure to extract the raw public key

	// Simple ASN.1 DER parser for SPKI format
	// SPKI structure: SEQUENCE { algorithm AlgorithmIdentifier, subjectPublicKey BIT STRING }
	if len(derBytes) < 10 {
		return common.Address{}, fmt.Errorf("DER data too short")
	}

	offset := 0

	// Check for SEQUENCE tag (0x30)
	if derBytes[offset] != 0x30 {
		return common.Address{}, fmt.Errorf("expected SEQUENCE tag at start of DER")
	}
	offset++

	// Skip sequence length
	seqLen := int(derBytes[offset])
	if seqLen > 0x80 {
		// Long form length
		lengthBytes := seqLen - 0x80
		if lengthBytes > 4 || offset+lengthBytes >= len(derBytes) {
			return common.Address{}, fmt.Errorf("invalid sequence length encoding")
		}
		offset += lengthBytes + 1
	} else {
		offset++
	}

	// Skip algorithm identifier sequence
	if offset >= len(derBytes) || derBytes[offset] != 0x30 {
		return common.Address{}, fmt.Errorf("expected algorithm identifier SEQUENCE")
	}
	offset++

	algLen := int(derBytes[offset])
	if algLen > 0x80 {
		// Long form length
		lengthBytes := algLen - 0x80
		if lengthBytes > 4 || offset+lengthBytes >= len(derBytes) {
			return common.Address{}, fmt.Errorf("invalid algorithm length encoding")
		}
		offset += lengthBytes + 1
		// Recalculate actual length
		algLen = 0
		for i := 0; i < lengthBytes; i++ {
			algLen = (algLen << 8) + int(derBytes[offset-lengthBytes+i])
		}
	} else {
		offset++
	}

	// Skip algorithm identifier content
	offset += algLen

	// Now we should be at the BIT STRING containing the public key
	if offset >= len(derBytes) || derBytes[offset] != 0x03 {
		return common.Address{}, fmt.Errorf("expected BIT STRING tag for public key")
	}
	offset++

	// Get bit string length
	bitStringLen := int(derBytes[offset])
	if bitStringLen > 0x80 {
		// Long form length
		lengthBytes := bitStringLen - 0x80
		if lengthBytes > 4 || offset+lengthBytes >= len(derBytes) {
			return common.Address{}, fmt.Errorf("invalid bit string length encoding")
		}
		offset++
		bitStringLen = 0
		for i := 0; i < lengthBytes; i++ {
			bitStringLen = (bitStringLen << 8) + int(derBytes[offset+i])
		}
		offset += lengthBytes
	} else {
		offset++
	}

	// Skip unused bits indicator (should be 0x00)
	if offset >= len(derBytes) {
		return common.Address{}, fmt.Errorf("missing unused bits indicator")
	}
	offset++

	// Now we should have the raw public key
	remainingBytes := len(derBytes) - offset
	if remainingBytes < 65 {
		return common.Address{}, fmt.Errorf("insufficient bytes for uncompressed public key: have %d, need 65", remainingBytes)
	}

	// Check for uncompressed key marker (0x04)
	if derBytes[offset] != 0x04 {
		return common.Address{}, fmt.Errorf("expected uncompressed public key marker 0x04, got 0x%02x", derBytes[offset])
	}

	// Extract the 65-byte uncompressed public key
	publicKeyBytes := derBytes[offset : offset+65]

	// Try to unmarshal it
	pubKey, err := crypto.UnmarshalPubkey(publicKeyBytes)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unmarshal extracted public key: %w", err)
	}

	// Derive Ethereum address
	address := crypto.PubkeyToAddress(*pubKey)
	return address, nil
}

// parseASN1Signature parses an ASN.1 DER encoded ECDSA signature into r and s values
func parseASN1Signature(signature []byte) (*big.Int, *big.Int, error) {
	// This is a simplified ASN.1 DER parser for ECDSA signatures
	// In production, you might want to use a more robust ASN.1 parser
	if len(signature) < 6 {
		return nil, nil, fmt.Errorf("signature too short")
	}

	// Skip SEQUENCE tag and length
	offset := 2
	if signature[1] > 0x80 {
		offset += int(signature[1] - 0x80)
	}

	// Parse r value
	if signature[offset] != 0x02 {
		return nil, nil, fmt.Errorf("expected INTEGER tag for r")
	}
	offset++
	rLen := int(signature[offset])
	offset++
	rBytes := signature[offset : offset+rLen]
	r := new(big.Int).SetBytes(rBytes)
	offset += rLen

	// Parse s value
	if signature[offset] != 0x02 {
		return nil, nil, fmt.Errorf("expected INTEGER tag for s")
	}
	offset++
	sLen := int(signature[offset])
	offset++
	sBytes := signature[offset : offset+sLen]
	s := new(big.Int).SetBytes(sBytes)

	return r, s, nil
}
