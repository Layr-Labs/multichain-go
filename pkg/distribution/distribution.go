package distribution

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
)

type OperatorSet struct {
	Id  uint32
	Avs common.Address
}

type Distribution struct {
	tableIndices map[OperatorSet]uint64
	tableData    map[OperatorSet][]byte
}

func NewDistribution() *Distribution {
	return &Distribution{
		tableIndices: make(map[OperatorSet]uint64),
	}
}

func NewDistributionWithOperatorSets(operatorSets []OperatorSet) *Distribution {
	dist := NewDistribution()
	for i, opset := range operatorSets {
		dist.tableIndices[opset] = uint64(i)
	}
	return dist
}

func (d *Distribution) GetTableIndex(opset OperatorSet) (uint64, bool) {
	index, ok := d.tableIndices[opset]
	return index, ok
}

func (d *Distribution) SetTableData(opset OperatorSet, data []byte) error {
	if _, exists := d.tableIndices[opset]; !exists {
		return fmt.Errorf("operator set %s with ID %d does not exist in the distribution", opset.Avs.String(), opset.Id)
	}
	d.tableData[opset] = data
	return nil
}

func (d *Distribution) GetTableData(opset OperatorSet) ([]byte, bool) {
	data, ok := d.tableData[opset]
	return data, ok
}

func (d *Distribution) GetOrderedOperatorSets() []OperatorSet {
	sets := make([]OperatorSet, 0, len(d.tableIndices))
	// iterate over the map and set the opset at the index stored as the value in the array
	for opset, index := range d.tableIndices {
		sets[index] = opset
	}
	return sets
}
