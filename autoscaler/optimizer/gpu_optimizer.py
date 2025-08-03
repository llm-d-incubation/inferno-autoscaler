import logging

from optimizer.opt_types import VariantData, InstancesData, OptimizationResultsInstances, OptimizationResult
from optimizer.abstract_optimization_model import AbstractOptimizationModel

logger = logging.getLogger('optimization')


class GPUOptimizer:
    def __init__(self, mdl: AbstractOptimizationModel):
        self.mdl = mdl

    def get_optimal_gpu_allocation(
        self,
        variants: dict[str, dict[str, VariantData]],
        required_rates: dict[str, float],
        gpu_limits: dict[str, int],
        gpu_cost: dict[str, float],
        scale_to_zero: list[str],
        deployed_instances: dict[str, dict[str, int]] | None = None,
        change_penalty: float = 0,
        homogeneous: bool = False,
        max_replicas: dict[str, dict[str, int]] = dict(),
        min_replicas: dict[str, dict[str, int]] = dict(),
    ) -> OptimizationResult | None:
        """Optimize the number and type of instances we allocate to each model.

        :param variants: dictionary[
            keys: model name,
            values: dictionary[
                keys: setup_id,
                values: VariantData
        :type: dict[str, dict[str, VariantData]]

        :param required_rates: prequire service rates for requests per model
        :type:  dict[str, float]

        :param gpu_limits: number of available gpus per gpu type
        :type: dict[str, int]

        :param gpu_cost: cost per gpu type
        :type: dict[str, float]

        :param scale_to_zero: indicates if we can scale to zero some models.
            list that specify specific models that can get 0 instances while other no
        :type: list[str]
        
        :param deployed_instances: dictionary[
            keys: model name,
            values: dictionary[
                keys: setup_id,
                values: number of instances]],
            default None
        :type: dict[str, dict[str, int]] | None

        :param change_penalty: penalty for a case we change one of deployed instances to other type, default 0
        :type:  float

        :param homogeneous: should we use homogeneous (mode 2) or heterogeneous (mode 3) deployments, default False
        :type:  bool

        :param min_replicas: Minimum number of instances per deployment
        :type: dict[str, dict[str, int]]

        :param max_replicas: Maximum number of instances per deployment
        :type: dict[str, dict[str, int]]

        :return Optimization results including number of available (-missing) GPUs, number of instances per deployment
            type, and various lists of models/deployments that not supported by optimization
        :rtype: OptimizationResultsV2

        """


        # number of instances of each setup per model
        eta = self.mdl.add_integer_vars(
            [
                (model_id, setup_id)
                for model_id, setups in variants.items()
                for setup_id in setups.keys()
            ],
            "eta",
        )
        # add upper bounds on instances:
        if max_replicas:
            self.mdl.add_constraints([eta[model_id, setup_id] <= replicas
                                      for model_id, model_data in max_replicas.items()
                                      for setup_id, replicas in model_data.items() if replicas is not None],
                                     "max_replicas_ct")

        if min_replicas:
            self.mdl.add_constraints([eta[model_id, setup_id] >= replicas
                                      for model_id, model_data in min_replicas.items()
                                      for setup_id, replicas in model_data.items() if replicas],
                                     "min_replicas_ct")
        # sos1 to accept only homogeneous deployments
        if homogeneous:
            [
                self.mdl.add_sos1(
                    [eta[(model_id, setup_id)] for setup_id in variants[model_id].keys()]
                )
                for model_id in required_rates.keys()
            ]
        # allowing some models get zero instances in case it is possible, otherwise requires all of the models have at
        # least one instance

        self.mdl.add_constraints(
            [
                self.mdl.sum(eta[(model_id, setup_id)] for setup_id in setups.keys()) >= 1
                for model_id, setups in variants.items()
                if model_id not in scale_to_zero and sum(deployed_instances.get(model_id, {'a': 0}).values()) > 0            ],
            "min_instances_ct",
        )

        # instances should serve input rate
        self.mdl.add_constraints(
            [
                required_rates[model_id]
                <= self.mdl.sum(
                    [
                        setup_data.max_service_rate
                        * eta[(model_id, setup_id)]
                        for setup_id, setup_data in variants[model_id].items()
                    ]
                )
                for model_id in required_rates.keys()
            ],
            "service_rate_ct",
        )

        setups_per_gpu = {
            gpu_type: [
                (model_id, setup_id, setup_data.gpu_number)
                for model_id, setups in variants.items()
                for setup_id, setup_data in setups.items()
                if setup_data.gpu_type == gpu_type
            ]
            for gpu_type in gpu_limits.keys()
        }
        setups_per_gpu = {
            gpu_type: setup_data for gpu_type, setup_data in setups_per_gpu.items() if setup_data
        }

        if setups_per_gpu.keys():
            max_gpu_cost = max(gpu_cost.get(gpu_type, 0) for gpu_type in setups_per_gpu.keys())
        else:
            max_gpu_cost = 0

        # total number of gpus should not exceed gpu limit
        self.mdl.add_constraints(
            [
                self.mdl.sum(
                    [setup_data[2] * eta[(setup_data[0], setup_data[1])] for setup_data in gpu_setups]
                )
                <= gpu_limits[gpu_type]
                for gpu_type, gpu_setups in setups_per_gpu.items()
            ],
            "gpu_limit_ct",
        )

        if change_penalty > 0:
            # creating variables to accumulate instance differences
            delta = self.mdl.add_continuous_vars(
                [
                    (model_id, setup_id)
                    for model_id in deployed_instances.keys()
                    for setup_id in variants[model_id].keys()
                ],
                "delta",
            )
            # calculate differences between input_rate and maximum_service_rate for current deployment

            delta_rates = {
                model_id: sum(
                    [
                        inst_num * variants[model_id][setup_id].max_service_rate
                        for setup_id, inst_num in inst_data.items()
                    ]
                )
                          - required_rates.get(model_id, 0)
                for model_id, inst_data in deployed_instances.items()
            }
            # add constraints to define differences between current and planned allocations
            self.mdl.add_constraints(
                [
                    delta[(model_id, setup_id)] >= eta[(model_id, setup_id)] - inst_num
                    if delta_rates[model_id] >= 0
                    else delta[(model_id, setup_id)] >= inst_num - eta[(model_id, setup_id)]
                    for model_id, setup_data in deployed_instances.items()
                    for setup_id, inst_num in setup_data.items()
                ],
                "instance_change_ct",
            )
        else:
            delta = 0
        # integer number of gpus for objective function
        used_gpu = self.mdl.add_integer_vars(setups_per_gpu.keys(), "used_gpu")
        self.mdl.add_constraints(
            [
                used_gpu[gpu_type]
                >= self.mdl.sum(
                    [setup_data[2] * eta[(setup_data[0], setup_data[1])] for setup_data in gpu_setups]
                )
                for gpu_type, gpu_setups in setups_per_gpu.items()
            ],
            "int_gpu_ct",
        )
        # objective function
        self.mdl.minimize(
            self.mdl.sum(
                [gpu_cost.get(gpu_type, 0) * used_gpu[gpu_type] for gpu_type in setups_per_gpu.keys()]
            )
            + change_penalty * self.mdl.sum(delta) * max_gpu_cost
        )

        lp_string, vars_names = self.mdl.print_lp()
        logger.debug("Optimization model to solve: \n" + lp_string)

        # solving
        result = self.mdl.solve()

        if result is not None:
            instances = self.mdl.get_solution_values(eta)
            gpus = self.mdl.get_solution_values(used_gpu)
            instances_per_model = {
                model_id: {
                    setup_id: InstancesData(
                        round(instances[(model_id, setup_id)]), *setup_data[1:3]
                    )
                    for setup_id, setup_data in setups.items()
                }
                for model_id, setups in variants.items()
            }

            gpus, instances = {gpu_type: round(gpu_limits[gpu_type] - gpu_num) for gpu_type, gpu_num in gpus.items()}, {
                model_id: OptimizationResultsInstances(
                    instances  # , ServiceRateTuple(sr[model_id][1], sr[model_id][0])
                )
                for model_id, instances in instances_per_model.items()
            }
            logger.info(f'get_optimal_gpu_allocation: returning optimal result {gpus, instances}')

            if not setups_per_gpu.keys():
                gpus = {gpu_type: round(limit) for gpu_type, limit in gpu_limits.items()}
            logger.info(f'get_optimal_gpu_allocation: instances after post solve {instances}')
            return OptimizationResult(
                gpu_after_allocation=gpus,
                models_data=instances,
                impossible_models=[],
                strange_models=[],
                missing_models=[],
                impossible_instances={}
            )
        else:
            logger.warning('get_optimal_gpu_allocation: returning empty result')
            return OptimizationResult(
    gpu_after_allocation={},
    models_data={},
    impossible_models=[],
    strange_models=[],
    missing_models=[],
    impossible_instances={}
)

