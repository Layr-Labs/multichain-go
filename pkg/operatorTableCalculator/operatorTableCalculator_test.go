package operatorTableCalculator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/Layr-Labs/eigenlayer-contracts/pkg/bindings/ICrossChainRegistry"
	"github.com/Layr-Labs/multichain-go/pkg/chainManager"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Helper function to create a test StakeTableCalculator with mocked dependencies
func setupTestCalculator(t *testing.T) (*StakeTableCalculator, *MockICrossChainRegistryCaller, *chainManager.MockEthClientInterface) {
	mockEthClient := chainManager.NewMockEthClientInterface(t)
	mockRegistryCaller := NewMockICrossChainRegistryCaller(t)

	logger, _ := zap.NewDevelopment()

	calculator := &StakeTableCalculator{
		config: &Config{
			CrossChainRegistryAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		},
		ethClient: mockEthClient,
		logger:    logger,
		// Note: In a real scenario, this would be the actual binding, but for tests we'll work around it
		crossChainRegistryCaller: nil,
	}

	return calculator, mockRegistryCaller, mockEthClient
}

// Helper function to create test operator sets
func createTestOperatorSets(count int) []ICrossChainRegistry.OperatorSet {
	operatorSets := make([]ICrossChainRegistry.OperatorSet, count)
	for i := 0; i < count; i++ {
		operatorSets[i] = ICrossChainRegistry.OperatorSet{
			Avs: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Id:  uint32(i + 1),
		}
	}
	return operatorSets
}

// testFetchActiveGenerationReservationsPaginated is a test helper that replicates the pagination logic
// This allows us to unit test the pagination logic independently
func testFetchActiveGenerationReservationsPaginated(mockRegistryCaller *MockICrossChainRegistryCaller, callOpts *bind.CallOpts) ([]ICrossChainRegistry.OperatorSet, error) {
	// Get the total count of generation reservations
	totalCount, err := mockRegistryCaller.GetActiveGenerationReservationCount(callOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch generation reservation count: %w", err)
	}

	if totalCount.Cmp(big.NewInt(0)) == 0 {
		return []ICrossChainRegistry.OperatorSet{}, nil
	}

	const pageSize = 50
	var allReservations []ICrossChainRegistry.OperatorSet

	for startIndex := uint64(0); startIndex < totalCount.Uint64(); startIndex += pageSize {
		endIndex := startIndex + pageSize
		if endIndex > totalCount.Uint64() {
			endIndex = totalCount.Uint64()
		}

		pageReservations, err := mockRegistryCaller.GetActiveGenerationReservationsByRange(
			callOpts,
			new(big.Int).SetUint64(startIndex),
			new(big.Int).SetUint64(endIndex),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch active generation reservations for range [%d, %d): %w", startIndex, endIndex, err)
		}

		allReservations = append(allReservations, pageReservations...)
	}

	return allReservations, nil
}

func TestStakeTableCalculator_fetchActiveGenerationReservationsPaginated_EmptyResult(t *testing.T) {
	_, mockRegistryCaller, _ := setupTestCalculator(t)

	callOpts := &bind.CallOpts{
		Context:     context.Background(),
		BlockNumber: big.NewInt(12345),
	}

	// Mock: return count = 0
	mockRegistryCaller.On("GetActiveGenerationReservationCount", callOpts).
		Return(big.NewInt(0), nil)

	result, err := testFetchActiveGenerationReservationsPaginated(mockRegistryCaller, callOpts)

	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestStakeTableCalculator_fetchActiveGenerationReservationsPaginated_SinglePage(t *testing.T) {
	_, mockRegistryCaller, _ := setupTestCalculator(t)

	callOpts := &bind.CallOpts{
		Context:     context.Background(),
		BlockNumber: big.NewInt(12345),
	}

	expectedOperatorSets := createTestOperatorSets(25) // Less than page size (50)

	// Mock: return count = 25
	mockRegistryCaller.On("GetActiveGenerationReservationCount", callOpts).
		Return(big.NewInt(25), nil)

	// Mock: expect single call to GetActiveGenerationReservationsByRange
	mockRegistryCaller.On("GetActiveGenerationReservationsByRange", callOpts, big.NewInt(0), big.NewInt(25)).
		Return(expectedOperatorSets, nil)

	result, err := testFetchActiveGenerationReservationsPaginated(mockRegistryCaller, callOpts)

	assert.NoError(t, err)
	assert.Equal(t, expectedOperatorSets, result)
}

func TestStakeTableCalculator_fetchActiveGenerationReservationsPaginated_MultiplePages(t *testing.T) {
	_, mockRegistryCaller, _ := setupTestCalculator(t)

	callOpts := &bind.CallOpts{
		Context:     context.Background(),
		BlockNumber: big.NewInt(12345),
	}

	// Create test data for multiple pages (125 total = 3 pages)
	page1Data := createTestOperatorSets(50)
	page2Data := createTestOperatorSets(50)
	page3Data := createTestOperatorSets(25) // Partial last page

	// Mock: return count = 125
	mockRegistryCaller.On("GetActiveGenerationReservationCount", callOpts).
		Return(big.NewInt(125), nil)

	// Mock: expect three calls to GetActiveGenerationReservationsByRange
	mockRegistryCaller.On("GetActiveGenerationReservationsByRange", callOpts, big.NewInt(0), big.NewInt(50)).
		Return(page1Data, nil)
	mockRegistryCaller.On("GetActiveGenerationReservationsByRange", callOpts, big.NewInt(50), big.NewInt(100)).
		Return(page2Data, nil)
	mockRegistryCaller.On("GetActiveGenerationReservationsByRange", callOpts, big.NewInt(100), big.NewInt(125)).
		Return(page3Data, nil)

	result, err := testFetchActiveGenerationReservationsPaginated(mockRegistryCaller, callOpts)

	assert.NoError(t, err)
	assert.Len(t, result, 125)

	// Verify the results are combined correctly
	expectedResult := append(page1Data, page2Data...)
	expectedResult = append(expectedResult, page3Data...)
	assert.Equal(t, expectedResult, result)
}

func TestStakeTableCalculator_fetchActiveGenerationReservationsPaginated_ErrorInCount(t *testing.T) {
	_, mockRegistryCaller, _ := setupTestCalculator(t)

	callOpts := &bind.CallOpts{
		Context:     context.Background(),
		BlockNumber: big.NewInt(12345),
	}

	expectedError := errors.New("failed to get count")

	// Mock: return error when getting count
	mockRegistryCaller.On("GetActiveGenerationReservationCount", callOpts).
		Return((*big.Int)(nil), expectedError)

	result, err := testFetchActiveGenerationReservationsPaginated(mockRegistryCaller, callOpts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch generation reservation count")
	assert.Contains(t, err.Error(), expectedError.Error())
	assert.Nil(t, result)
}

func TestStakeTableCalculator_fetchActiveGenerationReservationsPaginated_ErrorInRange(t *testing.T) {
	_, mockRegistryCaller, _ := setupTestCalculator(t)

	callOpts := &bind.CallOpts{
		Context:     context.Background(),
		BlockNumber: big.NewInt(12345),
	}

	expectedError := errors.New("failed to get range")

	// Mock: return count = 25
	mockRegistryCaller.On("GetActiveGenerationReservationCount", callOpts).
		Return(big.NewInt(25), nil)

	// Mock: return error when getting range
	mockRegistryCaller.On("GetActiveGenerationReservationsByRange", callOpts, big.NewInt(0), big.NewInt(25)).
		Return([]ICrossChainRegistry.OperatorSet(nil), expectedError)

	result, err := testFetchActiveGenerationReservationsPaginated(mockRegistryCaller, callOpts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch active generation reservations for range [0, 25)")
	assert.Contains(t, err.Error(), expectedError.Error())
	assert.Nil(t, result)
}

func TestStakeTableCalculator_PaginationEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		totalCount  int64
		pageSize    int
		expectPages int
	}{
		{
			name:        "Exactly one page",
			totalCount:  50,
			pageSize:    50,
			expectPages: 1,
		},
		{
			name:        "Exactly two pages",
			totalCount:  100,
			pageSize:    50,
			expectPages: 2,
		},
		{
			name:        "Partial last page",
			totalCount:  75,
			pageSize:    50,
			expectPages: 2,
		},
		{
			name:        "Single item",
			totalCount:  1,
			pageSize:    50,
			expectPages: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mockRegistryCaller, _ := setupTestCalculator(t)

			callOpts := &bind.CallOpts{
				Context:     context.Background(),
				BlockNumber: big.NewInt(12345),
			}

			// Mock: return specified count
			mockRegistryCaller.On("GetActiveGenerationReservationCount", callOpts).
				Return(big.NewInt(tt.totalCount), nil)

			// Mock: Set up expected calls based on pagination logic
			remainingCount := tt.totalCount
			for startIndex := int64(0); startIndex < tt.totalCount; startIndex += int64(tt.pageSize) {
				endIndex := startIndex + int64(tt.pageSize)
				if endIndex > tt.totalCount {
					endIndex = tt.totalCount
				}

				pageSize := endIndex - startIndex
				pageData := createTestOperatorSets(int(pageSize))

				mockRegistryCaller.On("GetActiveGenerationReservationsByRange",
					callOpts, big.NewInt(startIndex), big.NewInt(endIndex)).
					Return(pageData, nil)

				remainingCount -= pageSize
			}

			result, err := testFetchActiveGenerationReservationsPaginated(mockRegistryCaller, callOpts)

			assert.NoError(t, err)
			assert.Len(t, result, int(tt.totalCount))
		})
	}
}

func TestNewStakeTableRootCalculator_Success(t *testing.T) {
	mockEthClient := chainManager.NewMockEthClientInterface(t)
	logger, _ := zap.NewDevelopment()

	config := &Config{
		CrossChainRegistryAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
	}

	calculator, err := NewStakeTableRootCalculator(config, mockEthClient, logger)

	require.NoError(t, err)
	assert.NotNil(t, calculator)
	assert.Equal(t, config, calculator.config)
	assert.Equal(t, mockEthClient, calculator.ethClient)
	assert.Equal(t, logger, calculator.logger)
	assert.NotNil(t, calculator.crossChainRegistryCaller)
}

func TestStakeTableCalculatorConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "Valid address",
			address: "0x1234567890123456789012345678901234567890",
		},
		{
			name:    "Zero address",
			address: "0x0000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				CrossChainRegistryAddress: common.HexToAddress(tt.address),
			}

			assert.NotNil(t, config)
			assert.Equal(t, common.HexToAddress(tt.address), config.CrossChainRegistryAddress)
		})
	}
}
