package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/atantawi/inferno-hack/pkg/config"
	"github.com/atantawi/inferno-hack/pkg/core"
	"github.com/atantawi/inferno-hack/pkg/manager"
	"github.com/atantawi/inferno-hack/pkg/solver"
)

func main() {
	dataPath := "."
	if len(os.Args) > 1 {
		dataPath = os.Args[1]
	}
	prefix := dataPath + "/"
	fn_acc := prefix + "accelerator-data.json"
	fn_cap := prefix + "capacity-data.json"
	fn_mod := prefix + "model-data.json"
	fn_svc := prefix + "serviceclass-data.json"
	fn_srv := prefix + "server-data.json"
	fn_opt := prefix + "optimizer-data.json"
	fn_sol := prefix + "solution-data.json"

	system := core.NewSystem()

	bytes_acc, err_acc := os.ReadFile(fn_acc)
	if err_acc != nil {
		fmt.Println(err_acc)
	}
	if d, err := FromDataToSpec(bytes_acc, config.AcceleratorData{}); err == nil {
		system.SetAcceleratorsFromSpec(d)
	} else {
		fmt.Println(err)
		return
	}

	bytes_cap, err_cap := os.ReadFile(fn_cap)
	if err_cap != nil {
		fmt.Println(err_cap)
	}
	if d, err := FromDataToSpec(bytes_cap, config.CapacityData{}); err == nil {
		system.SetCapacityFromSpec(d)
	} else {
		fmt.Println(err)
		return
	}

	bytes_mod, err_mod := os.ReadFile(fn_mod)
	if err_mod != nil {
		fmt.Println(err_mod)
	}
	if d, err := FromDataToSpec(bytes_mod, config.ModelData{}); err == nil {
		system.SetModelsFromSpec(d)
	} else {
		fmt.Println(err)
		return
	}

	bytes_svc, err_svc := os.ReadFile(fn_svc)
	if err_svc != nil {
		fmt.Println(err_svc)
	}
	if d, err := FromDataToSpec(bytes_svc, config.ServiceClassData{}); err == nil {
		system.SetServiceClassesFromSpec(d)
	} else {
		fmt.Println(err)
		return
	}

	bytes_srv, err_srv := os.ReadFile(fn_srv)
	if err_srv != nil {
		fmt.Println(err_srv)
	}
	if d, err := FromDataToSpec(bytes_srv, config.ServerData{}); err == nil {
		system.SetServersFromSpec(d)
	} else {
		fmt.Println(err)
		return
	}

	var optimizer *solver.Optimizer
	bytes_opt, err_opt := os.ReadFile(fn_opt)
	if err_opt != nil {
		fmt.Println(err_acc)
	}
	if d, err := FromDataToSpec(bytes_opt, config.OptimizerData{}); err == nil {
		optimizer = solver.NewOptimizerFromSpec(&d.Spec)
	} else {
		fmt.Println(err)
		return
	}

	manager := manager.NewManager(system, optimizer)

	system.Calculate()
	if err := manager.Optimize(); err != nil {
		fmt.Println(err)
		return
	}
	allocationSolution := system.GenerateSolution()

	// generate json
	if byteValue, err := json.Marshal(allocationSolution); err != nil {
		fmt.Println(err)
	} else {
		os.WriteFile(fn_sol, byteValue, 0644)
	}

	fmt.Printf("%v", system)
	fmt.Printf("%v", optimizer)
}

// unmarshal a byte array to its corresponding object
func FromDataToSpec[T any](byteValue []byte, t T) (*T, error) {
	var d T
	if err := json.Unmarshal(byteValue, &d); err != nil {
		return nil, err
	}
	return &d, nil
}
