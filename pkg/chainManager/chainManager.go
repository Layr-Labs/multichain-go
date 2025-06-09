package chainManager

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	ErrChainNotFound = errors.New("chain not found")
)

type IChainManager interface {
	AddChain(cfg *ChainConfig) error
	GetChainForId(chainId uint32) (*Chain, error)
}

type ChainConfig struct {
	ChainID uint32
	RPCUrl  string
}

type Chain struct {
	config    *ChainConfig
	RPCClient *ethclient.Client
}

type ChainManager struct {
	chains map[uint32]*Chain
}

func NewChainManager() *ChainManager {
	return &ChainManager{
		chains: make(map[uint32]*Chain),
	}
}

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

func (cm *ChainManager) GetChainForId(chainId uint32) (*Chain, error) {
	chain, exists := cm.chains[chainId]
	if !exists {
		return nil, ErrChainNotFound
	}
	return chain, nil
}
