// Package awsSMBLSSigner provides AWS Secrets Manager-based BLS signature functionality.
// This package implements the IBLSSigner interface using BLS private keys stored
// securely in AWS Secrets Manager, providing enhanced security for production deployments.
package awsSMBLSSigner

import (
	"fmt"
	"github.com/Layr-Labs/crypto-libs/pkg/bn254"
	"github.com/Layr-Labs/crypto-libs/pkg/keystore"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"go.uber.org/zap"
)

// AWSSMBLSSignerConfig holds the configuration for AWS Secrets Manager BLS signer.
// This configuration specifies the AWS region and secret name containing the BLS private key.
type AWSSMBLSSignerConfig struct {
	// Region specifies the AWS region where the secret is stored
	Region string
	// SecretName is the name of the secret in AWS Secrets Manager containing the BLS keystore
	SecretName string
}

// AWSSMBLSSigner implements IBLSSigner using AWS Secrets Manager for secure key storage.
// This implementation retrieves BLS private keys from AWS Secrets Manager for each operation,
// providing enhanced security by avoiding in-memory key storage.
type AWSSMBLSSigner struct {
	logger *zap.Logger
	config *AWSSMBLSSignerConfig
}

// NewAWSSMBLSSigner creates a new AWSSMBLSSigner instance.
// The signer will retrieve BLS private keys from AWS Secrets Manager as needed.
//
// Parameters:
//   - logger: A zap logger for logging operations and errors
//
// Returns:
//   - *AWSSMBLSSigner: A new AWS Secrets Manager BLS signer instance
func NewAWSSMBLSSigner(logger *zap.Logger) *AWSSMBLSSigner {
	return &AWSSMBLSSigner{
		logger: logger,
	}
}

// getSecret retrieves and parses the BLS private key from AWS Secrets Manager.
// This method establishes an AWS session, retrieves the secret containing the keystore,
// and parses it to extract the BN254 private key.
//
// Returns:
//   - *bn254.PrivateKey: The BLS private key from the keystore
//   - error: An error if retrieval or parsing fails
func (a *AWSSMBLSSigner) getSecret() (*bn254.PrivateKey, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(a.config.Region),
	})
	if err != nil {
		return nil, err
	}

	svc := secretsmanager.New(sess)

	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(a.config.SecretName),
		VersionStage: aws.String("AWSCURRENT"), // Optional: defaults to AWSCURRENT
	}

	// Retrieve the secret
	result, err := svc.GetSecretValue(input)
	if err != nil {
		return nil, err
	}

	if result.SecretString == nil {
		return nil, fmt.Errorf("secret string is nil")
	}
	ks, err := keystore.ParseKeystoreJSON(*result.SecretString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse keystore JSON: %w", err)
	}
	return ks.GetBN254PrivateKey("")
}

// SignBytes signs the provided data using the BLS private key from AWS Secrets Manager.
// This method implements the IBLSSigner interface by retrieving the private key
// from AWS Secrets Manager and using it to sign the provided data.
//
// Parameters:
//   - data: The byte array to be signed
//
// Returns:
//   - *bn254.Signature: The BLS signature of the data
//   - error: An error if key retrieval or signing fails
func (a *AWSSMBLSSigner) SignBytes(data []byte) (*bn254.Signature, error) {
	pk, err := a.getSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	return pk.Sign(data)
}

// GetPublicKey returns the BLS public key corresponding to the private key in AWS Secrets Manager.
// This method retrieves the private key from AWS Secrets Manager and derives the public key.
//
// Returns:
//   - *bn254.PublicKey: The BLS public key for signature verification
//   - error: An error if key retrieval fails
func (a *AWSSMBLSSigner) GetPublicKey() (*bn254.PublicKey, error) {
	pk, err := a.getSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}
	return pk.Public(), nil
}
