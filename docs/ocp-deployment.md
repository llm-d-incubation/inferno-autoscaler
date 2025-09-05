# Deploying the Inferno-Autoscaler on OpenShift

This guide shows how to deploy the Inferno-Autoscaler (integrated with the HorizontalPodAutoscaler) on OpenShift (OCP), allowing vLLM servers to scale accordingly to the observed load.

**Note**: the deployment was tested on a cluster with *admin* privileges.

## Overview

After deploying the Inferno-autoscaler following the provided guides, this guide allows the integration of the following components:

1. **Inferno Controller** processes VariantAutoscaling objects and emits the `inferno_desired_replicas` metrics

2. **Prometheus and Thanos** scrape these metrics from the Inferno-autoscaler `/metrics` endpoint using TLS, using the existing monitoring infrastructure present on OCP

3. **Prometheus Adapter** exposes the metrics to Kubernetes external metrics API

4. **HPA** reads the value for the `inferno_desired_replicas` metrics and adjusts Deployment replicas accordingly

## Prerequisites

- Prometheus stack already present on the OCP cluster
- All components must be fully ready before proceeding: 2-3 minutes may be needed after the deployment

### 0. Deploy the Inferno-Autoscaler and the vLLM Deployment

#### Deploying the Inferno-Autoscaler

Before running the Make target to deploy Inferno-Autoscaler, the `PROMETHEUS_BASE_URL` in the `config/manager/configmap.yaml` must be changed into the following, to be able to connect to Thanos:

```yaml
# ...
  PROMETHEUS_BASE_URL: "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091"
```

After that, you can deploy the Inferno-Autoscaler using the basic Make target:

```sh
make deploy IMG=quay.io/infernoautoscaler/inferno-controller:0.0.2-multi-arch
```

Then, you need to deploy the required ConfigMaps for the accelerator costs and the service classes. An example of this configuration can be found [at the end of this README](#accelerator-costs-and-serviceclasses-configsamplesacc-servclass-configmapyaml).

```sh
## After creating the file `config/samples/acc-servclass-configmap.yaml`
kubectl apply -f config/samples/acc-servclass-configmap.yaml
```

And then, create the required ServiceMonitor for the Inferno-Autoscaler, to be deployed in the `openshift-user-workload-monitoring` namespace.
An example of this configuration can be found [at the end of this README](#servicemonitor-for-the-inferno-autoscaler-configsamplesinferno-servicemon-ocpyaml).

```sh
## After creating the file `config/samples/inferno-servicemon-ocp.yaml`
kubectl apply -f config/samples/inferno-servicemon-ocp.yaml
```

#### Deploying the vLLM Deployment

First, create a secret containing your HuggingFace token:

```sh
export HF_TOKEN="<your-hf-token>"
kubectl create secret -n llm-d-sim generic llm-d-hf-token --from-literal=token="$HF_TOKEN"
```

Then, create a Deployment for vLLM. An example of this configuration can be found [at the end of this README](#vllm-deployment-example-configsamplesvllm-deployment-service-servicemonyaml).

```sh
## After creating the file `config/samples/vllm-deployment-service-servicemon.yaml`
kubectl apply -f config/samples/vllm-deployment-service-servicemon.yaml
```

### 1. Create Thanos CA ConfigMap

Prometheus and Thanos are deployed on OCPs with TLS (HTTPS) for security. The Prometheus Adapter needs to connect to Thanos at https://thanos-querier.openshift-monitoring.svc.cluster.local.

We will use a CA configmap for TLS Certificate Verification:

```sh
# Extract the TLS certificate from the thanos-querier-tls secret
kubectl get secret thanos-queries-tls -n openshift-monitoring -o jsonpath='{.data.tls\.crt}' | base64 -d > /tmp/prometheus-ca.crt

# Create ConfigMap with the certificate
kubectl create configmap prometheus-ca --from-file=ca.crt=/tmp/prometheus-ca.crt -n openshift-user-workload-monitoring
```

### 2. Deploy the Prometheus Adapter

Note: a `yaml` example snippet for the Prometheus Adapter configuration with TLS for OCP can be found [at the end of this README](#prometheus-adapter-values-configsamplesprometheus-adapter-valuesyaml).

```sh
# Add Prometheus community helm repo - already there if you deployed Inferno-autoscaler using the scripts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Deploy Prometheus Adapter with Inferno-autoscaler metrics configuration
helm install prometheus-adapter prometheus-community/prometheus-adapter \
  -n openshift-user-workload-monitoring \
  -f config/samples/prometheus-adapter-values.yaml
```

### 3. Create the VariantAutoscaling resource

An example of VariantAutoscaling resource can be found [at the end of this README](#variantautoscaling-configuration-example-configsamplesvllm-vayaml).

```sh
## After creating the file `config/samples/vllm-va.yaml`
kubectl apply -f config/samples/vllm-va.yaml
```

### 4. Wait for Prometheus to fetch metrics from the Inferno-Autoscaler

You can verify that metrics are being emitted and fetched by querying for the following:

```bash
kubectl get --raw "/apis/external.metrics.k8s.io/v1beta1/namespaces/llm-d-sim/inferno_desired_replicas" | jq

{
  "kind": "ExternalMetricValueList",
  "apiVersion": "external.metrics.k8s.io/v1beta1",
  "metadata": {},
  "items": [
    {
      "metricName": "inferno_desired_replicas",
      "metricLabels": {
        "__name__": "inferno_desired_replicas",
        "accelerator_type": "H100",
        "endpoint": "https",
        "exported_namespace": "llm-d-sim",
        "instance": "10.130.3.58:8443",
        "job": "inferno-autoscaler-controller-manager-metrics-service",
        "managed_cluster": "dc670625-c0d1-48d6-bcc3-b932aaceecb4",
        "namespace": "inferno-autoscaler-system",
        "pod": "inferno-autoscaler-controller-manager-685966979-8lnnr",
        "prometheus": "openshift-monitoring/k8s",
        "service": "inferno-autoscaler-controller-manager-metrics-service",
        "variant_name": "ms-inference-scheduling-llm-d-modelservice-decode"
      },
      "timestamp": "2025-09-04T18:30:31Z",
      "value": "1"
    }
  ]
}
```

### 5. Deploy the HPA resource

Note: a `yaml` example snippet for HPA can be found [at the bottom of this README](#hpa-configuration-example-configsampleshpa-integrationyaml).

```sh
# After creating the file `config/samples/hpa-integration.yaml`
# Deploy HPA for your deployments
kubectl apply -f config/samples/hpa-integration.yaml
```

### 6. Verify the integration

- Wait for all components to be ready (1-2 minutes total)

- Check the status of HPA (should show actual target values, not `<unknown>/1`):

```sh
kubectl get hpa -n llm-d-sim
NAME                  REFERENCE                                                      TARGETS     MINPODS   MAXPODS   REPLICAS   AGE
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)   1         10        1          147m
```

- Check the VariantAutoscaling resource:

```sh
kubectl get variantautoscaling -n llm-d-sim
NAME                                                MODEL             ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 1           3h41m
```

## Example: scale-up scenario

1. Port-forward the Service:

```bash

# If you deployed Inferno-autoscaler without llm-d:
kubectl port-forward -n llm-d-sim svc/vllm-service 8000:8200
```

2. Launch the load generator via the following command (*Note*: this will generate traffic for 3 minutes):

```bash
cd hack/vllme/vllm_emulator
pip install -r requirements.txt
python loadgen.py --model Qwen/Qwen3-0.6B --rate '[[180, 50]]' --url http://localhost:8000/v1 --content 150
```

3. After a while, you will see a scale out happening:

```sh
kubectl get hpa -n llm-d-sim -w                                                             
NAME                  REFERENCE                                                      TARGETS     MINPODS   MAXPODS   REPLICAS   AGE
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)   1         10        1          150m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   3/1 (avg)   1         10        1          151m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)   1         10        3          151m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1334m/1 (avg)   1         10        3          152m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)       1         10        4          152m

kubectl get va -n llm-d-sim -w
NAME                                                MODEL             ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 1           3h44m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 3           3h45m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 3           3h45m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          3                 4           3h46m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          3                 4           3h46m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          4                 4           3h47m

kubectl get deployment -n llm-d-sim -w
NAME                                                READY   UP-TO-DATE   AVAILABLE   AGE
ms-inference-scheduling-llm-d-modelservice-decode   1/1     1            1           3h56m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     1            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     1            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     1            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     3            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     3            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     3            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     3            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     4            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   2/4     4            2           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   3/4     4            3           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   4/4     4            4           3h58m
```

It can be verified that the Inferno-autoscaler is optimizing and emitting metrics:

```bash
kubectl logs -n inferno-autoscaler-system deploy/inferno-autoscaler-controller-manager

###
2025-09-04T18:37:23.826150182Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.826Z","msg":"Found inventory: nodeName - pokprod-b93r38s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:23.826150182Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.826Z","msg":"Found inventory: nodeName - pokprod-b93r38s2 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:23.826150182Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.826Z","msg":"Found inventory: nodeName - pokprod-b93r39s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:23.826150182Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.826Z","msg":"Found inventory: nodeName - pokprod-b93r38s0 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:23.826336765Z {"level":"INFO","ts":"2025-09-04T18:37:23.826Z","msg":"Found SLO for model - model: Qwen/Qwen3-0.6B, class: Premium, slo-tpot: 24, slo-ttft: 500"}
2025-09-04T18:37:23.835891964Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.835Z","msg":"System data prepared for optimization: - { count: [  {   type: NVIDIA-H100-80GB-HBM3,   count: 32  } ]}"}
2025-09-04T18:37:23.835935875Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.835Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  },  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  },  {   name: H100,   type: NVIDIA-H100-80GB-HBM3,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 50  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  } ]}"}
2025-09-04T18:37:23.835967164Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.835Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Freemium,   priority: 10,   modelTargets: [    {     model: granite-13b,     slo-itl: 200,     slo-ttw: 2000,     slo-tps: 0    },    {     model: llama0-7b,     slo-itl: 150,     slo-ttw: 1500,     slo-tps: 0    }   ]  },  {   name: Premium,   priority: 1,   modelTargets: [    {     model: Qwen/Qwen3-0.6B,     slo-itl: 24,     slo-ttw: 500,     slo-tps: 0    },    {     model: llama0-70b,     slo-itl: 80,     slo-ttw: 500,     slo-tps: 0    }   ]  } ]}"}
2025-09-04T18:37:23.836000274Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.835Z","msg":"System data prepared for optimization: - { models: [  {   name: Qwen/Qwen3-0.6B,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 0  },  {   name: Qwen/Qwen3-0.6B,   acc: H100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 0  },  {   name: Qwen/Qwen3-0.6B,   acc: MI300X,   accCount: 1,   alpha: 7.77,   beta: 0.15,   maxBatchSize: 4,   atTokens: 0  },  {   name: Qwen/Qwen3-0.6B,   acc: G2,   accCount: 1,   alpha: 17.15,   beta: 0.34,   maxBatchSize: 4,   atTokens: 0  } ]}"}
2025-09-04T18:37:23.836014318Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: true,  delayedBestEffort: false,  saturationPolicy: None }}"}
2025-09-04T18:37:23.836051359Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"System data prepared for optimization: - { servers: [  {   name: ms-inference-scheduling-llm-d-modelservice-decode:llm-d-sim,   class: Premium,   model: Qwen/Qwen3-0.6B,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 4,   currentAlloc: {    accelerator: H100,    numReplicas: 1,    maxBatch: 256,    cost: 50,    itlAverage: 7.82,    waitAverage: 0.03,    load: {     arrivalRate: 37.5,     avgLength: 279,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
2025-09-04T18:37:23.836078086Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"Optimization solution - system: Solution: \ns=ms-inference-scheduling-llm-d-modelservice-decode:llm-d-sim; c=Premium; m=Qwen/Qwen3-0.6B; rate=37.5; tk=279; sol=1, sat=false, alloc={acc=H100; num=3; maxBatch=4; cost=150, val=100, servTime=21.48383, waitTime=101.370605, rho=0.7103443, maxRPM=14.308867}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-H100-80GB-HBM3, count=3, limit=32, cost=150 \ntotalCost=150 \n"}
2025-09-04T18:37:23.836081984Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"Optimization completed successfully, emitting optimization metrics"}
2025-09-04T18:37:23.836081984Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
2025-09-04T18:37:23.836093387Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"Optimized allocation entry - key: ms-inference-scheduling-llm-d-modelservice-decode, value: {2025-09-04 18:37:23.836074464 +0000 UTC m=+8140.676611163 H100 3}"}
2025-09-04T18:37:23.836093387Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
2025-09-04T18:37:23.836097655Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"Processing variant - index: 0, variantAutoscaling-name: ms-inference-scheduling-llm-d-modelservice-decode, namespace: llm-d-sim, has_optimized_alloc: true"}
2025-09-04T18:37:23.836156850Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"EmitReplicaMetrics completed for variantAutoscaling-name: ms-inference-scheduling-llm-d-modelservice-decode, current-replicas: 1, desired-replicas: 3, accelerator: H100"}
2025-09-04T18:37:23.836156850Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.836Z","msg":"Successfully emitted optimization signals for external autoscalers - variant: ms-inference-scheduling-llm-d-modelservice-decode"}
2025-09-04T18:37:23.841748006Z {"level":"DEBUG","ts":"2025-09-04T18:37:23.841Z","msg":"Completed variant processing loop"}
2025-09-04T18:37:23.841748006Z {"level":"INFO","ts":"2025-09-04T18:37:23.841Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
2025-09-04T18:37:24.059226028Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.059Z","msg":"Found inventory: nodeName - pokprod-b93r38s0 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:24.059226028Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.059Z","msg":"Found inventory: nodeName - pokprod-b93r38s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:24.059226028Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.059Z","msg":"Found inventory: nodeName - pokprod-b93r38s2 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:24.059226028Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.059Z","msg":"Found inventory: nodeName - pokprod-b93r39s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
2025-09-04T18:37:24.059258682Z {"level":"INFO","ts":"2025-09-04T18:37:24.059Z","msg":"Found SLO for model - model: Qwen/Qwen3-0.6B, class: Premium, slo-tpot: 24, slo-ttft: 500"}
2025-09-04T18:37:24.068919675Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"System data prepared for optimization: - { count: [  {   type: NVIDIA-H100-80GB-HBM3,   count: 32  } ]}"}
2025-09-04T18:37:24.068919675Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  },  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  },  {   name: H100,   type: NVIDIA-H100-80GB-HBM3,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 50  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  } ]}"}
2025-09-04T18:37:24.068919675Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Premium,   priority: 1,   modelTargets: [    {     model: Qwen/Qwen3-0.6B,     slo-itl: 24,     slo-ttw: 500,     slo-tps: 0    },    {     model: llama0-70b,     slo-itl: 80,     slo-ttw: 500,     slo-tps: 0    }   ]  },  {   name: Freemium,   priority: 10,   modelTargets: [    {     model: granite-13b,     slo-itl: 200,     slo-ttw: 2000,     slo-tps: 0    },    {     model: llama0-7b,     slo-itl: 150,     slo-ttw: 1500,     slo-tps: 0    }   ]  } ]}"}
2025-09-04T18:37:24.068919675Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"System data prepared for optimization: - { models: [  {   name: Qwen/Qwen3-0.6B,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 0  },  {   name: Qwen/Qwen3-0.6B,   acc: H100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 0  },  {   name: Qwen/Qwen3-0.6B,   acc: MI300X,   accCount: 1,   alpha: 7.77,   beta: 0.15,   maxBatchSize: 4,   atTokens: 0  },  {   name: Qwen/Qwen3-0.6B,   acc: G2,   accCount: 1,   alpha: 17.15,   beta: 0.34,   maxBatchSize: 4,   atTokens: 0  } ]}"}
2025-09-04T18:37:24.068950491Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: true,  delayedBestEffort: false,  saturationPolicy: None }}"}
2025-09-04T18:37:24.068967341Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"System data prepared for optimization: - { servers: [  {   name: ms-inference-scheduling-llm-d-modelservice-decode:llm-d-sim,   class: Premium,   model: Qwen/Qwen3-0.6B,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 4,   currentAlloc: {    accelerator: H100,    numReplicas: 1,    maxBatch: 256,    cost: 50,    itlAverage: 7.82,    waitAverage: 0.03,    load: {     arrivalRate: 37.5,     avgLength: 279,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
2025-09-04T18:37:24.068998313Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"Optimization solution - system: Solution: \ns=ms-inference-scheduling-llm-d-modelservice-decode:llm-d-sim; c=Premium; m=Qwen/Qwen3-0.6B; rate=37.5; tk=279; sol=1, sat=false, alloc={acc=H100; num=3; maxBatch=4; cost=150, val=100, servTime=21.48383, waitTime=101.370605, rho=0.7103443, maxRPM=14.308867}; slo-itl=24, slo-ttw=500, slo-tps=0 \nAllocationByType: \nname=NVIDIA-H100-80GB-HBM3, count=3, limit=32, cost=150 \ntotalCost=150 \n"}
2025-09-04T18:37:24.068998313Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"Optimization completed successfully, emitting optimization metrics"}
2025-09-04T18:37:24.069008299Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.068Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
2025-09-04T18:37:24.069012141Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.069Z","msg":"Optimized allocation entry - key: ms-inference-scheduling-llm-d-modelservice-decode, value: {2025-09-04 18:37:24.068994567 +0000 UTC m=+8140.909531265 H100 3}"}
2025-09-04T18:37:24.069012141Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.069Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
2025-09-04T18:37:24.069015490Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.069Z","msg":"Processing variant - index: 0, variantAutoscaling-name: ms-inference-scheduling-llm-d-modelservice-decode, namespace: llm-d-sim, has_optimized_alloc: true"}
2025-09-04T18:37:24.069093141Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.069Z","msg":"EmitReplicaMetrics completed for variantAutoscaling-name: ms-inference-scheduling-llm-d-modelservice-decode, current-replicas: 1, desired-replicas: 3, accelerator: H100"}
2025-09-04T18:37:24.069093141Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.069Z","msg":"Successfully emitted optimization signals for external autoscalers - variant: ms-inference-scheduling-llm-d-modelservice-decode"}
2025-09-04T18:37:24.074410146Z {"level":"DEBUG","ts":"2025-09-04T18:37:24.074Z","msg":"Completed variant processing loop"}
2025-09-04T18:37:24.074410146Z {"level":"INFO","ts":"2025-09-04T18:37:24.074Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
```

4. Once the load is stopped, the vLLM deployment will be scaled in to 1 replica:

```bash
kubectl get hpa -n llm-d-sim -w                                                             
NAME                  REFERENCE                                                      TARGETS     MINPODS   MAXPODS   REPLICAS   AGE
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)   1         10        1          150m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   3/1 (avg)   1         10        1          151m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)   1         10        3          151m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1334m/1 (avg)   1         10        3          152m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)       1         10        4          152m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   500m/1 (avg)    1         10        4          154m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)       1         10        2          154m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   500m/1 (avg)    1         10        2          155m
vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)       1         10        1          155m

kubectl get deployment -n llm-d-sim -w
NAME                                                READY   UP-TO-DATE   AVAILABLE   AGE
ms-inference-scheduling-llm-d-modelservice-decode   1/1     1            1           3h56m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     1            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     1            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     1            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/3     3            1           3h57m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     3            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     3            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     3            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   1/4     4            1           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   2/4     4            2           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   3/4     4            3           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   4/4     4            4           3h58m
ms-inference-scheduling-llm-d-modelservice-decode   4/2     4            4           4h
ms-inference-scheduling-llm-d-modelservice-decode   4/2     4            4           4h
ms-inference-scheduling-llm-d-modelservice-decode   2/2     2            2           4h
ms-inference-scheduling-llm-d-modelservice-decode   2/1     2            2           4h1m
ms-inference-scheduling-llm-d-modelservice-decode   2/1     2            2           4h1m
ms-inference-scheduling-llm-d-modelservice-decode   1/1     1            1           4h1m

kubectl get va -n llm-d-sim -w
NAME                                                MODEL             ACCELERATOR   CURRENTREPLICAS   OPTIMIZED   AGE
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 1           3h44m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 3           3h45m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 3           3h45m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          3                 4           3h46m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          3                 4           3h46m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          4                 4           3h47m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          4                 4           3h47m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          4                 2           3h48m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          4                 2           3h48m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          2                 1           3h49m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          2                 1           3h49m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 1           3h50m
ms-inference-scheduling-llm-d-modelservice-decode   Qwen/Qwen3-0.6B   H100          1                 1           3h50m
```

## Configuration Files

### Accelerator costs and ServiceClasses (`config/samples/acc-servclass-configmap.yaml`)

```yaml
apiVersion: v1
kind: ConfigMap
# This configMap defines the set of accelerators available
# to the autoscaler to assign to variants
#
# For each accelerator, need to specify a (unique) name and some properties:
# - device is the name of the device (card) corresponding to this accelerator,
#   it should be the same as the device specified in the node object
# - cost is the cents/hour cost of this accelerator
#
metadata:
  name: accelerator-unit-costs
  namespace: inferno-autoscaler-system
data:
  A100: |
    {
    "device": "NVIDIA-A100-PCIE-80GB",
    "cost": "40.00"
    }
  H100: | ##
    {
    "device": "NVIDIA-H100-80GB-HBM3",
    "cost": "50.00"
    }
  MI300X: |
    {
    "device": "AMD-MI300X-192GB",
    "cost": "65.00"
    }
  G2: |
    {
    "device": "Intel-Gaudi-2-96GB",
    "cost": "23.00"
    }
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: service-classes-config
  namespace: inferno-autoscaler-system
data:
  premium.yaml: |
    name: Premium
    priority: 1
    data:
      - model: Qwen/Qwen3-0.6B
        slo-tpot: 24
        slo-ttft: 500
      - model: llama0-70b
        slo-tpot: 80
        slo-ttft: 500
  freemium.yaml: |
    name: Freemium
    priority: 10
    data:
      - model: granite-13b
        slo-tpot: 200
        slo-ttft: 2000
      - model: llama0-7b
        slo-tpot: 150
        slo-ttft: 1500
```

### ServiceMonitor for the Inferno-Autoscaler (`config/samples/inferno-servicemon-ocp.yaml`)

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/name: inferno-autoscaler
    control-plane: controller-manager
  name: inferno-autoscaler-controller-manager-metrics-monitor
  namespace: openshift-user-workload-monitoring
spec:
  endpoints:
  - bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
    interval: 10s
    path: /metrics
    port: https
    scheme: https
    tlsConfig:
      insecureSkipVerify: true
  namespaceSelector:
    matchNames:
    - inferno-autoscaler-system
  selector:
    matchLabels:
      app.kubernetes.io/name: inferno-autoscaler
      control-plane: controller-manager
```

### vLLM Deployment Example (`config/samples/vllm-deployment-service-servicemon.yaml`)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    meta.helm.sh/release-name: ms-inference-scheduling
    meta.helm.sh/release-namespace: llm-d-sim
  name: ms-inference-scheduling-llm-d-modelservice-decode
  namespace: llm-d-sim
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vllm
      llm-d.ai/inferenceServing: "true"
      llm-d.ai/model: ms-inference-scheduling-llm-d-modelservice
      llm-d.ai/role: decode
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
    type: RollingUpdate
  template:
    metadata:
      labels:
        app: vllm
        llm-d.ai/inferenceServing: "true"
        llm-d.ai/model: ms-inference-scheduling-llm-d-modelservice
        llm-d.ai/role: decode
    spec:
      containers:
      - args:
        - Qwen/Qwen3-0.6B
        - --port
        - "8200"
        - --served-model-name
        - Qwen/Qwen3-0.6B
        - --enforce-eager
        - --kv-transfer-config
        - '{"kv_connector":"NixlConnector", "kv_role":"kv_both"}'
        command:
        - vllm
        - serve
        env:
        - name: CUDA_VISIBLE_DEVICES
          value: "0"
        - name: UCX_TLS
          value: cuda_ipc,cuda_copy,tcp
        - name: VLLM_NIXL_SIDE_CHANNEL_HOST
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: status.podIP
        - name: VLLM_NIXL_SIDE_CHANNEL_PORT
          value: "5557"
        - name: VLLM_LOGGING_LEVEL
          value: DEBUG
        - name: DP_SIZE
          value: "1"
        - name: TP_SIZE
          value: "1"
        - name: HF_HOME
          value: /model-cache
        - name: HF_TOKEN
          valueFrom:
            secretKeyRef:
              key: HF_TOKEN
              name: llm-d-hf-token
        image: ghcr.io/llm-d/llm-d:v0.2.0
        imagePullPolicy: IfNotPresent
        name: vllm
        resources:
          limits:
            nvidia.com/gpu: "1"
          requests:
            nvidia.com/gpu: "1"
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /.config
          name: metrics-volume
        - mountPath: /.cache
          name: torch-compile-cache
        - mountPath: /model-cache
          name: model-storage
      dnsPolicy: ClusterFirst
      initContainers:
      - args:
        - --port=8000
        - --vllm-port=8200
        - --connector=nixlv2
        - -v=5
        - --secure-proxy=false
        image: ghcr.io/llm-d/llm-d-routing-sidecar:v0.2.0
        imagePullPolicy: Always
        name: routing-proxy
        ports:
        - containerPort: 8000
          protocol: TCP
        resources: {}
        restartPolicy: Always
        securityContext:
          allowPrivilegeEscalation: false
          runAsNonRoot: true
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      # serviceAccount: ms-inference-scheduling-llm-d-modelservice
      # serviceAccountName: ms-inference-scheduling-llm-d-modelservice
      terminationGracePeriodSeconds: 30
      volumes:
      - emptyDir: {}
        name: metrics-volume
      - emptyDir: {}
        name: torch-compile-cache
      - emptyDir:
          sizeLimit: 20Gi
        name: model-storage
---
apiVersion: v1
kind: Service
metadata:
  name: vllm-service
  namespace: llm-d-sim
  labels:
    app: vllm
spec:
  selector:
    app: vllm
  ports:
    - name: vllm
      port: 8200
      protocol: TCP
      targetPort: 8200
      nodePort: 30000
  type: NodePort
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: vllm-servicemonitor
  namespace: openshift-user-workload-monitoring
  labels:
    app: vllm
spec:
  selector:
    matchLabels:
      app: vllm
  endpoints:
  - port: vllm
    path: /metrics
    interval: 15s
  namespaceSelector:
    any: true
---
```

### Prometheus Adapter Values (`config/samples/prometheus-adapter-values.yaml`)

```yaml
prometheus:
  url: https://thanos-querier.openshift-monitoring.svc.cluster.local
  port: 9091

rules:
  external:
  - seriesQuery: 'inferno_desired_replicas{variant_name!="",exported_namespace!=""}'
    resources:
      overrides:
        exported_namespace: {resource: "namespace"}
        variant_name: {resource: "deployment"}  
    name:
      matches: "^inferno_desired_replicas"
      as: "inferno_desired_replicas"
    metricsQuery: 'inferno_desired_replicas{<<.LabelMatchers>>}'

replicas: 2
logLevel: 4

tls:
  enable: false # Inbound TLS (Client → Adapter)

extraVolumes:
  - name: prometheus-ca
    configMap:
      name: prometheus-ca

extraVolumeMounts:
  - name: prometheus-ca
    mountPath: /etc/prometheus-ca
    readOnly: true

extraArguments:
  - --prometheus-ca-file=/etc/prometheus-ca/ca.crt
  - --prometheus-token-file=/var/run/secrets/kubernetes.io/serviceaccount/token


# k8s 1.21 needs fsGroup to be set for non root deployments
# ref: https://github.com/kubernetes/kubernetes/issues/70679
podSecurityContext:
  fsGroup: 1000460000    # this may need to change, depending on the allowed IDs for the OCP project

# SecurityContext of the container
# ref. https://kubernetes.io/docs/tasks/configure-pod-container/security-context
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 1000460000   # this may need to change, depending on the allowed IDs for the OCP project
  seccompProfile:
    type: RuntimeDefault
```

### VariantAutoscaling Configuration Example (`config/samples/vllm-va.yaml`)

```yaml
apiVersion: llmd.ai/v1alpha1
# Optimizing a variant, create only when the model is deployed and serving traffic
# this is for the collector the collect existing (previous) running metrics of the variant.
kind: VariantAutoscaling
metadata:
  # Unique name of the variant
  name: ms-inference-scheduling-llm-d-modelservice-decode 
  namespace: llm-d-sim
  labels:
    inference.optimization/acceleratorName: H100
# This is essentially static input to the optimizer
spec:
  # OpenAI API compatible name of the model
  modelID: "Qwen/Qwen3-0.6B"
  # Add SLOs in configmap, add reference to this per model data
  # to avoid duplication and Move to ISOs when available
  sloClassRef:
    # Configmap name to load in the same namespace as optimizer object
    # we start with static (non-changing) ConfigMaps (for ease of implementation only)
    name: premium-slo
    # Key (modelID) present inside configmap
    key: opt-125m
  # Static profiled benchmarked data for a variant running on different accelerators
  modelProfile:
    accelerators:
      - acc: "A100"
        accCount: 1
        alpha: "20.58"
        beta: "0.41"
        maxBatchSize: 4
      - acc: "H100"
        accCount: 1
        alpha: "20.58"
        beta: "0.41"
        maxBatchSize: 4
      - acc: "MI300X"
        accCount: 1
        alpha: "7.77"
        beta: "0.15"
        maxBatchSize: 4
      - acc: "G2"
        accCount: 1
        alpha: "17.15"
        beta: "0.34"
        maxBatchSize: 4
```

### HPA Configuration Example (`config/samples/hpa-integration.yaml`)

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: vllm-deployment-hpa
  namespace: llm-d-sim
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ms-inference-scheduling-llm-d-modelservice-decode
  # minReplicas: 0  # scale to zero - alpha feature
  maxReplicas: 10
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
      - type: Pods
        value: 10
        periodSeconds: 15
    scaleDown:
      stabilizationWindowSeconds: 0
      policies:
      - type: Pods
        value: 10
        periodSeconds: 15
  metrics:
  - type: External
    external:
      metric:
        name: inferno_desired_replicas
        selector:
          matchLabels:
            variant_name: ms-inference-scheduling-llm-d-modelservice-decode
      target:
        type: AverageValue
        averageValue: "1"
```
