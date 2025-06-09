// Package chainManager provides blockchain connection management for multichain operations.
// This package manages connections to multiple Ethereum-compatible blockchains,
// providing a unified interface for interacting with different chains in the
// EigenLayer multichain ecosystem.
package chainManager

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	// ErrChainNotFound is returned when a requested chain ID is not found in the manager
	ErrChainNotFound = errors.New("chain not found")
)

// IChainManager defines the interface for managing blockchain connections.
// Implementations provide the ability to add new chains and retrieve
// existing chain connections by their chain ID.
type IChainManager interface {
	// AddChain adds a new blockchain connection to the manager
	AddChain(cfg *ChainConfig) error
	// GetChainForId retrieves a chain connection by its chain ID
	GetChainForId(chainId uint64) (*Chain, error)
}

// ChainConfig holds the configuration for connecting to a blockchain.
// This includes the chain ID and the RPC URL for establishing connections.
type ChainConfig struct {
	// ChainID is the unique identifier for the blockchain network
	ChainID uint64
	// RPCUrl is the URL endpoint for connecting to the blockchain RPC
	RPCUrl string
}

// Chain represents an active connection to a blockchain.
// It contains both the configuration and the active RPC client connection.
type Chain struct {
	config *ChainConfig
	// RPCClient is the active Ethereum client connection for this chain
	RPCClient *ethclient.Client
}

// ChainManager implements IChainManager and manages multiple blockchain connections.
// It maintains a registry of active chains indexed by their chain IDs.
type ChainManager struct {
	chains map[uint64]*Chain
}

// NewChainManager creates a new ChainManager instance.
// The manager is initialized with an empty chain registry.
//
// Returns:
//   - *ChainManager: A new chain manager instance
func NewChainManager() *ChainManager {
	return &ChainManager{
		chains: make(map[uint64]*Chain),
	}
}

// AddChain adds a new blockchain connection to the manager.
// This method establishes a connection to the specified RPC URL and
// stores the resulting chain connection for future use.
//
// Parameters:
//   - cfg: The chain configuration containing chain ID and RPC URL
//
// Returns:
//   - error: An error if the chain already exists or connection fails
func (cm *ChainManager) AddChain(cfg *ChainConfig) error {
	if _, exists := cm.chains[cfg.ChainID]; exists {
		return fmt.Errorf("chain with ID %d already exists", cfg.ChainID)
	}
	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("failed to connect to RPC URL %s: %w", cfg.RPCUrl, err)
	}
	cm.chains[cfg.ChainID] = &Chain{
		config:    cfg,
		RPCClient: client,
	}
	return nil
}

// GetChainForId retrieves a chain connection by its chain ID.
// This method looks up an existing chain connection in the manager's registry.
//
// Parameters:
//   - chainId: The chain ID to look up
//
// Returns:
//   - *Chain: The chain connection if found
//   - error: ErrChainNotFound if the chain ID is not registered
func (cm *ChainManager) GetChainForId(chainId uint64) (*Chain, error) {
	chain, exists := cm.chains[chainId]
	if !exists {
		return nil, ErrChainNotFound
	}
	return chain, nil
}
