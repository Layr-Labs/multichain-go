// Package distribution provides operator set management and table data distribution
// for EigenLayer multichain operations. This package manages the mapping between
// operator sets and their corresponding table data and indices in Merkle trees.
package distribution

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
)

// OperatorSet represents a unique operator set in the EigenLayer ecosystem.
// It consists of an ID and the address of the associated AVS (Actively Validated Service).
type OperatorSet struct {
	// Id is the unique identifier for the operator set
	Id uint32
	// Avs is the Ethereum address of the Actively Validated Service
	Avs common.Address
}

// Distribution manages the organization and storage of operator set data.
// It maintains both the ordering of operator sets (for Merkle tree construction)
// and the associated table data for each operator set.
type Distribution struct {
	// TableIndices maps operator sets to their index positions in ordered structures
	TableIndices map[OperatorSet]uint64
	// tableData stores the table bytes for each operator set
	tableData map[OperatorSet][]byte
}

// NewDistribution creates a new empty Distribution instance.
// The distribution is initialized with empty maps for both indices and data.
//
// Returns:
//   - *Distribution: A new empty distribution instance
func NewDistribution() *Distribution {
	return &Distribution{
		TableIndices: make(map[OperatorSet]uint64),
		tableData:    make(map[OperatorSet][]byte),
	}
}

// NewDistributionWithOperatorSets creates a new Distribution with pre-defined operator sets.
// The operator sets are assigned indices based on their order in the provided slice.
//
// Parameters:
//   - operatorSets: A slice of operator sets to initialize the distribution
//
// Returns:
//   - *Distribution: A new distribution instance with the operator sets configured
func NewDistributionWithOperatorSets(operatorSets []OperatorSet) *Distribution {
	dist := NewDistribution()
	dist.SetOperatorSets(operatorSets)
	return dist
}

// SetOperatorSets assigns indices to the provided operator sets.
// Each operator set is assigned an index based on its position in the slice,
// which determines its position in Merkle tree structures.
//
// Parameters:
//   - operatorSets: A slice of operator sets to assign indices to
func (d *Distribution) SetOperatorSets(operatorSets []OperatorSet) {
	for i, opset := range operatorSets {
		d.TableIndices[opset] = uint64(i)
	}
}

// GetTableIndex retrieves the index position for a given operator set.
// This index is used for Merkle tree construction and proof generation.
//
// Parameters:
//   - opset: The operator set to look up
//
// Returns:
//   - uint64: The index position of the operator set
//   - bool: True if the operator set exists, false otherwise
func (d *Distribution) GetTableIndex(opset OperatorSet) (uint64, bool) {
	index, ok := d.TableIndices[opset]
	return index, ok
}

// SetTableData stores table data for a specific operator set.
// The operator set must already exist in the distribution before data can be set.
//
// Parameters:
//   - opset: The operator set to store data for
//   - data: The table data bytes to store
//
// Returns:
//   - error: An error if the operator set doesn't exist in the distribution
func (d *Distribution) SetTableData(opset OperatorSet, data []byte) error {
	if _, exists := d.TableIndices[opset]; !exists {
		return fmt.Errorf("operator set %s with ID %d does not exist in the distribution", opset.Avs.String(), opset.Id)
	}
	d.tableData[opset] = data
	return nil
}

// GetTableData retrieves the stored table data for a given operator set.
//
// Parameters:
//   - opset: The operator set to retrieve data for
//
// Returns:
//   - []byte: The table data bytes for the operator set
//   - bool: True if data exists for the operator set, false otherwise
func (d *Distribution) GetTableData(opset OperatorSet) ([]byte, bool) {
	data, ok := d.tableData[opset]
	return data, ok
}

// GetOrderedOperatorSets returns operator sets in their index order.
// This method returns operator sets ordered by their assigned indices,
// which is important for maintaining consistent Merkle tree construction.
//
// Returns:
//   - []OperatorSet: A slice of operator sets ordered by their indices
func (d *Distribution) GetOrderedOperatorSets() []OperatorSet {
	sets := make([]OperatorSet, len(d.TableIndices))
	// iterate over the map and set the opset at the index stored as the value in the array
	for opset, index := range d.TableIndices {
		sets[index] = opset
	}
	return sets
}

// GetOperatorSets returns all operator sets in the distribution.
// This method returns operator sets in arbitrary order, suitable for
// iteration when order doesn't matter.
//
// Returns:
//   - []OperatorSet: A slice of all operator sets in the distribution
func (d *Distribution) GetOperatorSets() []OperatorSet {
	sets := make([]OperatorSet, 0, len(d.TableIndices))
	for opset := range d.TableIndices {
		sets = append(sets, opset)
	}
	return sets
}
