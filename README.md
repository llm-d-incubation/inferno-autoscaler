<p style="font-size: 25px" align="center"><b>Inferno-Autoscaler</b></p>

The inferno-autoscaler assigns GPU types to inference model servers and decides on the number of replicas for each model for a given request traffic load and classes of service, as well as the batch size.

**Table of contents**

- [Description](#description)
- [Getting Started](#getting-started)
  - [Building & deploying inferno with llmd infrastructure](#quickstart-guide-installation-of-inferno-autoscaler-along-with-llm-d-infrastructure-emulated-deployment-on-a-kind-cluster)
  - [Building & deploying inferno in emulated mode](#details-on-emulated-mode-deployment-on-kind)
- [Contributing](#contributing)


## Description

The inferno-autoscaler is a Kubernetes controller that performs optimizated autoscaling using the below components:

![Diagram](docs/diagrams/inferno-WVA-design.png)

Reconciler:

The controller is implemented using the controller-runtime framework, which reconciles the namespace-scoped VariantAutoscaling objects created by the platform administrator, one per model.Due to runtime variability in model behavior (e.g., differences in prompt lengths, output sizes, or server-level contention), we treat model analysis as a continuously reconciled step during every autoscaler loop.

Collector(s):
The collectors that gather cluster data about the cluster state and the state of vllm servers running inside the controller.

Actuator:
The actuator is responsible for emitting metrics to the desired sources, like Prometheus, or changing replicas of existing deployments running on the cluster, which is the case with the Inferno autoscaler.

Model Analyzer:
Model Analyzer is a component that runs per model to perform scaling, estimation, prediction, and tuning.

Proposed sources:
These include the new [API proposal](https://docs.google.com/document/d/1j2KRAT68_FYxq1iVzG0xVL-DHQhGVUZBqiM22Hd_0hc/edit?usp=drivesdk&resourcekey=0-5cSovS8QcRQNYXj0_kRMiw), which is expected to work in conjunction with the inference scheduler (EPP) to provide insights into the request scheduler's dispatching logic.

For more details please refer to the community proposal [here](https://docs.google.com/document/d/1n6SAhloQaoSyF2k3EveIOerT-f97HuWXTLFm07xcvqk/edit?tab=t.0).

Modeling and optimization techniques used in the inferno-autoscaler are described in this [document](./docs/modeling-optimization.md).

## Getting Started

### Prerequisites
- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

## Quickstart Guide: Installation of Inferno-autoscaler along with llm-d infrastructure emulated deployment on a Kind cluster

Use this target to spin up a local test environment integrated with llm-d core components:

```sh
make deploy-llm-d-inferno-emulated-on-kind

# prebuilt image
# IMG=quay.io/infernoautoscaler/inferno-controller:latest
```

This target deploys an environment ready for testing, integrating the llm-d infrastructure and the Inferno-autoscaler.

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
# IMG=quay.io/infernoautoscaler/inferno-controller:0.0.1-multi-arch
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
```sh
kubectl port-forward -n llm-d-sim service/infra-sim-inference-gateway 8000:80
```

**Note**: Since the environment uses vllm-emulator, the **Criticality** parameter is set to `critical` for emulation purposes.

### Showing Inferno-autoscaler scaling replicas up and down
1. Target the deployed vLLM-emulator servers by deploying the VariantAutoscaling (Va) object:
```sh
kubectl apply -f hack/vllme/deploy/vllme-setup/vllme-variantautoscaling.yaml
``` 

2. Before starting the load generator, we can see that the Inferno-autoscaler is not scaling up existing deployments:

```sh
kubectl get deployments -n llm-d-sim
NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
gaie-sim-epp                  1/1     1            1           5m12s
infra-sim-inference-gateway   1/1     1            1           5m7s
vllme-deployment              1/1     1            1           5m58s

kubectl get variantautoscalings.llmd.ai -n llm-d-sim 
NAME               MODEL     ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
vllme-deployment   default   A100          1                 1           4m31s
```

3. Launch the `loadgen.py` load generator to send requests to the `v1/chat/completions` endpoint:
```sh
cd hack/vllme/vllm_emulator
pip install -r requirements.txt # if not already installed
python loadgen.py

# Default parameters
# Starting load generator with deterministic mode
# Server Address: http://localhost:8000/v1
# Request Rate = 60.0
# Model: gpt-1337-turbo-pro-max
# Content Length: 150

# for deterministic dynamically changing rate
python loadgen.py --model default  --rate '[[120, 60], [120, 80]]' --url http://localhost:8000/v1

# First 120 seconds (2 minutes): Send 60 requests per minute
# Next 120 seconds (2 minutes): Send 80 requests per minute
# Can use any combination of rates such as [[120, 60], [120, 80], [120, 40]]
```

- To request the port-forwarded gateway, as '*server base URL*' use **http://localhost:8000/v1** [**option 3**]
- As '*model name*', insert: "**vllm**"

4. **Scaling up**: after launching the load generator script `loadgen.py` with related RPM and context length (such as **RPM=40** and **context length** equal to **50**), we can see the logs from the Inferno-autoscaler controller effectively computing the optimal resource allocation and scaling up the deployments:

```sh
kubectl logs -n inferno-autoscaler-system deployments/inferno-autoscaler-controller-manager
#...
{"level":"DEBUG","ts":"2025-08-04T19:18:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-control-plane , model - NVIDIA-A100-PCIE-80GB , count - 2 , mem - 81920"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker , model - AMD-MI300X-192G , count - 2 , mem - 196608"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker2 , model - Intel-Gaudi-2-96GB , count - 2 , mem - 98304"}
{"level":"INFO","ts":"2025-08-04T19:18:16.481Z","msg":"Found SLOmodeldefaultclassPremiumslo-itl24slo-ttw500"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.483Z","msg":"System data prepared for optimization: systemData - &{{{[{G2 Intel-Gaudi-2-96GB 1 0 0 {0 0 0 0} 23} {MI300X AMD-MI300X-192GB 1 0 0 {0 0 0 0} 65} {A100 NVIDIA-A100-PCIE-80GB 1 0 0 {0 0 0 0} 40}]} {[{default A100 1 20.58 0.41 4 128} {default MI300X 1 7.77 0.15 4 128} {default G2 1 17.15 0.34 4 128}]} {[{Freemium granite-13b 10 200 2000 0} {Freemium llama0-7b 10 150 1500 0} {Premium default 1 24 500 0} {Premium llama0-70b 1 80 500 0}]} {[{vllme-deployment:llm-d-sim Premium default true 1 4 {A100 1 256 40 20 0 {22.67 178 0 0}} { 0 0 0 0 0 {0 0 0 0}}}]} {{false false}} {[{NVIDIA-A100-PCIE-80GB 2} {AMD-MI300X-192G 2} {Intel-Gaudi-2-96GB 2}]}}}"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.484Z","msg":"Optimization solutionsystemSolution: \nc=Premium; m=default; rate=22.67; tk=178; sol=1, alloc={acc=A100; num=1; maxBatch=4; cost=40, val=0, servTime=21.555601, waitTime=110.798096, rho=0.7630596}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-A100-PCIE-80GB, count=1, limit=2, cost=40 \ntotalCost=40 \n"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.484Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.484Z","msg":"Optimized allocation mapkeys1updateList_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.484Z","msg":"Optimized allocation entrykeyvllme-deploymentvalue{2025-08-04 19:18:16.484006425 +0000 UTC m=+1380.284253535 A100 1}"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.484Z","msg":"Optimization metrics emitted, starting to process variantsvariant_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.484Z","msg":"Processing variantindex0namevllme-deploymentnamespacellm-d-simhas_optimized_alloctrue"}
{"level":"INFO","ts":"2025-08-04T19:18:16.487Z","msg":"Patched Deployment: name: vllme-deployment num-replicas: 1"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.493Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-04T19:18:16.493Z","msg":"Emitted metrics for variantnamevllme-deploymentnamespacellm-d-sim"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.493Z","msg":"EmitMetrics call completed successfullynamevllme-deployment"}
{"level":"DEBUG","ts":"2025-08-04T19:18:16.493Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-04T19:18:16.493Z","msg":"Reconciliation completedvariants_processed1optimization_successfultrue"}
# New round:
{"level":"DEBUG","ts":"2025-08-04T19:19:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-control-plane , model - NVIDIA-A100-PCIE-80GB , count - 2 , mem - 81920"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker , model - AMD-MI300X-192G , count - 2 , mem - 196608"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker2 , model - Intel-Gaudi-2-96GB , count - 2 , mem - 98304"}
{"level":"INFO","ts":"2025-08-04T19:19:16.481Z","msg":"Found SLOmodeldefaultclassPremiumslo-itl24slo-ttw500"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.483Z","msg":"System data prepared for optimization: systemData - &{{{[{A100 NVIDIA-A100-PCIE-80GB 1 0 0 {0 0 0 0} 40} {G2 Intel-Gaudi-2-96GB 1 0 0 {0 0 0 0} 23} {MI300X AMD-MI300X-192GB 1 0 0 {0 0 0 0} 65}]} {[{default A100 1 20.58 0.41 4 128} {default MI300X 1 7.77 0.15 4 128} {default G2 1 17.15 0.34 4 128}]} {[{Freemium granite-13b 10 200 2000 0} {Freemium llama0-7b 10 150 1500 0} {Premium default 1 24 500 0} {Premium llama0-70b 1 80 500 0}]} {[{vllme-deployment:llm-d-sim Premium default true 1 4 {A100 1 256 40 20 0 {44 178 0 0}} { 0 0 0 0 0 {0 0 0 0}}}]} {{false false}} {[{AMD-MI300X-192G 2} {Intel-Gaudi-2-96GB 2} {NVIDIA-A100-PCIE-80GB 2}]}}}"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.483Z","msg":"Optimization solutionsystemSolution: \nc=Premium; m=default; rate=44; tk=178; sol=1, alloc={acc=A100; num=2; maxBatch=4; cost=80, val=40, servTime=21.540192, waitTime=99.1626, rho=0.7524011}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-A100-PCIE-80GB, count=2, limit=2, cost=80 \ntotalCost=80 \n"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.483Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.483Z","msg":"Optimized allocation mapkeys1updateList_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.483Z","msg":"Optimized allocation entrykeyvllme-deploymentvalue{2025-08-04 19:19:16.48370162 +0000 UTC m=+1440.283948730 A100 2}"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.483Z","msg":"Optimization metrics emitted, starting to process variantsvariant_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.483Z","msg":"Processing variantindex0namevllme-deploymentnamespacellm-d-simhas_optimized_alloctrue"}
{"level":"INFO","ts":"2025-08-04T19:19:16.488Z","msg":"Patched Deployment: name: vllme-deployment num-replicas: 2"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.494Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-04T19:19:16.494Z","msg":"Emitted metrics for variantnamevllme-deploymentnamespacellm-d-sim"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.494Z","msg":"EmitMetrics call completed successfullynamevllme-deployment"}
{"level":"DEBUG","ts":"2025-08-04T19:19:16.494Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-04T19:19:16.494Z","msg":"Reconciliation completedvariants_processed1optimization_successfultrue"}
```

Checking the deployments and the currently applied VAs: 

```sh
kubectl get deployments -n llm-d-sim 
NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
gaie-sim-epp                  1/1     1            1           7m8s
infra-sim-inference-gateway   1/1     1            1           7m3s
vllme-deployment              2/2     2            2           7m58s

kubectl get variantautoscalings.llmd.ai -n llm-d-sim 
NAME               MODEL     ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
vllme-deployment   default   A100          2                 2           6m31s
```

5. **Scaling down**: by stopping the load generator script with a keyboard interrupt, we can see that the Inferno-autoscaler effectively scales down the replicas:
```sh
kubectl logs -n inferno-autoscaler-system deployments/inferno-autoscaler-controller-manager
# ...
{"level":"DEBUG","ts":"2025-08-04T19:20:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-control-plane , model - NVIDIA-A100-PCIE-80GB , count - 2 , mem - 81920"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker , model - AMD-MI300X-192G , count - 2 , mem - 196608"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.481Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker2 , model - Intel-Gaudi-2-96GB , count - 2 , mem - 98304"}
{"level":"INFO","ts":"2025-08-04T19:20:16.481Z","msg":"Found SLOmodeldefaultclassPremiumslo-itl24slo-ttw500"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.484Z","msg":"System data prepared for optimization: systemData - &{{{[{A100 NVIDIA-A100-PCIE-80GB 1 0 0 {0 0 0 0} 40} {G2 Intel-Gaudi-2-96GB 1 0 0 {0 0 0 0} 23} {MI300X AMD-MI300X-192GB 1 0 0 {0 0 0 0} 65}]} {[{default A100 1 20.58 0.41 4 128} {default MI300X 1 7.77 0.15 4 128} {default G2 1 17.15 0.34 4 128}]} {[{Freemium granite-13b 10 200 2000 0} {Freemium llama0-7b 10 150 1500 0} {Premium default 1 24 500 0} {Premium llama0-70b 1 80 500 0}]} {[{vllme-deployment:llm-d-sim Premium default true 1 4 {A100 2 256 80 20 0 {37.76 178 0 0}} { 0 0 0 0 0 {0 0 0 0}}}]} {{false false}} {[{NVIDIA-A100-PCIE-80GB 2} {AMD-MI300X-192G 2} {Intel-Gaudi-2-96GB 2}]}}}"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.484Z","msg":"Optimization solutionsystemSolution: \nc=Premium; m=default; rate=37.76; tk=178; sol=1, alloc={acc=A100; num=2; maxBatch=4; cost=80, val=0, servTime=21.466858, waitTime=56.422607, rho=0.6966711}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-A100-PCIE-80GB, count=2, limit=2, cost=80 \ntotalCost=80 \n"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.484Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.484Z","msg":"Optimized allocation mapkeys1updateList_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.484Z","msg":"Optimized allocation entrykeyvllme-deploymentvalue{2025-08-04 19:20:16.484244316 +0000 UTC m=+1500.284491425 A100 2}"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.484Z","msg":"Optimization metrics emitted, starting to process variantsvariant_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.484Z","msg":"Processing variantindex0namevllme-deploymentnamespacellm-d-simhas_optimized_alloctrue"}
{"level":"INFO","ts":"2025-08-04T19:20:16.487Z","msg":"Patched Deployment: name: vllme-deployment num-replicas: 2"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.492Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-04T19:20:16.492Z","msg":"Emitted metrics for variantnamevllme-deploymentnamespacellm-d-sim"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.492Z","msg":"EmitMetrics call completed successfullynamevllme-deployment"}
{"level":"DEBUG","ts":"2025-08-04T19:20:16.492Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-04T19:20:16.492Z","msg":"Reconciliation completedvariants_processed1optimization_successfultrue"}
# New round:
{"level":"DEBUG","ts":"2025-08-04T19:21:16.482Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker , model - AMD-MI300X-192G , count - 2 , mem - 196608"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.482Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-worker2 , model - Intel-Gaudi-2-96GB , count - 2 , mem - 98304"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.482Z","msg":"Found inventory: nodeName - kind-inferno-gpu-cluster-control-plane , model - NVIDIA-A100-PCIE-80GB , count - 2 , mem - 81920"}
{"level":"INFO","ts":"2025-08-04T19:21:16.482Z","msg":"Found SLOmodeldefaultclassPremiumslo-itl24slo-ttw500"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.484Z","msg":"System data prepared for optimization: systemData - &{{{[{A100 NVIDIA-A100-PCIE-80GB 1 0 0 {0 0 0 0} 40} {G2 Intel-Gaudi-2-96GB 1 0 0 {0 0 0 0} 23} {MI300X AMD-MI300X-192GB 1 0 0 {0 0 0 0} 65}]} {[{default A100 1 20.58 0.41 4 128} {default MI300X 1 7.77 0.15 4 128} {default G2 1 17.15 0.34 4 128}]} {[{Premium default 1 24 500 0} {Premium llama0-70b 1 80 500 0} {Freemium granite-13b 10 200 2000 0} {Freemium llama0-7b 10 150 1500 0}]} {[{vllme-deployment:llm-d-sim Premium default true 1 4 {A100 2 256 80 20 0 {2.67 178 0 0}} { 0 0 0 0 0 {0 0 0 0}}}]} {{false false}} {[{AMD-MI300X-192G 2} {Intel-Gaudi-2-96GB 2} {NVIDIA-A100-PCIE-80GB 2}]}}}"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.484Z","msg":"Optimization solutionsystemSolution: \nc=Premium; m=default; rate=2.67; tk=178; sol=1, alloc={acc=A100; num=1; maxBatch=4; cost=40, val=-40, servTime=21.058374, waitTime=0.032958984, rho=0.15340477}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-A100-PCIE-80GB, count=1, limit=2, cost=40 \ntotalCost=40 \n"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.484Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.484Z","msg":"Optimized allocation mapkeys1updateList_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.484Z","msg":"Optimized allocation entrykeyvllme-deploymentvalue{2025-08-04 19:21:16.484650177 +0000 UTC m=+1560.284897245 A100 1}"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.484Z","msg":"Optimization metrics emitted, starting to process variantsvariant_count1"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.484Z","msg":"Processing variantindex0namevllme-deploymentnamespacellm-d-simhas_optimized_alloctrue"}
{"level":"INFO","ts":"2025-08-04T19:21:16.490Z","msg":"Patched Deployment: name: vllme-deployment num-replicas: 1"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.494Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-04T19:21:16.494Z","msg":"Emitted metrics for variantnamevllme-deploymentnamespacellm-d-sim"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.494Z","msg":"EmitMetrics call completed successfullynamevllme-deployment"}
{"level":"DEBUG","ts":"2025-08-04T19:21:16.494Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-04T19:21:16.494Z","msg":"Reconciliation completedvariants_processed1optimization_successfultrue"}
```

This can be verified by checking the deployments and VA status too:
```sh 
kubectl get deployments -n llm-d-sim                
NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
gaie-sim-epp                  1/1     1            1           18m
infra-sim-inference-gateway   1/1     1            1           17m
vllme-deployment              1/1     1            1           18m

kubectl get variantautoscalings.llmd.ai -n llm-d-sim
NAME               MODEL     ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
vllme-deployment   default   A100          1                 1           16m
```

### Uninstalling llm-d and Inferno-autoscaler 
Use this target to undeploy the integrated test environment and related resources:

```sh
make undeploy-llm-d-inferno-emulated-on-kind
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
kubectl get pods -n inferno-autoscaler-system
```


**Check the configmap is installed:**

```sh
kubectl get cm -n inferno-autoscaler-system
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
kubectl port-forward svc/prometheus-operated 9090:9090 -n inferno-autoscaler-monitoring
# server can be accessed at location: http://localhost:9090
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
curl -G http://localhost:9090/api/v1/query \
     --data-urlencode 'query=sum(rate(vllm:requests_count_total[1m])) * 60'

curl -G http://localhost:9090/api/v1/query \
     --data-urlencode 'query=sum(rate(vllm:requests_count_total[1m])) * 60'
{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1752075000.160,"9.333333333333332"]}]}}% 

```

### Inferno custom metrics

**Apply ServiceMonitor for custom metrics**

The Inferno Autoscaler exposes custom metrics that can be accessed through Prometheus.

```sh
kubectl apply -f config/prometheus/servicemonitor.yaml

# Verify ServiceMonitor is correctly configured
kubectl get servicemonitor inferno-autoscaler -n inferno-autoscaler-monitoring -o yaml | grep -A 10 namespaceSelector

# Note: ServiceMonitor discovery takes 1-2 minutes to complete
```

For detailed information about the custom metrics, see [Custom Metrics Documentation](docs/custom-metrics.md).

**Accessing the Grafana**

```sh
# username:admin
# password: prom-operator
kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80 -n inferno-autoscaler-monitoring
```

## Contributing

Please join llmd autoscaling community meetings and feel free to submit github issues and PRs. 

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

