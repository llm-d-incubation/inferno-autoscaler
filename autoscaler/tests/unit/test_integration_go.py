import json
import tempfile
from pathlib import Path
from optimizer.opt_types import InstancesData, OptimizationResultsInstances
from integration.integration_go_python import *

def test_save_gpu_optimizer_output_creates_file():

    gpus = {"A100": 5, "H100": 3}
    instances = {
        "google-flan-xl": OptimizationResultsInstances(
            requiredInstances={
                "setup-1": InstancesData(1, "A100", 1.0),
                "setup-2": InstancesData(2, "H100", 2.0),
            }
        )
    }

    with tempfile.TemporaryDirectory() as tmpdir:
        output_file = Path(tmpdir) / "result.json"
        print(output_file)

        save_gpu_optimizer_output(gpus, instances, str(output_file))



        with open(output_file) as f:
            data = json.load(f)

        assert data["gpus"] == {"A100": 5, "H100": 3}
        assert data["instances"]["google-flan-xl"]["setup-1"]["instance_num"] == 1
        assert data["instances"]["google-flan-xl"]["setup-1"]["gpu_type"] == "A100"
        assert data["instances"]["google-flan-xl"]["setup-1"]["gpu_number"] == 1.0
        assert data["instances"]["google-flan-xl"]["setup-2"]["instance_num"] == 2
        assert data["instances"]["google-flan-xl"]["setup-2"]["gpu_type"] == "H100"
        assert data["instances"]["google-flan-xl"]["setup-2"]["gpu_number"] == 2.0