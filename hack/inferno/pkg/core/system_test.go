package core

import (
	"strings"
	"testing"

	"github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/config"
)

func TestNewSystem(t *testing.T) {
	system := NewSystem()

	if system == nil {
		t.Fatal("NewSystem() returned nil")
	}

	if system.accelerators == nil {
		t.Error("accelerators map should be initialized")
	}

	if system.models == nil {
		t.Error("models map should be initialized")
	}

	if system.serviceClasses == nil {
		t.Error("serviceClasses map should be initialized")
	}

	if system.servers == nil {
		t.Error("servers map should be initialized")
	}

	if system.capacity == nil {
		t.Error("capacity map should be initialized")
	}

	if system.allocationByType == nil {
		t.Error("allocationByType map should be initialized")
	}
}

func TestSystem_SetFromSpec(t *testing.T) {
	system := NewSystem()

	spec := &config.SystemSpec{
		Accelerators: config.AcceleratorData{
			Spec: []config.AcceleratorSpec{
				{
					Name: "A100",
					Type: "GPU_A100",
					Power: config.PowerSpec{
						Idle:     50,
						MidPower: 150,
						Full:     350,
						MidUtil:  0.4,
					},
					Cost:         1.0,
					Multiplicity: 1,
					MemSize:      40,
				},
			},
		},
		Models: config.ModelData{
			PerfData: []config.ModelAcceleratorPerfData{
				{
					Name:         "llama-7b",
					Acc:          "A100",
					AccCount:     1,
					MaxBatchSize: 16,
					AtTokens:     100,
					DecodeParms: config.DecodeParms{
						Alpha: 10.0,
						Beta:  2.0,
					},
					PrefillParms: config.PrefillParms{
						Gamma: 5.0,
						Delta: 0.1,
					},
				},
			},
		},
		Capacity: config.CapacityData{
			Count: []config.AcceleratorCount{
				{
					Type:  "GPU_A100",
					Count: 4,
				},
			},
		},
		Servers: config.ServerData{
			Spec: []config.ServerSpec{
				{
					Name:  "server1",
					Model: "llama-7b",
					Class: "default",
					CurrentAlloc: config.AllocationData{
						Load: config.ServerLoadSpec{
							ArrivalRate:  30,
							AvgInTokens:  100,
							AvgOutTokens: 200,
						},
					},
					MinNumReplicas: 1,
					MaxBatchSize:   16,
				},
			},
		},
		ServiceClasses: config.ServiceClassData{
			Spec: []config.ServiceClassSpec{
				{
					Name:     "default",
					Priority: 1,
					ModelTargets: []config.ModelTarget{
						{
							Model:    "llama-7b",
							SLO_ITL:  100,
							SLO_TTFT: 1000,
							SLO_TPS:  50,
						},
					},
				},
			},
		},
		Optimizer: config.OptimizerData{
			Spec: config.OptimizerSpec{
				Unlimited:         false,
				SaturationPolicy:  "None",
				DelayedBestEffort: false,
			},
		},
	}

	optimizerSpec := system.SetFromSpec(spec)

	// Check that components were created
	if len(system.accelerators) != 1 {
		t.Errorf("Expected 1 accelerator, got %d", len(system.accelerators))
	}

	if len(system.models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(system.models))
	}

	if len(system.servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(system.servers))
	}

	if len(system.serviceClasses) != 1 {
		t.Errorf("Expected 1 service class, got %d", len(system.serviceClasses))
	}

	if len(system.capacity) != 1 {
		t.Errorf("Expected 1 capacity entry, got %d", len(system.capacity))
	}

	if optimizerSpec == nil {
		t.Error("Optimizer spec should be returned")
	}
}

func TestSystem_SetAcceleratorsFromSpec(t *testing.T) {
	system := NewSystem()

	acceleratorData := &config.AcceleratorData{
		Spec: []config.AcceleratorSpec{
			{
				Name: "A100",
				Type: "GPU_A100",
				Power: config.PowerSpec{
					Idle:     50,
					MidPower: 150,
					Full:     350,
					MidUtil:  0.4,
				},
				Cost:         1.0,
				Multiplicity: 1,
				MemSize:      40,
			},
			{
				Name: "H100",
				Type: "GPU_H100",
				Power: config.PowerSpec{
					Idle:     60,
					MidPower: 200,
					Full:     450,
					MidUtil:  0.5,
				},
				Cost:         2.0,
				Multiplicity: 1,
				MemSize:      80,
			},
		},
	}

	system.SetAcceleratorsFromSpec(acceleratorData)

	if len(system.accelerators) != 2 {
		t.Errorf("Expected 2 accelerators, got %d", len(system.accelerators))
	}

	if system.accelerators["A100"] == nil {
		t.Error("A100 accelerator should exist")
	}

	if system.accelerators["H100"] == nil {
		t.Error("H100 accelerator should exist")
	}
}

func TestSystem_AddAcceleratorFromSpec(t *testing.T) {
	system := NewSystem()

	spec := config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	}

	system.AddAcceleratorFromSpec(spec)

	if len(system.accelerators) != 1 {
		t.Errorf("Expected 1 accelerator, got %d", len(system.accelerators))
	}

	acc := system.accelerators["A100"]
	if acc == nil {
		t.Fatal("A100 accelerator should exist")
	}

	if acc.Name() != "A100" {
		t.Errorf("Expected accelerator name A100, got %s", acc.Name())
	}
}

func TestSystem_RemoveAccelerator(t *testing.T) {
	system := NewSystem()

	spec := config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	}

	system.AddAcceleratorFromSpec(spec)

	// Test successful removal
	err := system.RemoveAccelerator("A100")
	if err != nil {
		t.Errorf("RemoveAccelerator should succeed, got error: %v", err)
	}

	if len(system.accelerators) != 0 {
		t.Errorf("Expected 0 accelerators after removal, got %d", len(system.accelerators))
	}

	// Test removal of non-existent accelerator
	err = system.RemoveAccelerator("NonExistent")
	if err == nil {
		t.Error("RemoveAccelerator should fail for non-existent accelerator")
	}
}

func TestSystem_SetCapacityFromSpec(t *testing.T) {
	system := NewSystem()

	capacityData := &config.CapacityData{
		Count: []config.AcceleratorCount{
			{Type: "GPU_A100", Count: 4},
			{Type: "GPU_H100", Count: 2},
		},
	}

	system.SetCapacityFromSpec(capacityData)

	if len(system.capacity) != 2 {
		t.Errorf("Expected 2 capacity entries, got %d", len(system.capacity))
	}

	if system.capacity["GPU_A100"] != 4 {
		t.Errorf("Expected GPU_A100 capacity 4, got %d", system.capacity["GPU_A100"])
	}

	if system.capacity["GPU_H100"] != 2 {
		t.Errorf("Expected GPU_H100 capacity 2, got %d", system.capacity["GPU_H100"])
	}
}

func TestSystem_SetCountFromSpec(t *testing.T) {
	system := NewSystem()

	spec := config.AcceleratorCount{Type: "GPU_A100", Count: 4}

	system.SetCountFromSpec(spec)

	if len(system.capacity) != 1 {
		t.Errorf("Expected 1 capacity entry, got %d", len(system.capacity))
	}

	if system.capacity["GPU_A100"] != 4 {
		t.Errorf("Expected GPU_A100 capacity 4, got %d", system.capacity["GPU_A100"])
	}
}

func TestSystem_SetModelsFromSpec(t *testing.T) {
	system := NewSystem()

	modelData := &config.ModelData{
		PerfData: []config.ModelAcceleratorPerfData{
			{
				Name:         "llama-7b",
				Acc:          "A100",
				AccCount:     1,
				MaxBatchSize: 16,
				AtTokens:     100,
				DecodeParms: config.DecodeParms{
					Alpha: 10.0,
					Beta:  2.0,
				},
				PrefillParms: config.PrefillParms{
					Gamma: 5.0,
					Delta: 0.1,
				},
			},
			{
				Name:         "llama-13b",
				Acc:          "A100",
				AccCount:     2,
				MaxBatchSize: 8,
				AtTokens:     150,
				DecodeParms: config.DecodeParms{
					Alpha: 15.0,
					Beta:  3.0,
				},
				PrefillParms: config.PrefillParms{
					Gamma: 8.0,
					Delta: 0.15,
				},
			},
		},
	}

	system.SetModelsFromSpec(modelData)

	if len(system.models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(system.models))
	}

	if system.models["llama-7b"] == nil {
		t.Error("llama-7b model should exist")
	}

	if system.models["llama-13b"] == nil {
		t.Error("llama-13b model should exist")
	}
}

func TestSystem_AddModel(t *testing.T) {
	system := NewSystem()

	model := system.AddModel("test-model")

	if model == nil {
		t.Fatal("AddModel should return a model")
	}

	if len(system.models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(system.models))
	}

	if system.models["test-model"] != model {
		t.Error("Model should be stored in system")
	}

	if model.Name() != "test-model" {
		t.Errorf("Expected model name test-model, got %s", model.Name())
	}
}

func TestSystem_RemoveModel(t *testing.T) {
	system := NewSystem()

	system.AddModel("test-model")

	// Test successful removal
	err := system.RemoveModel("test-model")
	if err != nil {
		t.Errorf("RemoveModel should succeed, got error: %v", err)
	}

	if len(system.models) != 0 {
		t.Errorf("Expected 0 models after removal, got %d", len(system.models))
	}

	// Test removal of non-existent model
	err = system.RemoveModel("NonExistent")
	if err == nil {
		t.Error("RemoveModel should fail for non-existent model")
	}
}

func TestSystem_SetServersFromSpec(t *testing.T) {
	system := NewSystem()

	serverData := &config.ServerData{
		Spec: []config.ServerSpec{
			{
				Name:  "server1",
				Model: "llama-7b",
				Class: "default",
				CurrentAlloc: config.AllocationData{
					Load: config.ServerLoadSpec{
						ArrivalRate:  30,
						AvgInTokens:  100,
						AvgOutTokens: 200,
					},
				},
				MinNumReplicas: 1,
				MaxBatchSize:   16,
			},
			{
				Name:  "server2",
				Model: "llama-13b",
				Class: "high-priority",
				CurrentAlloc: config.AllocationData{
					Load: config.ServerLoadSpec{
						ArrivalRate:  20,
						AvgInTokens:  150,
						AvgOutTokens: 300,
					},
				},
				MinNumReplicas: 2,
				MaxBatchSize:   8,
			},
		},
	}

	system.SetServersFromSpec(serverData)

	if len(system.servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(system.servers))
	}

	if system.servers["server1"] == nil {
		t.Error("server1 should exist")
	}

	if system.servers["server2"] == nil {
		t.Error("server2 should exist")
	}
}

func TestSystem_AddServerFromSpec(t *testing.T) {
	system := NewSystem()

	spec := config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	}

	system.AddServerFromSpec(spec)

	if len(system.servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(system.servers))
	}

	server := system.servers["test-server"]
	if server == nil {
		t.Fatal("test-server should exist")
	}

	if server.Name() != "test-server" {
		t.Errorf("Expected server name test-server, got %s", server.Name())
	}
}

func TestSystem_RemoveServer(t *testing.T) {
	system := NewSystem()

	spec := config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	}

	system.AddServerFromSpec(spec)

	// Test successful removal
	err := system.RemoveServer("test-server")
	if err != nil {
		t.Errorf("RemoveServer should succeed, got error: %v", err)
	}

	if len(system.servers) != 0 {
		t.Errorf("Expected 0 servers after removal, got %d", len(system.servers))
	}

	// Test removal of non-existent server
	err = system.RemoveServer("NonExistent")
	if err == nil {
		t.Error("RemoveServer should fail for non-existent server")
	}
}

func TestSystem_SetServiceClassesFromSpec(t *testing.T) {
	system := NewSystem()

	serviceClassData := &config.ServiceClassData{
		Spec: []config.ServiceClassSpec{
			{
				Name:     "high-priority",
				Priority: 1,
				ModelTargets: []config.ModelTarget{
					{
						Model:    "llama-7b",
						SLO_ITL:  100,
						SLO_TTFT: 1000,
						SLO_TPS:  50,
					},
				},
			},
			{
				Name:     "low-priority",
				Priority: 3,
				ModelTargets: []config.ModelTarget{
					{
						Model:    "llama-13b",
						SLO_ITL:  200,
						SLO_TTFT: 2000,
						SLO_TPS:  25,
					},
				},
			},
		},
	}

	system.SetServiceClassesFromSpec(serviceClassData)

	if len(system.serviceClasses) != 2 {
		t.Errorf("Expected 2 service classes, got %d", len(system.serviceClasses))
	}

	if system.serviceClasses["high-priority"] == nil {
		t.Error("high-priority service class should exist")
	}

	if system.serviceClasses["low-priority"] == nil {
		t.Error("low-priority service class should exist")
	}
}

func TestSystem_AddServiceClass(t *testing.T) {
	system := NewSystem()

	system.AddServiceClass("test-class", 2)

	if len(system.serviceClasses) != 1 {
		t.Errorf("Expected 1 service class, got %d", len(system.serviceClasses))
	}

	serviceClass := system.serviceClasses["test-class"]
	if serviceClass == nil {
		t.Fatal("test-class should exist")
	}

	if serviceClass.Name() != "test-class" {
		t.Errorf("Expected service class name test-class, got %s", serviceClass.Name())
	}

	if serviceClass.Priority() != 2 {
		t.Errorf("Expected service class priority 2, got %d", serviceClass.Priority())
	}
}

func TestSystem_RemoveServiceClass(t *testing.T) {
	system := NewSystem()

	system.AddServiceClass("test-class", 2)

	// Test successful removal
	err := system.RemoveServiceClass("test-class")
	if err != nil {
		t.Errorf("RemoveServiceClass should succeed, got error: %v", err)
	}

	if len(system.serviceClasses) != 0 {
		t.Errorf("Expected 0 service classes after removal, got %d", len(system.serviceClasses))
	}

	// Test removal of non-existent service class
	err = system.RemoveServiceClass("NonExistent")
	if err == nil {
		t.Error("RemoveServiceClass should fail for non-existent service class")
	}
}

func TestSystem_Accelerators(t *testing.T) {
	system := NewSystem()

	spec := config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	}

	system.AddAcceleratorFromSpec(spec)

	accelerators := system.Accelerators()

	if len(accelerators) != 1 {
		t.Errorf("Expected 1 accelerator, got %d", len(accelerators))
	}

	if accelerators["A100"] == nil {
		t.Error("A100 accelerator should exist")
	}
}

func TestSystem_Models(t *testing.T) {
	system := NewSystem()

	system.AddModel("test-model")

	models := system.Models()

	if len(models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(models))
	}

	if models["test-model"] == nil {
		t.Error("test-model should exist")
	}
}

func TestSystem_ServiceClasses(t *testing.T) {
	system := NewSystem()

	system.AddServiceClass("test-class", 2)

	serviceClasses := system.ServiceClasses()

	if len(serviceClasses) != 1 {
		t.Errorf("Expected 1 service class, got %d", len(serviceClasses))
	}

	if serviceClasses["test-class"] == nil {
		t.Error("test-class should exist")
	}
}

func TestSystem_Servers(t *testing.T) {
	system := NewSystem()

	spec := config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	}

	system.AddServerFromSpec(spec)

	servers := system.Servers()

	if len(servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(servers))
	}

	if servers["test-server"] == nil {
		t.Error("test-server should exist")
	}
}

func TestSystem_Accelerator(t *testing.T) {
	system := NewSystem()

	spec := config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	}

	system.AddAcceleratorFromSpec(spec)

	// Test existing accelerator
	acc := system.Accelerator("A100")
	if acc == nil {
		t.Error("Accelerator A100 should exist")
	}

	// Test non-existent accelerator
	acc = system.Accelerator("NonExistent")
	if acc != nil {
		t.Error("NonExistent accelerator should return nil")
	}
}

func TestSystem_Model(t *testing.T) {
	system := NewSystem()

	system.AddModel("test-model")

	// Test existing model
	model := system.Model("test-model")
	if model == nil {
		t.Error("Model test-model should exist")
	}

	// Test non-existent model
	model = system.Model("NonExistent")
	if model != nil {
		t.Error("NonExistent model should return nil")
	}
}

func TestSystem_ServiceClass(t *testing.T) {
	system := NewSystem()

	system.AddServiceClass("test-class", 2)

	// Test existing service class
	serviceClass := system.ServiceClass("test-class")
	if serviceClass == nil {
		t.Error("ServiceClass test-class should exist")
	}

	// Test non-existent service class
	serviceClass = system.ServiceClass("NonExistent")
	if serviceClass != nil {
		t.Error("NonExistent service class should return nil")
	}
}

func TestSystem_Server(t *testing.T) {
	system := NewSystem()

	spec := config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	}

	system.AddServerFromSpec(spec)

	// Test existing server
	server := system.Server("test-server")
	if server == nil {
		t.Error("Server test-server should exist")
	}

	// Test non-existent server
	server = system.Server("NonExistent")
	if server != nil {
		t.Error("NonExistent server should return nil")
	}
}

func TestSystem_Capacities(t *testing.T) {
	system := NewSystem()

	system.SetCountFromSpec(config.AcceleratorCount{Type: "GPU_A100", Count: 4})

	capacities := system.Capacities()

	if len(capacities) != 1 {
		t.Errorf("Expected 1 capacity entry, got %d", len(capacities))
	}

	if capacities["GPU_A100"] != 4 {
		t.Errorf("Expected GPU_A100 capacity 4, got %d", capacities["GPU_A100"])
	}
}

func TestSystem_Capacity(t *testing.T) {
	system := NewSystem()

	system.SetCountFromSpec(config.AcceleratorCount{Type: "GPU_A100", Count: 4})

	// Test existing capacity
	capacity, exists := system.Capacity("GPU_A100")
	if !exists {
		t.Error("GPU_A100 capacity should exist")
	}
	if capacity != 4 {
		t.Errorf("Expected GPU_A100 capacity 4, got %d", capacity)
	}

	// Test non-existent capacity
	capacity, exists = system.Capacity("NonExistent")
	if exists {
		t.Error("NonExistent capacity should not exist")
	}
	if capacity != 0 {
		t.Errorf("Expected capacity 0 for non-existent type, got %d", capacity)
	}
}

func TestSystem_RemoveCapacity(t *testing.T) {
	system := NewSystem()

	system.SetCountFromSpec(config.AcceleratorCount{Type: "GPU_A100", Count: 4})

	// Test successful removal
	removed := system.RemoveCapacity("GPU_A100")
	if !removed {
		t.Error("RemoveCapacity should return true for existing capacity")
	}

	if len(system.capacity) != 0 {
		t.Errorf("Expected 0 capacity entries after removal, got %d", len(system.capacity))
	}

	// Test removal of non-existent capacity
	removed = system.RemoveCapacity("NonExistent")
	if removed {
		t.Error("RemoveCapacity should return false for non-existent capacity")
	}
}

func TestSystem_Calculate(t *testing.T) {
	system := NewSystem()

	// Add accelerator
	system.AddAcceleratorFromSpec(config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	})

	// Add model
	model := system.AddModel("test-model")
	model.AddPerfDataFromSpec(&config.ModelAcceleratorPerfData{
		Name:         "test-model",
		Acc:          "A100",
		AccCount:     1,
		MaxBatchSize: 16,
		AtTokens:     100,
		DecodeParms: config.DecodeParms{
			Alpha: 10.0,
			Beta:  2.0,
		},
		PrefillParms: config.PrefillParms{
			Gamma: 5.0,
			Delta: 0.1,
		},
	})

	// Add server
	system.AddServerFromSpec(config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	})

	// Should not panic
	system.Calculate()
}

func TestSystem_AllocateByType(t *testing.T) {
	system := NewSystem()
	TheSystem = system // Set global reference

	// Add accelerator
	system.AddAcceleratorFromSpec(config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	})

	// Add model
	model := system.AddModel("test-model")
	model.AddPerfDataFromSpec(&config.ModelAcceleratorPerfData{
		Name:         "test-model",
		Acc:          "A100",
		AccCount:     1,
		MaxBatchSize: 16,
		AtTokens:     100,
		DecodeParms: config.DecodeParms{
			Alpha: 10.0,
			Beta:  2.0,
		},
		PrefillParms: config.PrefillParms{
			Gamma: 5.0,
			Delta: 0.1,
		},
	})

	// Add service class with target
	system.AddServiceClass("default", 1)
	serviceClass := system.ServiceClass("default")
	if serviceClass != nil {
		serviceClass.AddModelTarget(&config.ModelTarget{
			Model:    "test-model",
			SLO_ITL:  100,
			SLO_TTFT: 1000,
			SLO_TPS:  50,
		})
	}

	// Set capacity
	system.SetCountFromSpec(config.AcceleratorCount{Type: "GPU_A100", Count: 4})

	// Add server
	system.AddServerFromSpec(config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
			Accelerator: "A100",
			NumReplicas: 2,
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	})

	// Calculate to prepare allocations
	system.Calculate()

	// Get the server and create an allocation for it
	server := system.Server("test-server")
	if server != nil {
		alloc := CreateAllocation("test-server", "A100")
		if alloc != nil {
			server.SetAllocation(alloc)
		}
	}

	// Should not panic - function creates allocation data internally
	system.AllocateByType()
}

func TestSystem_GenerateSolution(t *testing.T) {
	system := NewSystem()
	TheSystem = system // Set global reference

	// Add accelerator
	system.AddAcceleratorFromSpec(config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	})

	// Add model
	model := system.AddModel("test-model")
	model.AddPerfDataFromSpec(&config.ModelAcceleratorPerfData{
		Name:         "test-model",
		Acc:          "A100",
		AccCount:     1,
		MaxBatchSize: 16,
		AtTokens:     100,
		DecodeParms: config.DecodeParms{
			Alpha: 10.0,
			Beta:  2.0,
		},
		PrefillParms: config.PrefillParms{
			Gamma: 5.0,
			Delta: 0.1,
		},
	})

	// Add service class with target
	system.AddServiceClass("default", 1)
	serviceClass := system.ServiceClass("default")
	if serviceClass != nil {
		serviceClass.AddModelTarget(&config.ModelTarget{
			Model:    "test-model",
			SLO_ITL:  100,
			SLO_TTFT: 1000,
			SLO_TPS:  50,
		})
	}

	// Add server
	system.AddServerFromSpec(config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
			Accelerator: "A100",
			NumReplicas: 2,
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	})

	// Calculate to prepare allocations
	system.Calculate()

	// Get the server and create an allocation for it
	server := system.Server("test-server")
	if server != nil {
		alloc := CreateAllocation("test-server", "A100")
		if alloc != nil {
			server.SetAllocation(alloc)
		}
	}

	solution := system.GenerateSolution()

	if solution == nil {
		t.Fatal("GenerateSolution should return a solution")
	}

	if system.allocationSolution != solution {
		t.Error("System should store the generated solution")
	}
}

func TestSystem_String(t *testing.T) {
	system := NewSystem()

	// Add basic components
	system.AddAcceleratorFromSpec(config.AcceleratorSpec{
		Name: "A100",
		Type: "GPU_A100",
		Power: config.PowerSpec{
			Idle:     50,
			MidPower: 150,
			Full:     350,
			MidUtil:  0.4,
		},
		Cost:         1.0,
		Multiplicity: 1,
		MemSize:      40,
	})

	system.AddServiceClass("default", 1)
	serviceClass := system.ServiceClass("default")
	if serviceClass != nil {
		serviceClass.AddModelTarget(&config.ModelTarget{
			Model:    "test-model",
			SLO_ITL:  100,
			SLO_TTFT: 1000,
			SLO_TPS:  50,
		})
	}

	system.AddServerFromSpec(config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
			Accelerator: "A100",
			NumReplicas: 2,
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	})

	result := system.String()

	// Should contain solution information
	if !strings.Contains(result, "Solution:") {
		t.Error("String should contain Solution section")
	}

	// Should contain allocation by type information
	if !strings.Contains(result, "AllocationByType:") {
		t.Error("String should contain AllocationByType section")
	}

	// Should contain total cost
	if !strings.Contains(result, "totalCost=") {
		t.Error("String should contain totalCost")
	}
}

func TestAllocationByType_String(t *testing.T) {
	alloc := &AllocationByType{
		name:  "GPU_A100",
		count: 4,
		limit: 8,
		cost:  100.5,
	}

	result := alloc.String()

	if !strings.Contains(result, "name=GPU_A100") {
		t.Error("String should contain allocation type name")
	}

	if !strings.Contains(result, "count=4") {
		t.Error("String should contain count")
	}

	if !strings.Contains(result, "limit=8") {
		t.Error("String should contain limit")
	}

	if !strings.Contains(result, "cost=100.5") {
		t.Error("String should contain cost")
	}
}

// Test global functions
func TestGetModels(t *testing.T) {
	// Create a test system and set it as TheSystem
	system := NewSystem()
	system.AddModel("test-model")
	TheSystem = system

	models := GetModels()

	if len(models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(models))
	}

	if models["test-model"] == nil {
		t.Error("test-model should exist")
	}
}

func TestGetServers(t *testing.T) {
	// Create a test system and set it as TheSystem
	system := NewSystem()
	system.AddServerFromSpec(config.ServerSpec{
		Name:  "test-server",
		Model: "test-model",
		Class: "default",
		CurrentAlloc: config.AllocationData{
			Load: config.ServerLoadSpec{
				ArrivalRate:  30,
				AvgInTokens:  100,
				AvgOutTokens: 200,
			},
		},
		MinNumReplicas: 1,
		MaxBatchSize:   16,
	})
	TheSystem = system

	servers := GetServers()

	if len(servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(servers))
	}

	if servers["test-server"] == nil {
		t.Error("test-server should exist")
	}
}

func TestGetCapacities(t *testing.T) {
	// Create a test system and set it as TheSystem
	system := NewSystem()
	system.SetCountFromSpec(config.AcceleratorCount{Type: "GPU_A100", Count: 4})
	TheSystem = system

	capacities := GetCapacities()

	if len(capacities) != 1 {
		t.Errorf("Expected 1 capacity entry, got %d", len(capacities))
	}

	if capacities["GPU_A100"] != 4 {
		t.Errorf("Expected GPU_A100 capacity 4, got %d", capacities["GPU_A100"])
	}
}
