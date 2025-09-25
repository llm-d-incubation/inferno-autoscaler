<p style="font-size: 25px" align="center"><b>workload-variant-autoscaler</b></p>

The workload-variant-autoscaler assigns GPU types to inference model servers and decides on the number of replicas for each model for a given request traffic load and classes of service, as well as the batch size.

**Table of contents**

- [Description](#description)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
- [Quickstart Guide: Installation of workload-variant-autoscaler along with llm-d infrastructure emulated deployment on a Kind cluster](#quickstart-guide-installation-of-workload-variant-autoscaler-along-with-llm-d-infrastructure-emulated-deployment-on-a-kind-cluster)
  - [Showing workload-variant-autoscaler scaling replicas up and down](#showing-workload-variant-autoscaler-scaling-replicas-up-and-down)
  - [Uninstalling llm-d and workload-variant-autoscaler](#uninstalling-llm-d-and-workload-variant-autoscaler)
- [Running E2E tests](#running-e2e-tests)
- [Details on emulated mode deployment on Kind](#details-on-emulated-mode-deployment-on-kind)
  - [Deployment](#deployment)
  - [Uninstall](#uninstall)
  - [Prometheus vllme setup](#prometheus-vllme-setup)
    - [Note: The above script already deploys emulated vllm server:](#note-the-above-script-already-deploys-emulated-vllm-server)
  - [Inferno custom metrics](#inferno-custom-metrics)
- [Contributing](#contributing)


## Description

The workload-variant-autoscaler is a Kubernetes controller that performs optimizated autoscaling using the below components:

![Diagram](docs/diagrams/inferno-WVA-design.png)

Reconciler:

The controller is implemented using the controller-runtime framework, which reconciles the namespace-scoped VariantAutoscaling objects created by the platform administrator, one per model.Due to runtime variability in model behavior (e.g., differences in prompt lengths, output sizes, or server-level contention), we treat model analysis as a continuously reconciled step during every autoscaler loop.

Collector(s):
The collectors that gather cluster data about the cluster state and the state of vllm servers running inside the controller.

Actuator:
The actuator is responsible for emitting metrics to the desired sources, like Prometheus, or changing replicas of existing deployments running on the cluster, which is the case with the Inferno autoscaler.

Model Analyzer:
Model Analyzer is a component that runs per model to perform scaling, estimation, prediction, and tuning.

Optimizer:
Optimizer consumes output of Model Analyzer to make global scaling decisions.

Proposed sources:
These include the new [API proposal](https://docs.google.com/document/d/1j2KRAT68_FYxq1iVzG0xVL-DHQhGVUZBqiM22Hd_0hc/edit?usp=drivesdk&resourcekey=0-5cSovS8QcRQNYXj0_kRMiw), which is expected to work in conjunction with the inference scheduler (EPP) to provide insights into the request scheduler's dispatching logic.

For more details please refer to the community proposal [here](https://docs.google.com/document/d/1n6SAhloQaoSyF2k3EveIOerT-f97HuWXTLFm07xcvqk/edit?tab=t.0).

Modeling and optimization techniques used in the workload-variant-autoscaler are described in this [document](./docs/modeling-optimization.md).

## Getting Started

### Prerequisites
- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.32.0+.
- Access to a Kubernetes v1.32.0+ cluster.

**Note**: To verify autoscaling when the workload-variant-autoscaler is deployed, install the **HorizontalPodAutoscaler** following this [quick setup guide](docs/hpa-integration.md)

## Quickstart Guide: Installation of workload-variant-autoscaler along with llm-d infrastructure emulated deployment on a Kind cluster

Use this target to spin up a local test environment integrated with llm-d core components:

```sh
make deploy-llm-d-inferno-emulated-on-kind

# prebuilt image
# IMG=ghcr.io/llm-d/workload-variant-autoscaler:latest
```

This target deploys an environment ready for testing, integrating the llm-d infrastructure and the workload-variant-autoscaler.

The default set up:
- Deploys a Kind cluster with nodes, 2 GPUs per node, mixed vendors with fake GPU resources
- Includes the Inferno autoscaler
- Installs the [llm-d core infrastructure for simulation purposes](https://github.com/llm-d-incubation/llm-d-infra/blob/main/quickstart/examples/sim/README.md)
- Includes vLLM emulator and load generator (OpenAI-based)

**Optionally: Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/inferno-controller:tag
```

Cross-platform or multi-arch images can be built using `make docker-buildx`. When using Docker as your container tool, make
sure to create a builder instance. Refer to [Multi-platform images](https://docs.docker.com/build/building/multi-platform/)
for documentation on building mutli-platform images with Docker. You can change the destination platform(s) by setting `PLATFORMS`, e.g.:

```sh
PLATFORMS=linux/arm64,linux/amd64 make docker-buildx

# prebuilt image
# IMG=ghcr.io/llm-d/workload-variant-autoscaler:latest
# Built for: linux/arm64, linux/amd64, linux/s390x, linux/ppc64le
```

To curl the Gateway:
1. Find the gateway service:
```sh
kubectl get service -n llm-d-sim 
NAME                          TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)             AGE
gaie-sim-epp                  ClusterIP   10.96.158.61   <none>        9002/TCP,9090/TCP   2m17s
infra-sim-inference-gateway   NodePort    10.96.1.46     <none>        80:31214/TCP        2m12s
vllme-service                 NodePort    10.96.90.51    <none>        80:30000/TCP        4m3s
```

2. Then `port-forward` the gateway service:

**Note**: Make sure the `infra-sim-inference-gateway` pod is in running state
```sh
kubectl port-forward -n llm-d-sim service/infra-sim-inference-gateway 8000:80
```

**Note**: Since the environment uses vllm-emulator, the **Criticality** parameter is set to `critical` for emulation purposes.

### Showing workload-variant-autoscaler scaling replicas up and down
1. Target the deployed vLLM-emulator servers by deploying the VariantAutoscaling (Va) object:
```sh
kubectl apply -f hack/vllme/deploy/vllme-setup/vllme-variantautoscaling.yaml
``` 

2. Before starting the load generator, we can see that the workload-variant-autoscaler is not scaling up existing deployments:

```sh
kubectl get deployments -n llm-d-sim
NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
gaie-sim-epp                  1/1     1            1           5m12s
infra-sim-inference-gateway   1/1     1            1           5m7s
vllme-deployment              1/1     1            1           5m58s

kubectl get variantautoscalings.llmd.ai -n llm-d-sim 
NAME               MODEL             ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
vllme-deployment   default/default   A100          1                 1           4m31s
```

3. Install the **HorizontalPodAutoscaler** following this [quick setup guide](docs/hpa-integration.md)

4. Launch the `loadgen.py` load generator to send requests to the `v1/chat/completions` endpoint:
```sh
cd hack/vllme/vllm_emulator
pip install -r requirements.txt # if not already installed
python loadgen.py --model default/default  --rate '[[1200, 40]]' --url http://localhost:8000/v1 --content 50

# Default parameters
# Starting load generator with deterministic mode
# Server Address: http://localhost:8000/v1
# Request Rate = 60.0
# Model: gpt-1337-turbo-pro-max
# Content Length: 150

# for deterministic dynamically changing rate
python loadgen.py --model default/default  --rate '[[120, 60], [120, 80]]' --url http://localhost:8000/v1

# First 120 seconds (2 minutes): Send 60 requests per minute
# Next 120 seconds (2 minutes): Send 80 requests per minute
# Can use any combination of rates such as [[120, 60], [120, 80], [120, 40]]
```

- To request the port-forwarded gateway, use **--url http://localhost:8000/v1**
- To request the deployed vLLM emulator servers, insert: "**--model default/default**"

5. **Scaling out**: after launching the load generator script `loadgen.py` with related RPM and content length (such as **RPM=40** and **content length** equal to **50**), we can see the logs from the workload-variant-autoscaler controller effectively computing the optimal resource allocation and emitting metrics to Prometheus.

```sh
kubectl logs -n workload-variant-autoscaler-system deployments/workload-variant-autoscaler-controller-manager
#...
{"level":"DEBUG","ts":"2025-08-25T19:06:55.378Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-control-plane , model - NVIDIA-A100-PCIE-80GB , count - 2 , mem - 81920"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.378Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker , model - AMD-MI300X-192G , count - 2 , mem - 196608"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.378Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker2 , model - Intel-Gaudi-2-96GB , count - 2 , mem - 98304"}
{"level":"INFO","ts":"2025-08-25T19:06:55.378Z","msg":"Found SLO for model - model: default/default, class: Premium, slo-tpot: 24, slo-ttft: 500"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"System data prepared for optimization: - { count: [  {   type: AMD-MI300X-192G,   count: 2  },  {   type: Intel-Gaudi-2-96GB,   count: 2  },  {   type: NVIDIA-A100-PCIE-80GB,   count: 2  } ]}"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  },  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  },  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  } ]}"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Freemium,   model: ibm/granite-13b,   priority: 10,   slo-itl: 200,   slo-ttw: 2000,   slo-tps: 0  },  {   name: Freemium,   model: meta/llama0-7b,   priority: 10,   slo-itl: 150,   slo-ttw: 1500,   slo-tps: 0  },  {   name: Premium,   model: default/default,   priority: 1,   slo-itl: 24,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: meta/llama0-70b,   priority: 1,   slo-itl: 80,   slo-ttw: 500,   slo-tps: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"System data prepared for optimization: - { models: [  {   name: default/default,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: true,  saturationPolicy: None }}"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"System data prepared for optimization: - { servers: [  {   name: vllme-deployment:llm-d-sim,   class: Premium,   model: default/default,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 4,   currentAlloc: {    accelerator: A100,    numReplicas: 1,    maxBatch: 256,    cost: 40,    itlAverage: 20,    waitAverage: 0,    load: {     arrivalRate: 40,     avgLength: 178,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"Optimization solution - system: Solution: \ns=vllme-deployment:llm-d-sim; c=Premium; m=default/default; rate=40; tk=178; sol=1, sat=false, alloc={acc=A100; num=2; maxBatch=4; cost=80, val=40, servTime=21.49347, waitTime=69.7666, rho=0.71789724, maxRPM=25.31145}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-A100-PCIE-80GB, count=2, limit=2, cost=80 \ntotalCost=80 \n"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"Optimized allocation entry - key: vllme-deployment, value: {2025-08-25 19:06:55.38322633 +0000 UTC m=+399.343807608 A100 2}"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"Processing variant - index: 0, variantAutoscaling-name: vllme-deployment, namespace: llm-d-sim, has_optimized_alloc: true"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"EmitReplicaMetrics completed for variantAutoscaling-name: vllme-deployment, current-replicas: 1, desired-replicas: 2, accelerator: A100"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.383Z","msg":"Successfully emitted optimization signals for external autoscalers - variant: vllme-deployment"}
{"level":"DEBUG","ts":"2025-08-25T19:06:55.388Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-25T19:06:55.388Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
```

Checking the deployments and the currently applied VAs: 

```sh
kubectl get deployments -n llm-d-sim 
NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
gaie-sim-epp                  1/1     1            1           7m8s
infra-sim-inference-gateway   1/1     1            1           7m3s
vllme-deployment              2/2     2            2           7m58s

kubectl get variantautoscalings.llmd.ai -n llm-d-sim 
NAME               MODEL             ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
vllme-deployment   default/default   A100          1                 2           6m31s
```

6. **Scaling in**: by stopping the load generator script with a keyboard interrupt, we can see that the workload-variant-autoscaler effectively scales replicas in:
```sh
kubectl logs -n workload-variant-autoscaler-system deployments/workload-variant-autoscaler-controller-manager
# ...
{"level":"DEBUG","ts":"2025-08-26T13:13:25.708Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-control-plane , model - NVIDIA-A100-PCIE-80GB , count - 2 , mem - 81920"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.708Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker , model - AMD-MI300X-192G , count - 2 , mem - 196608"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.708Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker2 , model - Intel-Gaudi-2-96GB , count - 2 , mem - 98304"}
{"level":"INFO","ts":"2025-08-26T13:13:25.708Z","msg":"Found SLO for model - model: default/default, class: Premium, slo-tpot: 24, slo-ttft: 500"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"System data prepared for optimization: - { count: [  {   type: Intel-Gaudi-2-96GB,   count: 2  },  {   type: NVIDIA-A100-PCIE-80GB,   count: 2  },  {   type: AMD-MI300X-192G,   count: 2  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  },  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Premium,   model: default/default,   priority: 1,   slo-itl: 24,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: meta/llama0-70b,   priority: 1,   slo-itl: 80,   slo-ttw: 500,   slo-tps: 0  },  {   name: Freemium,   model: ibm/granite-13b,   priority: 10,   slo-itl: 200,   slo-ttw: 2000,   slo-tps: 0  },  {   name: Freemium,   model: meta/llama0-7b,   priority: 10,   slo-itl: 150,   slo-ttw: 1500,   slo-tps: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"System data prepared for optimization: - { models: [  {   name: default/default,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: true,  saturationPolicy: None }}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"System data prepared for optimization: - { servers: [  {   name: vllme-deployment:llm-d-sim,   class: Premium,   model: default/default,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 4,   currentAlloc: {    accelerator: A100,    numReplicas: 2,    maxBatch: 256,    cost: 80,    itlAverage: 20,    waitAverage: 0,    load: {     arrivalRate: 1.33,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"Optimization solution - system: Solution: \ns=vllme-deployment:llm-d-sim; c=Premium; m=default/default; rate=1.33; tk=0; sol=1, sat=false, alloc={acc=A100; num=1; maxBatch=4; cost=40, val=-40, servTime=20.99, waitTime=0, rho=0, maxRPM=10801.08}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-A100-PCIE-80GB, count=1, limit=2, cost=40 \ntotalCost=40 \n"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"Optimized allocation entry - key: vllme-deployment, value: {2025-08-26 13:13:25.712287789 +0000 UTC m=+1132.957343957 A100 1}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"Processing variant - index: 0, variantAutoscaling-name: vllme-deployment, namespace: llm-d-sim, has_optimized_alloc: true"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"EmitReplicaMetrics completed for variantAutoscaling-name: vllme-deployment, current-replicas: 2, desired-replicas: 1, accelerator: A100"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.712Z","msg":"Successfully emitted optimization signals for external autoscalers - variant: vllme-deployment"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.717Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-26T13:13:25.717Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.717Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-control-plane , model - NVIDIA-A100-PCIE-80GB , count - 2 , mem - 81920"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.717Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker , model - AMD-MI300X-192G , count - 2 , mem - 196608"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.717Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker2 , model - Intel-Gaudi-2-96GB , count - 2 , mem - 98304"}
{"level":"INFO","ts":"2025-08-26T13:13:25.717Z","msg":"Found SLO for model - model: default/default, class: Premium, slo-tpot: 24, slo-ttft: 500"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.719Z","msg":"System data prepared for optimization: - { count: [  {   type: NVIDIA-A100-PCIE-80GB,   count: 2  },  {   type: AMD-MI300X-192G,   count: 2  },  {   type: Intel-Gaudi-2-96GB,   count: 2  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  },  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Premium,   model: default/default,   priority: 1,   slo-itl: 24,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: meta/llama0-70b,   priority: 1,   slo-itl: 80,   slo-ttw: 500,   slo-tps: 0  },  {   name: Freemium,   model: ibm/granite-13b,   priority: 10,   slo-itl: 200,   slo-ttw: 2000,   slo-tps: 0  },  {   name: Freemium,   model: meta/llama0-7b,   priority: 10,   slo-itl: 150,   slo-ttw: 1500,   slo-tps: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"System data prepared for optimization: - { models: [  {   name: default/default,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: true,  saturationPolicy: None }}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"System data prepared for optimization: - { servers: [  {   name: vllme-deployment:llm-d-sim,   class: Premium,   model: default/default,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 4,   currentAlloc: {    accelerator: A100,    numReplicas: 2,    maxBatch: 256,    cost: 80,    itlAverage: 20,    waitAverage: 0,    load: {     arrivalRate: 1.33,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"Optimization solution - system: Solution: \ns=vllme-deployment:llm-d-sim; c=Premium; m=default/default; rate=1.33; tk=0; sol=1, sat=false, alloc={acc=A100; num=1; maxBatch=4; cost=40, val=-40, servTime=20.99, waitTime=0, rho=0, maxRPM=10801.08}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-A100-PCIE-80GB, count=1, limit=2, cost=40 \ntotalCost=40 \n"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"Optimized allocation entry - key: vllme-deployment, value: {2025-08-26 13:13:25.720248039 +0000 UTC m=+1132.965304207 A100 1}"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"Processing variant - index: 0, variantAutoscaling-name: vllme-deployment, namespace: llm-d-sim, has_optimized_alloc: true"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"EmitReplicaMetrics completed for variantAutoscaling-name: vllme-deployment, current-replicas: 2, desired-replicas: 1, accelerator: A100"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.720Z","msg":"Successfully emitted optimization signals for external autoscalers - variant: vllme-deployment"}
{"level":"DEBUG","ts":"2025-08-26T13:13:25.723Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-26T13:13:25.723Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
```

This can be verified by checking the deployments and VA status too:
```sh 
kubectl get deployments -n llm-d-sim                
NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
gaie-sim-epp                  1/1     1            1           18m
infra-sim-inference-gateway   1/1     1            1           17m
vllme-deployment              1/1     1            1           18m

kubectl get variantautoscalings.llmd.ai -n llm-d-sim
NAME               MODEL             ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
vllme-deployment   default/default   A100          2                 1           16m
```

### Uninstalling llm-d and workload-variant-autoscaler 
Use this target to undeploy the integrated test environment and related resources:

```sh
make undeploy-llm-d-inferno-emulated-on-kind
```

## Running E2E tests
Use this target to run E2E tests:

```sh
make test-e2e
```

You can change Kubernetes configuration file (default: **$HOME/.kube/config**) and the minimum required version (default: **v1.32.0**) by setting the corresponding environment variables:
```sh
make test-e2e KUBECONFIG="path/to/your/config" K8S_VERSION="vX.y.z"
```

By default, all E2E tests are executed. You can focus on and/or skip certain tests by passing a pattern to the environment variables FOCUS and SKIP:
```sh
make test-e2e FOCUS="pattern" SKIP="pattern"

# - FOCUS="pattern": Run all tests matching the pattern
# - SKIP="pattern": Skip all tests matching the pattern

# Examples:
# - make test-e2e FOCUS="single VA"
# - make test-e2e FOCUS="multiple VA|Manager"
```

## Details on emulated mode deployment on Kind

- Emulated deployment, creates fake gpu resources on the node and deploys inferno on the cluster where inferno consumes fake gpu resources. As well as the emulated vllm server (vllme).

### Deployment

Use this target to spin up a complete local test environment:

```sh
make deploy-inferno-emulated-on-kind IMG=<some-registry>/inferno-controller:tag KIND_ARGS="-t mix -n 3 -g 4"

# -t mix - mix vendors
# -n - number of nodes
# -g - number of gpus per node 

# prebuilt image
# make deploy-inferno-emulated-on-kind 
```

```console
Summary: GPU resource capacities and allocatables for cluster 'kind-inferno-gpu-cluster':
-------------------------------------------------------------------------------------------------------------------------------
Node                                     Resource             Capacity   Allocatable GPU Product                    Memory (MB)
-------------------------------------------------------------------------------------------------------------------------------
kind-inferno-gpu-cluster-control-plane   nvidia.com/gpu       2          2          NVIDIA-A100-PCIE-40GB          40960     
kind-inferno-gpu-cluster-worker          amd.com/gpu          2          2          AMD-RX-7800-XT                 16384     
kind-inferno-gpu-cluster-worker2         intel.com/gpu        2          2          Intel-Arc-A770                 16384     
-------------------------------------------------------------------------------------------------------------------------------
```

**Check the inferno controller is up:**

```sh
kubectl get pods -n workload-variant-autoscaler-system
```


**Check the configmap is installed:**

```sh
kubectl get cm -n workload-variant-autoscaler-system
```


### Uninstall

**Delete the APIs(CRDs) from the cluster:**

```sh
make undeploy
```

**Delete cluster**

```sh
make destroy-kind-cluster
```

### Prometheus vllme setup

Local development will need emulated vllm server, prometheus installed in KinD cluster. 

#### Note: The above script already deploys emulated vllm server:

```sh
# Check if vllme is deployed and prometheus is setup
kubectl get pods -A | grep -E "(inferno|vllme|prometheus)"
```

If you dont have vllme deployed, run:

```sh
bash hack/deploy-emulated-vllme-server.sh
```

**Expose the prometheus server**

```sh
# Wait for all pods to be ready before port forwarding
sleep 30 && kubectl get pods -A | grep -E "(inferno|vllme|prometheus)"

# Port forward Prometheus
kubectl port-forward svc/prometheus-operated 9090:9090 -n workload-variant-autoscaler-monitoring
# server can be accessed at location: https://localhost:9090
```

**Note**: Always ensure pods are ready before attempting port forwarding to avoid connection errors.

**Check vllm emulated deployment**

```sh
kubectl get deployments -n llm-d-sim
NAME               READY   UP-TO-DATE   AVAILABLE   AGE
vllme-deployment   1/1     1            1           35s
```

**Expose the vllme server**
```sh
# Note: Ensure pods are ready before port forwarding (see Prometheus section above)
kubectl port-forward svc/vllme-service 8000:80 -n llm-d-sim
```

**Sanity check**

Go to http://localhost:8000/metrics and check if you see metrics starting with vllm:. Refresh to see the values changing with the load generator on.

**Run sample query**

```sh
curl -G https://localhost:9090/api/v1/query \
     --data-urlencode 'query=sum(rate(vllm:requests_count_total[1m])) * 60'

curl -G https://localhost:9090/api/v1/query \
     --data-urlencode 'query=sum(rate(vllm:requests_count_total[1m])) * 60'
{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1752075000.160,"9.333333333333332"]}]}}% 

```

**Generate API docs**

```sh
make crd-docs
```

### Inferno custom metrics

**Apply ServiceMonitor for custom metrics**

The Inferno Autoscaler exposes custom metrics that can be accessed through Prometheus.

```sh
kubectl apply -f config/prometheus/servicemonitor.yaml

# Verify ServiceMonitor is correctly configured
kubectl get servicemonitor workload-variant-autoscaler -n workload-variant-autoscaler-monitoring -o yaml | grep -A 10 namespaceSelector

# Note: ServiceMonitor discovery takes 1-2 minutes to complete
```

For detailed information about the custom metrics, see [Custom Metrics Documentation](docs/custom-metrics.md).

**Accessing the Grafana**

```sh
# username:admin
# password: prom-operator
kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80 -n workload-variant-autoscaler-monitoring
```

## Contributing

Please join llmd autoscaling community meetings and feel free to submit github issues and PRs. 

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

