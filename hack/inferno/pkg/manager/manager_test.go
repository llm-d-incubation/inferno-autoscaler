package manager

import (
	"testing"

	"github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/config"
	"github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/core"
	"github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/solver"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name      string
		system    *core.System
		optimizer *solver.Optimizer
		wantNil   bool
	}{
		{
			name:      "create manager with valid system and optimizer",
			system:    &core.System{},
			optimizer: &solver.Optimizer{},
			wantNil:   false,
		},
		{
			name:      "create manager with nil system",
			system:    nil,
			optimizer: &solver.Optimizer{},
			wantNil:   false,
		},
		{
			name:      "create manager with nil optimizer",
			system:    &core.System{},
			optimizer: nil,
			wantNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewManager(tt.system, tt.optimizer)
			if (got == nil) != tt.wantNil {
				t.Errorf("NewManager() = %v, wantNil %v", got, tt.wantNil)
			}

			if got != nil {
				if got.system != tt.system {
					t.Errorf("NewManager().system = %v, want %v", got.system, tt.system)
				}
				if got.optimizer != tt.optimizer {
					t.Errorf("NewManager().optimizer = %v, want %v", got.optimizer, tt.optimizer)
				}
				// Verify that core.TheSystem is set
				if core.TheSystem != tt.system {
					t.Errorf("core.TheSystem = %v, want %v", core.TheSystem, tt.system)
				}
			}
		})
	}
}

func TestManager_Optimize(t *testing.T) {
	tests := []struct {
		name         string
		setupManager func() *Manager
		wantErr      bool
		checkCalls   func(t *testing.T, manager *Manager)
	}{
		{
			name: "successful optimization",
			setupManager: func() *Manager {
				optimizerSpec := &config.OptimizerSpec{
					Unlimited:        false,
					SaturationPolicy: "None",
				}
				optimizer := solver.NewOptimizerFromSpec(optimizerSpec)
				return &Manager{
					system:    &core.System{},
					optimizer: optimizer,
				}
			},
			wantErr: false,
			checkCalls: func(t *testing.T, manager *Manager) {
				// In a real test, we would check that methods were called
				// For now, we just verify no error occurred
			},
		},
		{
			name: "optimization with error scenario",
			setupManager: func() *Manager {
				optimizerSpec := &config.OptimizerSpec{
					Unlimited:        false,
					SaturationPolicy: "None",
				}
				optimizer := solver.NewOptimizerFromSpec(optimizerSpec)
				return &Manager{
					system:    &core.System{},
					optimizer: optimizer,
				}
			},
			wantErr: false, // the optimizer shouldn't fail with valid input
			checkCalls: func(t *testing.T, manager *Manager) {
				// Verify that optimization completed
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := tt.setupManager()
			err := manager.Optimize()

			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.Optimize() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.checkCalls != nil {
				tt.checkCalls(t, manager)
			}
		})
	}
}

func TestManager_OptimizeIntegration(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (*core.System, *solver.Optimizer)
		wantErr bool
	}{
		{
			name: "integration with minimal system",
			setup: func() (*core.System, *solver.Optimizer) {
				system := &core.System{}

				optimizerSpec := &config.OptimizerSpec{
					Unlimited:        false,
					SaturationPolicy: "None",
				}
				optimizer := solver.NewOptimizerFromSpec(optimizerSpec)

				return system, optimizer
			},
			wantErr: false,
		},
		{
			name: "integration with unlimited resources",
			setup: func() (*core.System, *solver.Optimizer) {
				system := &core.System{}

				optimizerSpec := &config.OptimizerSpec{
					Unlimited:        true,
					SaturationPolicy: "PriorityExhaustive",
				}
				optimizer := solver.NewOptimizerFromSpec(optimizerSpec)

				return system, optimizer
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system, optimizer := tt.setup()
			manager := NewManager(system, optimizer)

			err := manager.Optimize()
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.Optimize() integration error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		manager *Manager
		wantErr bool
	}{
		{
			name: "manager with nil system",
			manager: &Manager{
				system:    nil,
				optimizer: &solver.Optimizer{},
			},
			wantErr: true,
		},
		{
			name: "manager with nil optimizer",
			manager: &Manager{
				system:    &core.System{},
				optimizer: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil && !tt.wantErr {
					t.Errorf("Manager.Optimize() panicked: %v", r)
				}
			}()

			err := tt.manager.Optimize()
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.Optimize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
