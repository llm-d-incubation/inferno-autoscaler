import json
import sys
import time

from optimizer.abstract_optimization_model import AbstractOptimizationModel
from optimizer.gpu_optimizer import GPUOptimizer
from optimizer.opt_types import *
from optimizer.docplex_optimization_model import *

def convert_variants(raw_variants: dict[str, dict[str, dict]]) -> dict[str, dict[str, VariantData]]:
    return {
        model: {
            setup: VariantData( **variant_data)
            for setup, variant_data in setups.items()
        }
        for model, setups in raw_variants.items()
    }

def optimization_result_to_dict(opt_res: OptimizationResult) -> dict:
    return {
        "gpu_after_allocation": opt_res.gpu_after_allocation,
        "models_data": {
            model: {
                "requiredInstances": {
                    setup: {
                        "instance_num": inst.instance_num,
                        "gpu_type": inst.gpu_type,
                        "gpu_number": inst.gpu_number,
                    }
                    for setup, inst in model_data.requiredInstances.items()
                }
            }
            for model, model_data in opt_res.models_data.items()
        },
        "impossible_models": opt_res.impossible_models,
        "strange_models": opt_res.strange_models,
        "missing_models": opt_res.missing_models,
        "impossible_instances": opt_res.impossible_instances,
    }


def connect_with_files():
    """
    Reads structured data from an input file, processes it,
    and writes the result to an output file.
    """
    if len(sys.argv) != 3:
        print("Usage: python process.py <input_file_path> <output_file_path>", file=sys.stderr)
        sys.exit(1)

    input_file = sys.argv[1]
    output_file = sys.argv[2]

    print(f"Python: Reading from {input_file}")

    try:
        with open(input_file, 'r') as f:
            data = json.load(f)

        deployed_instances = data.get("deployed_instances", {})
        min_replicas = data.get("min_replicas", {})
        max_replicas = data.get("max_replicas", {})

        data['variants_data'] = convert_variants(data['variants_data'])

        solver = DoCplexOptimizationModel(name="optimization")
        mdl = GPUOptimizer(solver)
        result = mdl.get_optimal_gpu_allocation(
            variants=data["variants_data"],
            required_rates=data["required_service_rates"],
            gpu_limits=data["gpu_limits"],
            gpu_cost=data["gpu_cost"],
            scale_to_zero=data.get("scale_to_zero", []),
            deployed_instances=deployed_instances,
            min_replicas=min_replicas,
            max_replicas=max_replicas
        )

        time.sleep(1)

        result_dict = optimization_result_to_dict(result)

        with open(output_file, "w") as f:
            json.dump(result_dict, f, indent=4)

        print(f"Python: Successfully processed data and wrote result to {output_file}")

    except Exception as e:
        error_result = {
            "error": str(e),
            "is_success": False
        }
        with open(output_file, 'w') as f:
            json.dump(error_result, f, indent=4)
        print(f"Python: An error occurred: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    connect_with_files()