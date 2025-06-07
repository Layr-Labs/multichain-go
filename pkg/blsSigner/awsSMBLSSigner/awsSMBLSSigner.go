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

type AWSSMBLSSignerConfig struct {
	Region     string
	SecretName string
}

type AWSSMBLSSigner struct {
	logger *zap.Logger
	config *AWSSMBLSSignerConfig
}

func NewAWSSMBLSSigner(logger *zap.Logger) *AWSSMBLSSigner {
	return &AWSSMBLSSigner{
		logger: logger,
	}
}

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

func (a *AWSSMBLSSigner) SignBytes(data []byte) (*bn254.Signature, error) {
	pk, err := a.getSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	return pk.Sign(data)
}

func (a *AWSSMBLSSigner) GetPublicKey() (*bn254.PublicKey, error) {
	pk, err := a.getSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}
	return pk.Public(), nil
}
