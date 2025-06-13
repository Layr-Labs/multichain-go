package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/Layr-Labs/crypto-libs/pkg/bn254"
	"github.com/Layr-Labs/multichain-go/pkg/blsSigner"
	"github.com/Layr-Labs/multichain-go/pkg/chainManager"
	"github.com/Layr-Labs/multichain-go/pkg/logger"
	"github.com/Layr-Labs/multichain-go/pkg/operatorTableCalculator"
	"github.com/Layr-Labs/multichain-go/pkg/transport"
	"github.com/Layr-Labs/multichain-go/pkg/txSigner"
	"github.com/ethereum/go-ethereum/common"
	cli "github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

func main() {
	app := &cli.App{
		Name:  "transporter",
		Usage: "EigenLayer multichain operator table transporter",
		Description: `The transporter CLI enables operators to calculate and transport stake table roots 
across multiple blockchain networks in the EigenLayer ecosystem. It supports both 
direct private key signing and secure remote keystore integration.`,
		Version: "1.0.0",
		Authors: []*cli.Author{
			{
				Name:  "EigenLayer",
				Email: "support@eigenlayer.xyz",
			},
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "Enable debug logging",
				EnvVars: []string{"DEBUG"},
			},
			&cli.StringFlag{
				Name:     "cross-chain-registry",
				Aliases:  []string{"ccr"},
				Usage:    "CrossChainRegistry contract address",
				Required: true,
				EnvVars:  []string{"CROSS_CHAIN_REGISTRY_ADDRESS"},
			},
			&cli.StringSliceFlag{
				Name:     "chains",
				Aliases:  []string{"c"},
				Usage:    "Blockchain configurations in format 'chainId:rpcUrl' (e.g., '17000:https://ethereum-holesky-rpc.publicnode.com')",
				Required: true,
				EnvVars:  []string{"CHAINS"},
			},
			// Transaction signing options
			&cli.StringFlag{
				Name:    "tx-private-key",
				Usage:   "Private key for transaction signing (hex format, with or without 0x prefix)",
				EnvVars: []string{"TX_PRIVATE_KEY"},
			},
			&cli.StringFlag{
				Name:    "tx-aws-kms-key-id",
				Usage:   "AWS KMS key ID for transaction signing",
				EnvVars: []string{"TX_AWS_KMS_KEY_ID"},
			},
			&cli.StringFlag{
				Name:    "tx-aws-region",
				Usage:   "AWS region for transaction signing KMS key",
				Value:   "us-east-1",
				EnvVars: []string{"TX_AWS_REGION"},
			},
			// BLS signing options
			&cli.StringFlag{
				Name:    "bls-private-key",
				Usage:   "BLS private key for message signing (hex format, with or without 0x prefix)",
				EnvVars: []string{"BLS_PRIVATE_KEY"},
			},
			&cli.StringFlag{
				Name:    "bls-keystore-json",
				Usage:   "BLS keystore JSON string for message signing",
				EnvVars: []string{"BLS_KEYSTORE_JSON"},
			},
			&cli.StringFlag{
				Name:    "bls-aws-secret-name",
				Usage:   "AWS Secrets Manager secret name containing BLS keystore",
				EnvVars: []string{"BLS_AWS_SECRET_NAME"},
			},
			&cli.StringFlag{
				Name:    "bls-aws-region",
				Usage:   "AWS region for BLS keystore secret",
				Value:   "us-east-1",
				EnvVars: []string{"BLS_AWS_REGION"},
			},
		},
		Commands: []*cli.Command{
			{
				Name:    "transport",
				Aliases: []string{"t"},
				Usage:   "Calculate and transport stake table roots",
				Description: `Calculate stake table roots from operator set data and transport them 
to all configured blockchain networks. This includes both global table roots 
and individual AVS stake tables.`,
				Flags: []cli.Flag{
					&cli.Uint64Flag{
						Name:    "block-number",
						Aliases: []string{"b"},
						Usage:   "Specific block number to use for calculation (defaults to latest)",
						EnvVars: []string{"BLOCK_NUMBER"},
					},
					&cli.BoolFlag{
						Name:    "skip-avs-tables",
						Usage:   "Skip individual AVS stake table transport (only do global root)",
						EnvVars: []string{"SKIP_AVS_TABLES"},
					},
				},
				Action: transportAction,
			},
			{
				Name:    "calculate",
				Aliases: []string{"calc"},
				Usage:   "Calculate stake table root without transporting",
				Description: `Calculate the stake table root for the current state without 
transporting it to any blockchain networks. Useful for testing and verification.`,
				Flags: []cli.Flag{
					&cli.Uint64Flag{
						Name:    "block-number",
						Aliases: []string{"b"},
						Usage:   "Specific block number to use for calculation (defaults to latest)",
						EnvVars: []string{"BLOCK_NUMBER"},
					},
				},
				Action: calculateAction,
			},
		},
		Before: validateFlags,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func validateFlags(c *cli.Context) error {
	// Validate transaction signing configuration
	txPrivateKey := c.String("tx-private-key")
	txKMSKeyID := c.String("tx-aws-kms-key-id")

	if txPrivateKey == "" && txKMSKeyID == "" {
		return fmt.Errorf("must specify either --tx-private-key or --tx-aws-kms-key-id for transaction signing")
	}
	if txPrivateKey != "" && txKMSKeyID != "" {
		return fmt.Errorf("cannot specify both --tx-private-key and --tx-aws-kms-key-id")
	}

	// Validate BLS signing configuration
	blsPrivateKey := c.String("bls-private-key")
	blsKeystoreJSON := c.String("bls-keystore-json")
	blsAWSSecretName := c.String("bls-aws-secret-name")

	blsOptions := 0
	if blsPrivateKey != "" {
		blsOptions++
	}
	if blsKeystoreJSON != "" {
		blsOptions++
	}
	if blsAWSSecretName != "" {
		blsOptions++
	}

	if blsOptions == 0 {
		return fmt.Errorf("must specify one of: --bls-private-key, --bls-keystore-json, or --bls-aws-secret-name for BLS signing")
	}
	if blsOptions > 1 {
		return fmt.Errorf("can only specify one BLS signing option")
	}

	return nil
}

func setupLogger(c *cli.Context) (*zap.Logger, error) {
	return logger.NewLogger(&logger.LoggerConfig{
		Debug: c.Bool("debug"),
	})
}

func setupChainManager(c *cli.Context) (*chainManager.ChainManager, error) {
	cm := chainManager.NewChainManager()

	chains := c.StringSlice("chains")
	for _, chainConfig := range chains {
		parts := strings.SplitN(chainConfig, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid chain configuration: %s (expected format: 'chainId:rpcUrl')", chainConfig)
		}

		chainID := new(big.Int)
		chainID, success := chainID.SetString(parts[0], 10)
		if !success {
			return nil, fmt.Errorf("invalid chain ID: %s", parts[0])
		}

		config := &chainManager.ChainConfig{
			ChainID: chainID.Uint64(),
			RPCUrl:  parts[1],
		}

		if err := cm.AddChain(config); err != nil {
			return nil, fmt.Errorf("failed to add chain %d: %w", config.ChainID, err)
		}
	}

	return cm, nil
}

func setupTransactionSigner(c *cli.Context) (txSigner.ITransactionSigner, error) {
	if privateKey := c.String("tx-private-key"); privateKey != "" {
		return txSigner.NewPrivateKeySigner(privateKey)
	}

	if kmsKeyID := c.String("tx-aws-kms-key-id"); kmsKeyID != "" {
		region := c.String("tx-aws-region")
		return txSigner.NewAWSKMSSigner(kmsKeyID, region)
	}

	return nil, fmt.Errorf("no transaction signing method configured")
}

func setupBLSSigner(c *cli.Context, l *zap.Logger) (blsSigner.IBLSSigner, error) {
	if privateKey := c.String("bls-private-key"); privateKey != "" {
		// Parse private key using crypto-libs
		scheme := bn254.NewScheme()
		genericPk, err := scheme.NewPrivateKeyFromHexString(privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BLS private key: %w", err)
		}
		pk, err := bn254.NewPrivateKeyFromBytes(genericPk.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to convert BLS private key: %w", err)
		}
		return blsSigner.NewInMemoryBLSSigner(pk)
	}

	if keystoreJSON := c.String("bls-keystore-json"); keystoreJSON != "" {
		// For now, return an error as the keystore loader function needs to be implemented
		return nil, fmt.Errorf("BLS keystore JSON loading not yet implemented")
	}

	if secretName := c.String("bls-aws-secret-name"); secretName != "" {
		// For now, return an error as the AWS SM BLS signer needs proper configuration
		return nil, fmt.Errorf("AWS Secrets Manager BLS signing not yet fully implemented")
	}

	return nil, fmt.Errorf("no BLS signing method configured")
}

func setupTransport(c *cli.Context, cm *chainManager.ChainManager, txSig txSigner.ITransactionSigner, blsSig blsSigner.IBLSSigner, l *zap.Logger) (*transport.Transport, *chainManager.Chain, error) {
	// Get the first chain as the primary chain for CrossChainRegistry
	chains := c.StringSlice("chains")
	if len(chains) == 0 {
		return nil, nil, fmt.Errorf("no chains configured")
	}

	// Parse first chain to get primary chain ID
	parts := strings.SplitN(chains[0], ":", 2)
	chainID := new(big.Int)
	chainID, _ = chainID.SetString(parts[0], 10)

	primaryChain, err := cm.GetChainForId(chainID.Uint64())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get primary chain: %w", err)
	}

	// Parse CrossChainRegistry address
	registryAddr := common.HexToAddress(c.String("cross-chain-registry"))

	config := &transport.TransportConfig{
		L1CrossChainRegistryAddress: registryAddr,
	}

	transport, err := transport.NewTransport(config, primaryChain.RPCClient, blsSig, txSig, cm, l)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create transport: %w", err)
	}

	return transport, primaryChain, nil
}

func transportAction(c *cli.Context) error {
	l, err := setupLogger(c)
	if err != nil {
		return fmt.Errorf("failed to setup logger: %w", err)
	}

	cm, err := setupChainManager(c)
	if err != nil {
		return fmt.Errorf("failed to setup chain manager: %w", err)
	}

	txSig, err := setupTransactionSigner(c)
	if err != nil {
		return fmt.Errorf("failed to setup transaction signer: %w", err)
	}

	blsSig, err := setupBLSSigner(c, l)
	if err != nil {
		return fmt.Errorf("failed to setup BLS signer: %w", err)
	}

	stakeTransport, primaryChain, err := setupTransport(c, cm, txSig, blsSig, l)
	if err != nil {
		return fmt.Errorf("failed to setup transport: %w", err)
	}

	ctx := context.Background()

	// Determine block number
	var blockNumber uint64
	if specified := c.Uint64("block-number"); specified != 0 {
		blockNumber = specified
	} else {
		blockNumber, err = primaryChain.RPCClient.BlockNumber(ctx)
		if err != nil {
			return fmt.Errorf("failed to get latest block number: %w", err)
		}
	}

	// Get block details for timestamp
	block, err := primaryChain.RPCClient.BlockByNumber(ctx, big.NewInt(int64(blockNumber)))
	if err != nil {
		return fmt.Errorf("failed to get block %d: %w", blockNumber, err)
	}

	referenceTimestamp := uint32(block.Time())

	l.Sugar().Infow("Starting transport operation",
		"blockNumber", blockNumber,
		"timestamp", referenceTimestamp,
	)

	// Calculate stake table root
	registryAddr := common.HexToAddress(c.String("cross-chain-registry"))
	tableCalc, err := operatorTableCalculator.NewStakeTableRootCalculator(&operatorTableCalculator.Config{
		CrossChainRegistryAddress: registryAddr,
	}, primaryChain.RPCClient, l)
	if err != nil {
		return fmt.Errorf("failed to create stake table calculator: %w", err)
	}

	root, tree, dist, err := tableCalc.CalculateStakeTableRoot(ctx, blockNumber)
	if err != nil {
		return fmt.Errorf("failed to calculate stake table root: %w", err)
	}

	l.Sugar().Infow("Calculated stake table root",
		"root", fmt.Sprintf("%x", root),
		"blockNumber", blockNumber,
	)

	// Transport global table root
	err = stakeTransport.SignAndTransportGlobalTableRoot(root, referenceTimestamp, blockNumber, nil)
	if err != nil {
		return fmt.Errorf("failed to transport global table root: %w", err)
	}

	l.Sugar().Infow("Successfully transported global table root")

	// Transport individual AVS stake tables (unless skipped)
	if !c.Bool("skip-avs-tables") {
		opsets := dist.GetOperatorSets()
		if len(opsets) == 0 {
			l.Sugar().Infow("No operator sets found, skipping AVS stake table transport")
		} else {
			l.Sugar().Infow("Transporting AVS stake tables", "operatorSetCount", len(opsets))
			for _, opset := range opsets {
				err = stakeTransport.SignAndTransportAvsStakeTable(referenceTimestamp, blockNumber, opset, root, tree, dist)
				if err != nil {
					return fmt.Errorf("failed to transport AVS stake table for opset %v: %w", opset, err)
				}
				l.Sugar().Infow("Successfully transported AVS stake table", "operatorSet", opset)
			}
		}
	}

	l.Sugar().Infow("Transport operation completed successfully")
	return nil
}

func calculateAction(c *cli.Context) error {
	l, err := setupLogger(c)
	if err != nil {
		return fmt.Errorf("failed to setup logger: %w", err)
	}

	cm, err := setupChainManager(c)
	if err != nil {
		return fmt.Errorf("failed to setup chain manager: %w", err)
	}

	// Get the first chain as the primary chain for calculation
	chains := c.StringSlice("chains")
	if len(chains) == 0 {
		return fmt.Errorf("no chains configured")
	}

	parts := strings.SplitN(chains[0], ":", 2)
	chainID := new(big.Int)
	chainID, _ = chainID.SetString(parts[0], 10)

	primaryChain, err := cm.GetChainForId(chainID.Uint64())
	if err != nil {
		return fmt.Errorf("failed to get primary chain: %w", err)
	}

	ctx := context.Background()

	// Determine block number
	var blockNumber uint64
	if specified := c.Uint64("block-number"); specified != 0 {
		blockNumber = specified
	} else {
		blockNumber, err = primaryChain.RPCClient.BlockNumber(ctx)
		if err != nil {
			return fmt.Errorf("failed to get latest block number: %w", err)
		}
	}

	l.Sugar().Infow("Starting calculation",
		"blockNumber", blockNumber,
	)

	// Calculate stake table root
	registryAddr := common.HexToAddress(c.String("cross-chain-registry"))
	tableCalc, err := operatorTableCalculator.NewStakeTableRootCalculator(&operatorTableCalculator.Config{
		CrossChainRegistryAddress: registryAddr,
	}, primaryChain.RPCClient, l)
	if err != nil {
		return fmt.Errorf("failed to create stake table calculator: %w", err)
	}

	root, _, dist, err := tableCalc.CalculateStakeTableRoot(ctx, blockNumber)
	if err != nil {
		return fmt.Errorf("failed to calculate stake table root: %w", err)
	}

	// Display results
	opsets := dist.GetOperatorSets()
	fmt.Printf("Stake Table Root: %x\n", root)
	fmt.Printf("Block Number: %d\n", blockNumber)
	fmt.Printf("Tree Leaves: %d\n", len(opsets))
	fmt.Printf("Operator Sets: %d\n", len(opsets))
	for i, opset := range opsets {
		index, _ := dist.GetTableIndex(opset)
		fmt.Printf("  [%d] ID: %d, AVS: %s, Index: %d\n", i, opset.Id, opset.Avs.Hex(), index)
	}

	return nil
}
