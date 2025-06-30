package txSigner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	bind2 "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"math/big"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var secp256k1N = crypto.S256().Params().N
var secp256k1HalfN = new(big.Int).Div(secp256k1N, big.NewInt(2))

// AWSKMSSigner implements ITransactionSigner using AWS KMS
type AWSKMSSigner struct {
	kmsClient *kms.KMS
	keyID     string
	publicKey *ecdsa.PublicKey
	address   common.Address
}

// NewAWSKMSSigner creates a new AWSKMSSigner with the specified KMS key ID and AWS region.
// This constructor establishes a connection to AWS KMS and derives the Ethereum address
// by performing a test signature and recovery operation.
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

	signer := &AWSKMSSigner{
		kmsClient: kmsClient,
		keyID:     keyID,
	}
	pubKey, err := signer.GetPubKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get public key from KMS: %w", err)
	}

	keyAddr := crypto.PubkeyToAddress(*pubKey)

	signer.address = keyAddr
	signer.publicKey = pubKey

	return signer, nil
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
	// Create transact opts with custom signer
	auth := &bind.TransactOpts{
		From:    a.address,
		Signer:  a.SignerFn(chainID),
		Context: ctx,
	}

	return auth, nil
}

func (a *AWSKMSSigner) GetNoSendTransactOpts(ctx context.Context, chainID *big.Int) (*bind.TransactOpts, error) {
	auth, err := a.GetTransactOpts(ctx, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transact opts: %w", err)
	}
	auth.NoSend = true

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
func (k *AWSKMSSigner) SignerFn(chainID *big.Int) bind2.SignerFn {
	return func(address common.Address, tx *types.Transaction) (*types.Transaction, error) {
		pubKeyBytes := secp256k1.S256().Marshal(k.publicKey.X, k.publicKey.Y)

		signer := types.LatestSignerForChainID(chainID)

		if address != k.address {
			return nil, fmt.Errorf("address mismatch: expected %s, got %s", k.address.Hex(), address.Hex())
		}

		txHashBytes := signer.Hash(tx).Bytes()

		rBytes, sBytes, err := k.getSignatureFromKms(txHashBytes)
		if err != nil {
			return nil, err
		}

		// Adjust S value from signature according to Ethereum standard
		sBigInt := new(big.Int).SetBytes(sBytes)
		if sBigInt.Cmp(secp256k1HalfN) > 0 {
			sBytes = new(big.Int).Sub(secp256k1N, sBigInt).Bytes()
		}

		signature, err := k.getEthereumSignature(pubKeyBytes, txHashBytes, rBytes, sBytes)
		if err != nil {
			return nil, err
		}

		return tx.WithSignature(signer, signature)
	}
}

func (k *AWSKMSSigner) getPublicKeyDerBytesFromKMS() ([]byte, error) {
	getPubKeyOutput, err := k.kmsClient.GetPublicKey(&kms.GetPublicKeyInput{
		KeyId: aws.String(k.keyID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get public key from KMS: %w", err)
	}

	var asn1pubk asn1EcPublicKey
	_, err = asn1.Unmarshal(getPubKeyOutput.PublicKey, &asn1pubk)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ASN.1 public key: %w", err)
	}

	return asn1pubk.PublicKey.Bytes, nil
}

func (k *AWSKMSSigner) GetPubKey() (*ecdsa.PublicKey, error) {
	pkBytes, err := k.getPublicKeyDerBytesFromKMS()
	if err != nil {
		return nil, fmt.Errorf("failed to get public key bytes from KMS: %w", err)
	}
	pubkey, err := crypto.UnmarshalPubkey(pkBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal public key: %w", err)
	}
	return pubkey, nil
}

func (k *AWSKMSSigner) getSignatureFromKms(txHashBytes []byte) ([]byte, []byte, error) {
	signInput := &kms.SignInput{
		KeyId:            aws.String(k.keyID),
		SigningAlgorithm: aws.String("ECDSA_SHA_256"),
		MessageType:      aws.String("DIGEST"),
		Message:          txHashBytes,
	}

	signOutput, err := k.kmsClient.Sign(signInput)
	if err != nil {
		return nil, nil, err
	}

	var sigAsn1 asn1EcSig
	_, err = asn1.Unmarshal(signOutput.Signature, &sigAsn1)
	if err != nil {
		return nil, nil, err
	}

	return sigAsn1.R.Bytes, sigAsn1.S.Bytes, nil
}

func (k *AWSKMSSigner) getEthereumSignature(expectedPublicKeyBytes []byte, txHash []byte, r []byte, s []byte) ([]byte, error) {
	rsSignature := append(adjustSignatureLength(r), adjustSignatureLength(s)...)
	signature := append(rsSignature, []byte{0}...)

	recoveredPublicKeyBytes, err := crypto.Ecrecover(txHash, signature)
	if err != nil {
		return nil, err
	}

	if hex.EncodeToString(recoveredPublicKeyBytes) != hex.EncodeToString(expectedPublicKeyBytes) {
		signature = append(rsSignature, []byte{1}...)
		recoveredPublicKeyBytes, err = crypto.Ecrecover(txHash, signature)
		if err != nil {
			return nil, err
		}

		if hex.EncodeToString(recoveredPublicKeyBytes) != hex.EncodeToString(expectedPublicKeyBytes) {
			return nil, fmt.Errorf("recovered public key does not match expected public key")
		}
	}

	return signature, nil
}
func adjustSignatureLength(buffer []byte) []byte {
	buffer = bytes.TrimLeft(buffer, "\x00")
	for len(buffer) < 32 {
		zeroBuf := []byte{0}
		buffer = append(zeroBuf, buffer...)
	}
	return buffer
}

// ASN.1 structures matching the reference implementation
type asn1EcSig struct {
	R asn1.RawValue
	S asn1.RawValue
}

type asn1EcPublicKey struct {
	EcPublicKeyInfo asn1EcPublicKeyInfo
	PublicKey       asn1.BitString
}

type asn1EcPublicKeyInfo struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.ObjectIdentifier
}
