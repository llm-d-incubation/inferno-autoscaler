import pytest
import tempfile
import json
import os
from unittest.mock import patch, MagicMock
from cmd_folder.autoscaler import convert_variants, optimization_result_to_dict, connect_with_files
from optimizer.opt_types import VariantData, OptimizationResult, InstancesData, OptimizationResultsInstances


def test_optimization_result_to_dict():
    opt_result = OptimizationResult(
        gpu_after_allocation={"a100": 2},
        models_data={
            "model1": OptimizationResultsInstances(requiredInstances={
                "setupA": InstancesData(instance_num=2, gpu_type="a100", gpu_number=1)
            })
        },
        impossible_models=["modelX"],
        strange_models=["modelY"],
        missing_models=["modelZ"],
        impossible_instances=["modelW"]
    )

    result_dict = optimization_result_to_dict(opt_result)

    assert "gpu_after_allocation" in result_dict
    assert "models_data" in result_dict
    assert result_dict["models_data"]["model1"]["requiredInstances"]["setupA"]["instance_num"] == 2

@patch("cmd_folder.autoscaler.GPUOptimizer")
@patch("cmd_folder.autoscaler.DoCplexOptimizationModel")
def test_connect_with_files(mock_solver_class, mock_optimizer_class):
    input_data = {
        "variants_data": {
            "model1": {
                "setupA": {
                    "variant_id": "setupA",
                    "gpu_type": "a100",
                    "gpu_number": 1,
                    "role": "default",
                    "slo_class": "standard",
                    "max_service_rate": 25.0,
                    "max_concurrency": 5.0
                }
            }
        },
        "required_service_rates": {"model1": 20},
        "gpu_limits": {"a100": 2},
        "gpu_cost": {"a100": 1}
    }

    expected_output = OptimizationResult(
        gpu_after_allocation={"a100": 2},
        models_data={
            "model1": OptimizationResultsInstances(requiredInstances={
                "setupA": InstancesData(instance_num=2, gpu_type="a100", gpu_number=1)
            })
        },
        impossible_models=[],
        strange_models=[],
        missing_models=[],
        impossible_instances=[]
    )

    mock_optimizer = MagicMock()
    mock_optimizer.get_optimal_gpu_allocation.return_value = expected_output
    mock_optimizer_class.return_value = mock_optimizer
    mock_solver_class.return_value = MagicMock()

    with tempfile.NamedTemporaryFile(delete=False, mode='w', suffix=".json") as input_file:
        json.dump(input_data, input_file)
        input_file_path = input_file.name

    with tempfile.NamedTemporaryFile(delete=False, mode='r+', suffix=".json") as output_file:
        output_file_path = output_file.name

    try:
        with patch("sys.argv", ["autoscaler.py", input_file_path, output_file_path]):
            connect_with_files()

        with open(output_file_path) as f:
            result = json.load(f)
            assert "gpu_after_allocation" in result
            assert result["models_data"]["model1"]["requiredInstances"]["setupA"]["instance_num"] == 2

    finally:
        os.remove(input_file_path)
        os.remove(output_file_path)