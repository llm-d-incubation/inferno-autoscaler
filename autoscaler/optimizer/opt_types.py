from typing import NamedTuple, NewType
from dataclasses import dataclass, field



class VariantData(NamedTuple):
    """VariantData tuple


    """

    variant_id: str
    gpu_type: str
    gpu_number: float
    role: str
    slo_class: str
    max_service_rate: float
    max_concurrency: float


class InstancesData(NamedTuple):
    """InstancesData tuple

    tuple[0]: Number of instances

    tuple[1]: gpu_type,

    tuple[2]: gpu_number,

    """

    instance_num: int
    gpu_type: str
    gpu_number: float
    #service_rate: ServiceRateData


class OptimizationResultsInstances(NamedTuple):
    requiredInstances: dict[str, InstancesData]

class OptimizationResult(NamedTuple):
    gpu_after_allocation: dict[str, int]
    models_data: dict[str, OptimizationResultsInstances]
    impossible_models: list[str] = []
    strange_models: list[str] = []
    missing_models: list[str] = []
    impossible_instances: dict[str, list[str]] = {}


@dataclass
class GPUOptimizationInput:
    variants_data: dict[str, dict[str, VariantData]]
    gpu_limits: dict[str, int]
    required_service_rates: dict[str, float]
    gpu_cost: dict[str, float]
    scale_to_zero: list[str] = field(default=False)
    deployed_instances: dict[str, dict[str, int]] | None = None
    min_replicas: dict[str, dict[str, int]] = field(default_factory=dict)
    max_replicas: dict[str, dict[str, int]] = field(default_factory=dict)
