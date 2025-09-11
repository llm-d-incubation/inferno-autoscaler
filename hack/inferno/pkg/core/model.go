package core

import (
	"fmt"

	"github.com/atantawi/inferno-hack/pkg/config"
)

// An inference model
type Model struct {
	name string

	// model performance data for specified accelerators
	perfData map[string]*config.ModelAcceleratorPerfData

	// number of accelerator instances needed to fit a model on a given accelerator
	numInstances map[string]int
}

func NewModel(name string) *Model {
	return &Model{
		name:         name,
		perfData:     make(map[string]*config.ModelAcceleratorPerfData),
		numInstances: make(map[string]int),
	}
}

// Calculate basic parameters
func (m *Model) Calculate(accelerators map[string]*Accelerator) {
	// add any operations here
}

func (m *Model) Name() string {
	return m.name
}

func (m *Model) NumInstances(acceleratorName string) int {
	return m.numInstances[acceleratorName]
}

func (m *Model) PerfData(acceleratorName string) *config.ModelAcceleratorPerfData {
	return m.perfData[acceleratorName]
}

func (m *Model) AddPerfDataFromSpec(spec *config.ModelAcceleratorPerfData) {
	if spec.Name == m.name {
		m.perfData[spec.Acc] = spec
		var count int
		if count = spec.AccCount; count <= 0 {
			count = 1
		}
		m.numInstances[spec.Acc] = count
	}
}

func (m *Model) RemovePerfData(accName string) {
	delete(m.perfData, accName)
}

func (m *Model) Spec() *config.ModelData {
	md := &config.ModelData{
		PerfData: make([]config.ModelAcceleratorPerfData, len(m.perfData)),
	}
	i := 0
	for _, pd := range m.perfData {
		md.PerfData[i] = *pd
		i++
	}
	return md
}

func (m *Model) String() string {
	return fmt.Sprintf("Model: name=%s; numInstances=%v",
		m.name, m.numInstances)
}
