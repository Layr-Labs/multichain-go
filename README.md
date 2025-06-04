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