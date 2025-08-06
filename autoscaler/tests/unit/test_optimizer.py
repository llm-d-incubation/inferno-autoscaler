from optimizer.gpu_optimizer import *
from optimizer.docplex_optimization_model import *
from optimizer.opt_types import *
import pytest

def test_get_optimal_gpu_allocation():
    model = DoCplexOptimizationModel("test")
    optimizer = GPUOptimizer(model)

    models = ["google-flan-xl", "lama-8b", "lama-80b", "mistral"]
    gpu_types = ["A100", "H100", "A100+"]
    gpu_cost = {"A100": 1.0, "H100": 1.3, "A100+": 1.2}
    gpu_limits = {"A100": 15, "H100": 17, "A100+": 20}


    # varients data: role, slo_class, max_svc_rate, max_concurrency
    variants = {
        "google-flan-xl": {
            "flan-a100": VariantData("flan-a100", "A100", 6, "inference", "standard", 15.0, 4),
            "flan-h100": VariantData("flan-h100", "H100", 3, "inference", "standard", 20.0, 4)
        },
        "lama-8b": {
            "lama8-a100+": VariantData("lama8-a100+", "A100+", 5, "inference", "standard", 10.0, 2),
            "lama8-a100": VariantData("lama8-a100", "A100", 3, "inference", "standard", 8.0, 2)
        },
        "lama-80b": {
            "lama80-h100": VariantData("lama80-h100", "H100", 4, "inference", "premium", 12.0, 1),
            "lama80-a100+": VariantData("lama80-a100+", "A100+", 7, "inference", "premium", 10.0, 1)
        },
        "mistral": {
            "mistral-a100": VariantData("mistral-a100", "A100", 4, "inference", "standard", 14.0, 3),
            "mistral-h100": VariantData("mistral-h100", "H100", 1, "inference", "standard", 12.0, 3)
        }
    }

    required_rates = {
        "google-flan-xl": 25.0,
        "lama-8b": 20.0,
        "lama-80b": 22.0,
        "mistral": 15.0
    }

    scale_to_zero = ["lama-80b"]
    min_replicas = {
        "google-flan-xl": {"flan-a100": 1},
        "mistral": {"mistral-h100": 1}
    }
    max_replicas = {
        "lama-8b": {"lama8-a100+": 3, "lama8-a100": 2},
        "mistral": {"mistral-a100": 2, "mistral-h100": 2}
    }

    deployed_instances = {
        "google-flan-xl": {"flan-a100": 1, "flan-h100": 0},
        "lama-80b": {"lama80-h100": 1, "lama80-a100+": 0},
        "lama-8b": {"lama8-a100+": 1, "lama8-a100": 0},
        "mistral": {"mistral-a100": 0, "mistral-h100": 0}
}
    change_penalty = 0
    homogeneous = True

    result = optimizer.get_optimal_gpu_allocation(
        variants,
        required_rates,
        gpu_limits,
        gpu_cost,
        scale_to_zero,
        deployed_instances,
        change_penalty,
        homogeneous,
        max_replicas,
        min_replicas)

    assert (result.gpu_after_allocation, result.models_data) == ({'A100': 3, 'A100+': 10, 'H100': 7}, {'google-flan-xl': OptimizationResultsInstances(requiredInstances={'flan-a100': InstancesData(instance_num=2, gpu_type='A100', gpu_number=6), 'flan-h100': InstancesData(instance_num=0, gpu_type='H100', gpu_number=3)}), 'lama-80b': OptimizationResultsInstances(requiredInstances={'lama80-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=4), 'lama80-a100+': InstancesData(instance_num=0, gpu_type='A100+', gpu_number=7)}), 'lama-8b': OptimizationResultsInstances(requiredInstances={'lama8-a100+': InstancesData(instance_num=2, gpu_type='A100+', gpu_number=5), 'lama8-a100': InstancesData(instance_num=0, gpu_type='A100', gpu_number=3)}), 'mistral': OptimizationResultsInstances(requiredInstances={'mistral-a100': InstancesData(instance_num=0, gpu_type='A100', gpu_number=4), 'mistral-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=1)})})



    deployed_instances = {
        "google-flan-xl": {"flan-a100": 1, "flan-h100": 1},
        "lama-80b": {"lama80-h100": 1, "lama80-a100+": 0},
        "lama-8b": {"lama8-a100+": 1, "lama8-a100": 0},
        "mistral": {"mistral-a100": 0, "mistral-h100": 0}
    }


    change_penalty = 3
    homogeneous = False

    result = optimizer.get_optimal_gpu_allocation(
        variants,
        required_rates,
        gpu_limits,
        gpu_cost,
        scale_to_zero,
        deployed_instances,
        change_penalty,
        homogeneous,
        max_replicas,
        min_replicas)

    assert (result.gpu_after_allocation, result.models_data) == ({'A100': 9, 'A100+': 10, 'H100': 4}, {'google-flan-xl': OptimizationResultsInstances(requiredInstances={'flan-a100': InstancesData(instance_num=1, gpu_type='A100', gpu_number=6), 'flan-h100': InstancesData(instance_num=1, gpu_type='H100', gpu_number=3)}), 'lama-80b': OptimizationResultsInstances(requiredInstances={'lama80-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=4), 'lama80-a100+': InstancesData(instance_num=0, gpu_type='A100+', gpu_number=7)}), 'lama-8b': OptimizationResultsInstances(requiredInstances={'lama8-a100+': InstancesData(instance_num=2, gpu_type='A100+', gpu_number=5), 'lama8-a100': InstancesData(instance_num=0, gpu_type='A100', gpu_number=3)}), 'mistral': OptimizationResultsInstances(requiredInstances={'mistral-a100': InstancesData(instance_num=0, gpu_type='A100', gpu_number=4), 'mistral-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=1)})})

    max_replicas = {
        "lama-8b": {"lama8-a100+": 3, "lama8-a100": 2},
        "mistral": {"mistral-a100": 2, "mistral-h100": 2}
    }

    scale_to_zero = []

    result = optimizer.get_optimal_gpu_allocation(
        variants,
        required_rates,
        gpu_limits,
        gpu_cost,
        scale_to_zero,
        deployed_instances,
        change_penalty,
        homogeneous,
        max_replicas,
        min_replicas)

    assert (result.gpu_after_allocation, result.models_data) == ({'A100': 9, 'A100+': 10, 'H100': 4}, {'google-flan-xl': OptimizationResultsInstances(requiredInstances={'flan-a100': InstancesData(instance_num=1, gpu_type='A100', gpu_number=6), 'flan-h100': InstancesData(instance_num=1, gpu_type='H100', gpu_number=3)}), 'lama-80b': OptimizationResultsInstances(requiredInstances={'lama80-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=4), 'lama80-a100+': InstancesData(instance_num=0, gpu_type='A100+', gpu_number=7)}), 'lama-8b': OptimizationResultsInstances(requiredInstances={'lama8-a100+': InstancesData(instance_num=2, gpu_type='A100+', gpu_number=5), 'lama8-a100': InstancesData(instance_num=0, gpu_type='A100', gpu_number=3)}), 'mistral': OptimizationResultsInstances(requiredInstances={'mistral-a100': InstancesData(instance_num=0, gpu_type='A100', gpu_number=4), 'mistral-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=1)})})



    max_replicas = {}
    min_replicas = {}

    result = optimizer.get_optimal_gpu_allocation(
        variants,
        required_rates,
        gpu_limits,
        gpu_cost,
        scale_to_zero,
        deployed_instances,
        change_penalty,
        homogeneous,
        max_replicas,
        min_replicas)

    assert (result.gpu_after_allocation, result.models_data) == ({'A100': 3, 'A100+': 15, 'H100': 4}, {'google-flan-xl': OptimizationResultsInstances(requiredInstances={'flan-a100': InstancesData(instance_num=1, gpu_type='A100', gpu_number=6), 'flan-h100': InstancesData(instance_num=1, gpu_type='H100', gpu_number=3)}), 'lama-80b': OptimizationResultsInstances(requiredInstances={'lama80-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=4), 'lama80-a100+': InstancesData(instance_num=0, gpu_type='A100+', gpu_number=7)}), 'lama-8b': OptimizationResultsInstances(requiredInstances={'lama8-a100+': InstancesData(instance_num=1, gpu_type='A100+', gpu_number=5), 'lama8-a100': InstancesData(instance_num=2, gpu_type='A100', gpu_number=3)}), 'mistral': OptimizationResultsInstances(requiredInstances={'mistral-a100': InstancesData(instance_num=0, gpu_type='A100', gpu_number=4), 'mistral-h100': InstancesData(instance_num=2, gpu_type='H100', gpu_number=1)})})