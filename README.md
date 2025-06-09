# multichain-go

A Go library for multichain operations related to EigenLayer, specifically focused on stake table calculations for operator sets across different chains.

## Overview

This library provides utilities for calculating stake table roots by aggregating operator data from multiple chains and operator sets. It integrates with EigenLayer's CrossChainRegistry and OperatorTableCalculator contracts to compute Merkle roots of operator stake tables.

## Features

- **Stake Table Calculation**: Calculate aggregated stake table roots from multiple operator sets
- **Cross-Chain Integration**: Interface with EigenLayer's cross-chain registry system
- **Smart Contract Bindings**: Auto-generated Go bindings for EigenLayer contracts
- **Merkle Tree Support**: Efficient Merkle tree construction using keccak256 hashing

## Installation

```bash
go get github.com/Layr-Labs/multichain-go
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/Layr-Labs/multichain-go/pkg/stakeTableCalculator"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/ethclient"
    "go.uber.org/zap"
)

func main() {
    // Connect to Ethereum client
    client, err := ethclient.Dial("https://eth-mainnet.alchemyapi.io/v2/your-api-key")
    if err != nil {
        log.Fatal(err)
    }

    // Configure the calculator
    config := &stakeTableCalculator.Config{
        CrossChainRegistryAddress:      common.HexToAddress("0x..."),
        OperatorTableCalculatorAddress: common.HexToAddress("0x..."),
    }

    // Create logger
    logger, _ := zap.NewProduction()
    defer logger.Sync()

    // Initialize calculator
    calculator, err := stakeTableCalculator.NewStakeTableRootCalculator(config, client, logger)
    if err != nil {
        log.Fatal(err)
    }

    // Calculate stake table root for a specific block
    root, err := calculator.CalculateStakeTableRoot(context.Background(), 12345678)
    if err != nil {
        log.Fatal(err)
    }

    logger.Info("Calculated stake table root", zap.Binary("root", root[:]))
}
```

## Development

### Prerequisites

- Go 1.23.6 or later
- `golangci-lint` for linting
- `abigen` for generating contract bindings
- Git submodules for contract dependencies

### Setup

```bash
# Clone the repository
git clone https://github.com/Layr-Labs/multichain-go.git
cd multichain-go

# Install dependencies and initialize submodules
make deps

# Generate contract bindings
make bindings
```

### Building and Testing

```bash
# Run tests
make test

# Lint code
make lint

# Format code
make fmt

# Check formatting
make fmtcheck
```

### Contract Bindings

Smart contract bindings are automatically generated from the EigenLayer contracts using the `compileBindings.sh` script. To regenerate bindings:

```bash
make bindings
```

## Architecture

### Core Components

- **StakeTableCalculator**: Main component for calculating stake table roots
- **Config**: Configuration for contract addresses and client settings
- **Smart Contract Integration**: Generated bindings for EigenLayer contracts

### Calculation Process

1. Fetch active generation reservations from CrossChainRegistry
2. For each operator set, calculate operator table bytes using respective calculators
3. Build Merkle tree from all operator table roots
4. Return the final Merkle root as a 32-byte array

## CLI Tool Usage

The `transporter` CLI tool provides a command-line interface for calculating and transporting stake table roots across multiple blockchain networks.

### Installation

Build the CLI tool using Make (recommended):

```bash
make build
```

This will create the binary at `./bin/transporter`.

Or build manually:

```bash
go build ./cmd/transporter
```

Or run directly:

```bash
go run ./cmd/transporter [command] [options]
```

### Commands

#### `calculate` - Calculate stake table root without transporting

Calculate the stake table root for verification and testing purposes without actually transporting it to any blockchain networks.

```bash
go run ./cmd/transporter calculate [options]
```

#### `transport` - Calculate and transport stake table roots

Calculate stake table roots and transport them to all configured blockchain networks. This includes both global table roots and individual AVS stake tables.

```bash
go run ./cmd/transporter transport [options]
```

### Configuration Options

#### Required Flags

- `--cross-chain-registry` / `--ccr` - CrossChainRegistry contract address
- `--chains` / `-c` - Blockchain configurations in format `chainId:rpcUrl` (can be specified multiple times)

#### Transaction Signing (choose one)

- `--tx-private-key` - Private key for transaction signing (hex format, with or without 0x prefix)
- `--tx-aws-kms-key-id` - AWS KMS key ID for transaction signing
- `--tx-aws-region` - AWS region for transaction signing KMS key (default: "us-east-1")

#### BLS Signing (choose one)

- `--bls-private-key` - BLS private key for message signing (hex format)
- `--bls-keystore-json` - BLS keystore JSON string for message signing *(not yet implemented)*
- `--bls-aws-secret-name` - AWS Secrets Manager secret name containing BLS keystore *(not yet implemented)*
- `--bls-aws-region` - AWS region for BLS keystore secret (default: "us-east-1")

#### Optional Flags

- `--debug` / `-d` - Enable debug logging
- `--block-number` / `-b` - Specific block number to use for calculation (defaults to latest)
- `--skip-avs-tables` - Skip individual AVS stake table transport (only do global root, transport command only)

### Environment Variables

All flags can be set using environment variables:

- `CROSS_CHAIN_REGISTRY_ADDRESS`
- `CHAINS`
- `TX_PRIVATE_KEY`
- `TX_AWS_KMS_KEY_ID`
- `TX_AWS_REGION`
- `BLS_PRIVATE_KEY`
- `BLS_KEYSTORE_JSON`
- `BLS_AWS_SECRET_NAME`
- `BLS_AWS_REGION`
- `DEBUG`
- `BLOCK_NUMBER`
- `SKIP_AVS_TABLES`

### Usage Examples

#### Basic Calculation

```bash
go run ./cmd/transporter calculate \
  --cross-chain-registry "0x0022d2014901F2AFBF5610dDFcd26afe2a65Ca6F" \
  --chains "17000:https://ethereum-holesky-rpc.publicnode.com" \
  --tx-private-key "0x..." \
  --bls-private-key "0x..." \
  --debug
```

#### Transport with Private Keys

```bash
go run ./cmd/transporter transport \
  --cross-chain-registry "0x0022d2014901F2AFBF5610dDFcd26afe2a65Ca6F" \
  --chains "17000:https://ethereum-holesky-rpc.publicnode.com" \
  --tx-private-key "0x..." \
  --bls-private-key "0x..." \
  --block-number 1000000 \
  --debug
```

#### Transport with AWS KMS for Transaction Signing

```bash
go run ./cmd/transporter transport \
  --cross-chain-registry "0x0022d2014901F2AFBF5610dDFcd26afe2a65Ca6F" \
  --chains "17000:https://ethereum-holesky-rpc.publicnode.com" \
  --tx-aws-kms-key-id "your-kms-key-id" \
  --tx-aws-region "us-east-1" \
  --bls-private-key "0x..." \
  --debug
```

#### Multiple Chains

```bash
go run ./cmd/transporter transport \
  --cross-chain-registry "0x0022d2014901F2AFBF5610dDFcd26afe2a65Ca6F" \
  --chains "17000:https://ethereum-holesky-rpc.publicnode.com" \
  --chains "1:https://eth-mainnet.alchemyapi.io/v2/your-api-key" \
  --tx-private-key "0x..." \
  --bls-private-key "0x..." \
  --debug
```

#### Environment Variable Configuration

```bash
export CROSS_CHAIN_REGISTRY_ADDRESS="0x0022d2014901F2AFBF5610dDFcd26afe2a65Ca6F"
export CHAINS="17000:https://ethereum-holesky-rpc.publicnode.com"
export TX_PRIVATE_KEY="0x..."
export BLS_PRIVATE_KEY="0x..."
export DEBUG=true

go run ./cmd/transporter calculate
```

#### Skip AVS Tables (Global Root Only)

```bash
go run ./cmd/transporter transport \
  --cross-chain-registry "0x0022d2014901F2AFBF5610dDFcd26afe2a65Ca6F" \
  --chains "17000:https://ethereum-holesky-rpc.publicnode.com" \
  --tx-private-key "0x..." \
  --bls-private-key "0x..." \
  --skip-avs-tables \
  --debug
```

### Security Notes

- **Private Keys**: Never commit private keys to version control. Use environment variables or secure key management systems.
- **AWS KMS**: For production use, AWS KMS provides enhanced security for transaction signing.
- **AWS Secrets Manager**: For BLS keys, AWS Secrets Manager integration is planned for secure key storage.

### Help

Get help for any command:

```bash
go run ./cmd/transporter --help
go run ./cmd/transporter transport --help
go run ./cmd/transporter calculate --help
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please ensure your code passes all tests and linting before submitting a PR:

```bash
make test lint fmtcheck
```