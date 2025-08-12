// Package chainManager provides blockchain connection management for multichain operations.
// This package manages connections to multiple Ethereum-compatible blockchains,
// providing a unified interface for interacting with different Chains in the
// EigenLayer multichain ecosystem.
package chainManager

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
	"sync"
)

var (
	// ErrChainNotFound is returned when a requested chain ID is not found in the manager
	ErrChainNotFound = errors.New("chain not found")
)

// IChainManager defines the interface for managing blockchain connections.
// Implementations provide the ability to add new Chains and retrieve
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
	// RPCClient is the active client connection for this chain
	RPCClient EthClientInterface
}

// ChainManager implements IChainManager and manages multiple blockchain connections.
// It maintains a registry of active Chains indexed by their chain IDs.
// This implementation is thread-safe using sync.Map for concurrent access.
type ChainManager struct {
	Chains sync.Map // map[uint64]*Chain
}

// NewChainManager creates a new ChainManager instance.
// The manager is initialized with an empty chain registry.
//
// Returns:
//   - *ChainManager: A new chain manager instance
func NewChainManager() *ChainManager {
	return &ChainManager{}
}

// AddChain adds a new blockchain connection to the manager.
// This method establishes a connection to the specified RPC URL and
// stores the resulting chain connection for future use.
// This method is thread-safe and can be called concurrently.
//
// Parameters:
//   - cfg: The chain configuration containing chain ID and RPC URL
//
// Returns:
//   - error: An error if the chain already exists or connection fails
func (cm *ChainManager) AddChain(cfg *ChainConfig) error {
	if _, exists := cm.Chains.Load(cfg.ChainID); exists {
		return fmt.Errorf("chain with ID %d already exists", cfg.ChainID)
	}
	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("failed to connect to RPC URL %s: %w", cfg.RPCUrl, err)
	}
	cm.Chains.Store(cfg.ChainID, &Chain{
		config:    cfg,
		RPCClient: client,
	})
	return nil
}

// GetChainForId retrieves a chain connection by its chain ID.
// This method looks up an existing chain connection in the manager's registry.
// This method is thread-safe and can be called concurrently.
//
// Parameters:
//   - chainId: The chain ID to look up
//
// Returns:
//   - *Chain: The chain connection if found
//   - error: ErrChainNotFound if the chain ID is not registered
func (cm *ChainManager) GetChainForId(chainId uint64) (*Chain, error) {
	value, exists := cm.Chains.Load(chainId)
	if !exists {
		return nil, ErrChainNotFound
	}
	chain, ok := value.(*Chain)
	if !ok {
		return nil, fmt.Errorf("invalid chain type stored for ID %d", chainId)
	}
	return chain, nil
}
