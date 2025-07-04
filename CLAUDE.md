# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common Commands

### Dependencies and Setup
```bash
make deps         # Install Go dependencies and update git submodules
make deps/go      # Only install Go dependencies (go mod tidy)
make bindings     # Generate Go bindings from smart contracts
make build        # Build CLI tool to ./bin/transporter
```

### Testing and Code Quality
```bash
make test         # Run all tests with verbose output
make lint         # Run golangci-lint with 5-minute timeout
make fmt          # Format all Go code with gofmt
make fmtcheck     # Check if code is properly formatted
```

## Architecture Overview

This is a Go library for multichain operations related to EigenLayer, specifically focused on stake table calculations for operator sets across different chains.

### Core Components

- **OperatorTableCalculator** (`pkg/operatorTableCalculator/`): The main component that calculates stake table roots by:
  1. Fetching active generation reservations from the CrossChainRegistry
  2. Calculating operator table bytes for each operator set using their respective calculators
  3. Building a Merkle tree from the operator table roots
  4. Returning the final Merkle root as a 32-byte array

- **Transport** (`pkg/transport/`): Handles signing and transporting stake table data with BLS signatures
- **Distribution** (`pkg/distribution/`): Manages operator set indexing and table data storage
- **BLS Signer** (`pkg/blsSigner/`): Provides BLS signature functionality including AWS HSM support
- **Transporter** (`cmd/transporter/`): Currently a placeholder main package

### Smart Contract Integration

The library integrates with EigenLayer contracts through generated Go bindings:
- `ICrossChainRegistry`: Manages cross-chain operator set registrations
- `IOperatorTableCalculator`: Calculates operator table data for specific operator sets
- `IOperatorTableUpdater`: Updates operator table data on-chain with BLS signatures

Contract bindings are generated using the `compileBindings.sh` script which:
1. Builds contracts using Forge in the `modules/eigenlayer-middleware` submodule
2. Generates Go bindings using `abigen` for each contract

### Key Dependencies

- `github.com/ethereum/go-ethereum`: Ethereum client and utilities
- `github.com/wealdtech/go-merkletree/v2`: Merkle tree implementation with keccak256 hashing
- `go.uber.org/zap`: Structured logging
- `github.com/Layr-Labs/eigenlayer-contracts`: EigenLayer smart contract bindings
- `github.com/Layr-Labs/crypto-libs`: BLS signature and BN254 curve utilities
- `github.com/aws/aws-sdk-go`: AWS SDK for HSM-based BLS signing

## Development Notes

- The project uses Git submodules for smart contract dependencies
- All Go code should be formatted with `gofmt` before committing
- Tests run with count=1 to disable caching and ensure fresh runs
- Linting is enforced with a 5-minute timeout for comprehensive checks
- Always run the tests and linter when as you develop to ensure code quality
