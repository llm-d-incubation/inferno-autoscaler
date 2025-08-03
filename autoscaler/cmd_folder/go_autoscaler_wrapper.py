#!/usr/bin/env python3
"""
Go Autoscaler Wrapper

This script serves as a bridge between the Go inferno-autoscaler controller
and the existing Python optimization engine. It:

1. Accepts input from Go controller (comprehensive cluster data)
2. Transforms Go data structures to Python optimization input format
3. Executes existing Python optimization logic
4. Transforms results back to Go-compatible format
5. Returns optimized allocations to Go controller

Usage:
    python go_autoscaler_wrapper.py <input_file> <output_file>
"""

import json
import sys
import time
import traceback
from typing import Dict, Any, Optional, List

from optimizer.abstract_optimization_model import AbstractOptimizationModel
from optimizer.gpu_optimizer import GPUOptimizer
from optimizer.opt_types import VariantData, OptimizationResult
from optimizer.docplex_optimization_model import DoCplexOptimizationModel


def transform_go_input_to_python(go_data: Dict[str, Any]) -> Dict[str, Any]:
    """
    Transform Go controller input to Python optimizer input format.
    
    Args:
        go_data: Complete data from Go controller including:
            - variant_autoscalings: List of VariantAutoscaling objects
            - model_analysis: Analysis results per model
            - cluster_inventory: GPU inventory from cluster
            - accelerator_config: GPU costs and properties
            - service_class_config: SLO configurations
            - system_data: System configuration data
    
    Returns:
        Python optimization input in the format expected by existing optimizer
    """
    print(f"Python: Transforming Go input with {len(go_data.get('variant_autoscalings', []))} variants")
    
    # Extract variants data from Go VariantAutoscaling objects
    variants_data = extract_variants_from_go(go_data)
    
    # Extract required service rates from model analysis
    required_service_rates = extract_service_rates_from_analysis(go_data)
    
    # Extract GPU limits from cluster inventory
    gpu_limits = extract_gpu_limits_from_inventory(go_data)
    
    # Extract GPU costs from accelerator config
    gpu_cost = extract_gpu_costs_from_config(go_data)
    
    # TODO: Extract additional parameters
    scale_to_zero = []  # Can be derived from Go data if needed
    deployed_instances = {}  # Current deployment state
    min_replicas = {}
    max_replicas = {}
    
    return {
        "variants_data": variants_data,
        "required_service_rates": required_service_rates,
        "gpu_limits": gpu_limits,
        "gpu_cost": gpu_cost,
        "scale_to_zero": scale_to_zero,
        "deployed_instances": deployed_instances,
        "min_replicas": min_replicas,
        "max_replicas": max_replicas
    }


def extract_variants_from_go(go_data: Dict[str, Any]) -> Dict[str, Dict[str, VariantData]]:
    """
    Convert Go VariantAutoscaling objects to Python VariantData format.
    
    Each Go VariantAutoscaling contains:
    - spec.modelID: Model identifier
    - spec.modelProfile.accelerators: List of accelerator profiles
    - Each accelerator profile has: acc, accCount, alpha, beta, maxBatchSize, atTokens
    """
    variants_data = {}
    
    for va in go_data.get("variant_autoscalings", []):
        model_id = va["spec"]["modelID"]
        model_setups = {}
        
        for i, acc_profile in enumerate(va["spec"]["modelProfile"]["accelerators"]):
            setup_id = f"{acc_profile['acc']}-{i}"
            
            # Convert Go AcceleratorProfile to Python VariantData
            variant_data = VariantData(
                variant_id=setup_id,
                gpu_type=acc_profile["acc"],
                gpu_number=float(acc_profile["accCount"]),
                role="inference",  # Default role
                slo_class="default",  # TODO: Extract from sloClassRef
                max_service_rate=calculate_max_service_rate(acc_profile),
                max_concurrency=float(acc_profile["maxBatchSize"])
            )
            
            model_setups[setup_id] = variant_data
        
        if model_setups:
            variants_data[model_id] = model_setups
    
    print(f"Python: Extracted {len(variants_data)} models with variants")
    return variants_data


def calculate_max_service_rate(acc_profile: Dict[str, Any]) -> float:
    """
    Calculate maximum service rate from accelerator profile.
    
    Uses alpha, beta parameters and token information to estimate
    the maximum requests per second this setup can handle.
    """
    try:
        alpha = float(acc_profile.get("alpha", "1.0"))
        beta = float(acc_profile.get("beta", "1.0"))
        at_tokens = float(acc_profile.get("atTokens", "100"))
        
        # Simple calculation - can be refined based on actual model
        # This is a placeholder calculation
        max_rate = (1.0 / (alpha + beta * at_tokens)) * 1000  # Convert to requests per second
        return max(max_rate, 0.1)  # Ensure positive non-zero rate
        
    except (ValueError, ZeroDivisionError):
        print(f"Python: Warning - failed to calculate service rate for {acc_profile}, using default")
        return 1.0  # Default fallback


def extract_service_rates_from_analysis(go_data: Dict[str, Any]) -> Dict[str, float]:
    """
    Extract required service rates from Go model analysis results.
    
    ModelAnalyzeResponse contains allocations with RequiredPrefillQPS and RequiredDecodeQPS
    """
    required_rates = {}
    
    model_analysis = go_data.get("model_analysis", {})
    for model_name, analysis in model_analysis.items():
        total_rate = 0.0
        
        allocations = analysis.get("allocations", {})
        for acc_name, allocation in allocations.items():
            prefill_qps = allocation.get("RequiredPrefillQPS", 0.0)
            decode_qps = allocation.get("RequiredDecodeQPS", 0.0)
            total_rate += prefill_qps + decode_qps
        
        if total_rate > 0:
            required_rates[model_name] = total_rate
    
    print(f"Python: Extracted service rates for {len(required_rates)} models")
    return required_rates


def extract_gpu_limits_from_inventory(go_data: Dict[str, Any]) -> Dict[str, int]:
    """
    Extract GPU limits from cluster inventory.
    
    ClusterInventory format: map[nodeName]map[gpuModel]AcceleratorModelInfo
    AcceleratorModelInfo has: Count, Memory
    """
    gpu_limits = {}
    
    inventory = go_data.get("cluster_inventory", {})
    for node_name, node_gpus in inventory.items():
        for gpu_model, gpu_info in node_gpus.items():
            count = gpu_info.get("Count", 0)
            if gpu_model not in gpu_limits:
                gpu_limits[gpu_model] = 0
            gpu_limits[gpu_model] += count
    
    print(f"Python: Extracted GPU limits: {gpu_limits}")
    return gpu_limits


def extract_gpu_costs_from_config(go_data: Dict[str, Any]) -> Dict[str, float]:
    """
    Extract GPU costs from accelerator configuration.
    
    AcceleratorConfig format: map[acceleratorName]map[property]value
    Looking for "cost" property in each accelerator's config
    """
    gpu_cost = {}
    
    acc_config = go_data.get("accelerator_config", {})
    for acc_name, acc_properties in acc_config.items():
        cost_str = acc_properties.get("cost", "0.0")
        try:
            cost = float(cost_str)
            gpu_cost[acc_name] = cost
        except ValueError:
            print(f"Python: Warning - invalid cost '{cost_str}' for {acc_name}, using 0.0")
            gpu_cost[acc_name] = 0.0
    
    print(f"Python: Extracted GPU costs: {gpu_cost}")
    return gpu_cost


def run_optimization(python_input: Dict[str, Any]) -> OptimizationResult:
    """
    Execute the existing Python optimization logic.
    """
    print("Python: Starting optimization with existing engine")
    
    # Convert raw variants to VariantData objects (if not already done)
    if "variants_data" in python_input and python_input["variants_data"]:
        # Check if already converted
        first_model = list(python_input["variants_data"].values())[0]
        first_setup = list(first_model.values())[0]
        if not isinstance(first_setup, VariantData):
            python_input["variants_data"] = convert_variants(python_input["variants_data"])
    
    # Create solver and optimizer
    solver = DoCplexOptimizationModel(name="go_integration_optimization")
    optimizer = GPUOptimizer(solver)
    
    # Execute optimization
    result = optimizer.get_optimal_gpu_allocation(
        variants=python_input["variants_data"],
        required_rates=python_input["required_service_rates"],
        gpu_limits=python_input["gpu_limits"],
        gpu_cost=python_input["gpu_cost"],
        scale_to_zero=python_input.get("scale_to_zero", []),
        deployed_instances=python_input.get("deployed_instances"),
        min_replicas=python_input.get("min_replicas", {}),
        max_replicas=python_input.get("max_replicas", {})
    )
    
    return result


def convert_variants(raw_variants: Dict[str, Dict[str, Dict]]) -> Dict[str, Dict[str, VariantData]]:
    """Convert raw variant dictionaries to VariantData objects."""
    return {
        model: {
            setup: VariantData(**variant_data)
            for setup, variant_data in setups.items()
        }
        for model, setups in raw_variants.items()
    }


def transform_python_output_for_go(opt_result: OptimizationResult, go_variant_autoscalings: List[Dict]) -> Dict[str, Any]:
    """
    Transform Python optimization result to Go-compatible format.
    
    Returns both:
    1. Raw OptimizationResult for logging/debugging 
    2. Go-compatible OptimizedAlloc objects for each VariantAutoscaling
    
    Args:
        opt_result: Python optimization result
        go_variant_autoscalings: List of VariantAutoscaling objects from Go input
        
    Go OptimizedAlloc structure:
    - LastRunTime: metav1.Time (RFC3339 format)
    - Accelerator: string  
    - NumReplicas: int
    
    Key insight: Go creates one OptimizedAlloc per VariantAutoscaling (not per model)
    using FullName(name, namespace) as the key.
    """
    optimized_allocations = {}
    
    # Convert OptimizationResult to serializable format
    raw_optimization_result = None
    if opt_result:
        raw_optimization_result = {
            "gpu_after_allocation": dict(opt_result.gpu_after_allocation) if opt_result.gpu_after_allocation else {},
            "models_data": {},
            "impossible_models": list(opt_result.impossible_models) if opt_result.impossible_models else [],
            "strange_models": list(opt_result.strange_models) if opt_result.strange_models else [],
            "missing_models": list(opt_result.missing_models) if opt_result.missing_models else [],
            "impossible_instances": dict(opt_result.impossible_instances) if opt_result.impossible_instances else {}
        }
        
        # Convert models_data from NamedTuples to dicts
        if opt_result.models_data:
            for model_name, model_data in opt_result.models_data.items():
                raw_optimization_result["models_data"][model_name] = {
                    "requiredInstances": {}
                }
                
                for setup_id, instance_data in model_data.requiredInstances.items():
                    raw_optimization_result["models_data"][model_name]["requiredInstances"][setup_id] = {
                        "instance_num": instance_data.instance_num,
                        "gpu_type": instance_data.gpu_type,
                        "gpu_number": instance_data.gpu_number
                    }
    
    # Generate Go-compatible OptimizedAlloc objects for each VariantAutoscaling
    # This mirrors the Go optimizer behavior that creates one allocation per VariantAutoscaling
    # Key insight: We need to map from Python's model-based results back to VariantAutoscaling-specific allocations
    for va in go_variant_autoscalings:
        va_name = va["metadata"]["name"]
        va_namespace = va["metadata"]["namespace"]
        model_id = va["spec"]["modelID"]
        
        # Use va.Name as key (like Go optimizer does: optimizedAllocMap[vaName] = ...)
        # Go doesn't use FullName for the map key, just vaName
        allocation_key = va_name
        
        # Find allocation for this model in optimization result
        allocation_found = False
        if opt_result and opt_result.models_data and model_id in opt_result.models_data:
            model_data = opt_result.models_data[model_id]
            
            # For now, if the model has any allocation, assign it to this VariantAutoscaling
            # TODO: In the future, we might want more sophisticated mapping based on
            # accelerator requirements in va["spec"]["modelProfile"]["accelerators"]
            total_replicas = 0
            accelerator = ""
            
            for setup_id, instance_data in model_data.requiredInstances.items():
                total_replicas += instance_data.instance_num
                if not accelerator:  # Use first accelerator type found
                    accelerator = instance_data.gpu_type
            
            if total_replicas > 0:
                optimized_allocations[allocation_key] = {
                    "lastRunTime": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                    "accelerator": accelerator,
                    "numReplicas": total_replicas
                }
                allocation_found = True
        
        # If no allocation found for this VariantAutoscaling, it means Python optimizer
        # determined it's not feasible or should have 0 replicas
        if not allocation_found:
            print(f"Python: No feasible allocation found for VariantAutoscaling {allocation_key} (model: {model_id})")
    
    print(f"Python: Generated {len(optimized_allocations)} OptimizedAlloc objects for {len(go_variant_autoscalings)} VariantAutoscalings")
    
    return {
        "optimized_allocations": optimized_allocations,
        "raw_optimization_result": raw_optimization_result,
        "success": True,
        "error": "",
        "processing_time": 0.0,  # Will be calculated by caller
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    }


def main():
    """Main entry point for Go integration."""
    if len(sys.argv) != 3:
        print("Usage: python go_autoscaler_wrapper.py <input_file> <output_file>", file=sys.stderr)
        sys.exit(1)
    
    input_file = sys.argv[1]
    output_file = sys.argv[2]
    
    start_time = time.time()
    
    try:
        print(f"Python: Reading Go input from {input_file}")
        
        # Read Go input data
        with open(input_file, 'r') as f:
            go_data = json.load(f)
        
        print(f"Python: Loaded Go data with keys: {list(go_data.keys())}")
        
        # Transform to Python format
        python_input = transform_go_input_to_python(go_data)
        
        # Run optimization
        optimization_result = run_optimization(python_input)
        
        # Transform result back to Go format
        go_output = transform_python_output_for_go(optimization_result, go_data.get("variant_autoscalings", []))
        go_output["processing_time"] = time.time() - start_time
        
        # Write output
        with open(output_file, 'w') as f:
            json.dump(go_output, f, indent=2)
        
        print(f"Python: Successfully wrote optimization results to {output_file}")
        print(f"Python: Processing completed in {go_output['processing_time']:.2f} seconds")
        
    except Exception as e:
        error_output = {
            "optimized_allocations": {},
            "raw_optimization_result": None,
            "success": False,
            "error": str(e),
            "processing_time": time.time() - start_time,
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        }
        
        with open(output_file, 'w') as f:
            json.dump(error_output, f, indent=2)
        
        print(f"Python: Error occurred: {e}", file=sys.stderr)
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()