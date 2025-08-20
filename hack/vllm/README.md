# vllm with wva autoscaler


Prerequisites: 
1. Access to OpenShift Cluster.
2. Hugging Face token.
3. This tutorial uses  `vllm-test` namespace for vllm deployment components and the load generator jobs. If the namespace doesn't already exists, create one by running `oc create ns vllm-test`.
4. We assume that the wva autoscaler is deployed in the `inferno-autoscaler-system` namespace.

## Setting up a vllm deployment and service
The following is largely based on existing reference material with a few tweaks. 
Refs:
1. https://docs.vllm.ai/en/v0.9.2/deployment/k8s.html#deployment-with-gpus
2. https://github.com/rh-aiservices-bu/llm-on-openshift/tree/main/llm-servers/vllm/gpu

### Step 1: Create a PVC
Create PVC (`oc apply -f vllm-deploy/pvc.yaml`) named `vllm-models-cache` with enough space to hold all the models you want to try.
```yaml
# pvc.ymal
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: vllm-models-cache
  namespace: vllm-test
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
  resources:
    requests:
      storage: 100Gi
```
Note: 
A storage class field is not explicitly set in the provided yaml, and therefore the created pvc will be bound to the default storage class. To use another storage class, run  `oc get storageclass` to get the available options.
Before proceeding to next steps, make sure that the `STATUS` of pvc is `BOUND`.

### Step 2: Create a secret
Secret is optional and only required for accessing gated models, you can skip this step if you are not using gated models.

Run `oc apply -f vllm-deploy/secret.yaml`
```yaml
# secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: hf-token-secret
  namespace: vllm-test
type: Opaque
stringData:
  token: "<your-hf-token>"
```

### Step 3: Create deployment
The following example deploys the `unsloth/Meta-Llama-3.1-8B` model with 1 replica. We use H100 GPUs for our deployments.

Run `oc apply -f vllm-deploy/deployment.yaml`.
```yaml
# deployment.yaml
kind: Deployment
apiVersion: apps/v1
metadata:
  name: vllm
  namespace: vllm-test
  labels:
    app: vllm
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vllm
  template:
    metadata:
      labels:
        app: vllm
    spec:
      restartPolicy: Always
      schedulerName: default-scheduler
      affinity: {}
      terminationGracePeriodSeconds: 120
      securityContext: {}
      containers:
        - resources:
            limits:
              cpu: '8'
              memory: 24Gi
              nvidia.com/gpu: '1'
            requests:
              cpu: '6'
              memory: 6Gi
              nvidia.com/gpu: '1'
          readinessProbe:
            httpGet:
              path: /health
              port: http
              scheme: HTTP
            timeoutSeconds: 5
            periodSeconds: 30
            successThreshold: 1
            failureThreshold: 3
          terminationMessagePath: /dev/termination-log
          name: server
          livenessProbe:
            httpGet:
              path: /health
              port: http
              scheme: HTTP
            timeoutSeconds: 8
            periodSeconds: 100
            successThreshold: 1
            failureThreshold: 3
          env:
            - name: HUGGING_FACE_HUB_TOKEN
              valueFrom:
                secretKeyRef:
                  name: hf-token-secret
                  key: token
            - name: HOME
              value: /models-cache
            - name: VLLM_PORT
              value: "8000"
          args: [
            "vllm serve unsloth/Meta-Llama-3.1-8B --trust-remote-code --download-dir /models-cache --dtype float16"
            ]
          securityContext:
            capabilities:
              drop:
                - ALL
            runAsNonRoot: true
            allowPrivilegeEscalation: false
            seccompProfile:
              type: RuntimeDefault
          ports:
            - name: http
              containerPort: 8000
              protocol: TCP
          imagePullPolicy: IfNotPresent
          startupProbe:
            httpGet:
              path: /health
              port: http
              scheme: HTTP
            timeoutSeconds: 1
            periodSeconds: 30
            successThreshold: 1
            failureThreshold: 24
          volumeMounts:
            - name: models-cache
              mountPath: /models-cache
            - name: shm
              mountPath: /dev/shm
          terminationMessagePolicy: File
          image: 'vllm/vllm-openai:latest'
          command: ["/bin/sh","-c"]
      volumes:
        - name: models-cache
          persistentVolumeClaim:
            claimName: vllm-models-cache
        - name: shm
          emptyDir:
            medium: Memory
            sizeLimit: 1Gi
      dnsPolicy: ClusterFirst
      tolerations:
        - key: nvidia.com/gpu
          operator: Exists
          effect: NoSchedule
  strategy:
    type: Recreate
  revisionHistoryLimit: 10
  progressDeadlineSeconds: 600
```

Wait until the pod is in the `READY` state before proceeding to next steps.

### Create a service
Create a service to expose the vllm deployment: `oc apply -f vllm-deploy/service.yaml`
```yaml
# service.yaml
kind: Service
apiVersion: v1
metadata:
  name: vllm
  namespace: vllm-test
  labels:
    app: vllm
spec:
  ports:
    - name: http
      protocol: TCP
      port: 8000
      targetPort: http
  selector:
    app: vllm
  type: ClusterIP   # default, enables load-balancing
```

Run `oc get service` to make sure that the service indeed has `CLUSTER-IP` set.

### Create a ServiceMonitor
We need service monitor to let Prometheus scrape vllm metrics: `oc apply -f vllm-deploy/service-monitor.yaml `
```yaml
# service-monitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: vllm-monitor
  namespace: vllm-test
  labels:
    app: vllm
    release: kube-prometheus-stack   
spec:
  selector:
    matchLabels:
      app: vllm
  endpoints:
  - port: http
    interval: 15s
    path: /metrics
  namespaceSelector:
    any: true
```


## Deploy configmaps and VA object
Create accelerator configmap (`oc apply -f deploy/configmap-accelerator-unitcost.yaml`):
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: accelerator-unit-costs
  namespace: inferno-autoscaler-system
data:
  A100: |
    {
    "device": "NVIDIA-A100-PCIE-80GB",
    "cost": "40.00"
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
  H100: |
    {
    "device": "NVIDIA-H100-80GB-HBM3",
    "cost": "100.0"
    }
```

Create service class config map `oc apply -f deploy/configmap-serviceclass.yaml`
```yaml
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
      - model: default/default
        slo-tpot: 24
        slo-ttft: 500
      - model: llama0-70b
        slo-tpot: 80
        slo-ttft: 500
      - model: unsloth/Meta-Llama-3.1-8B
        slo-tpot: 9
        slo-ttft: 1000
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
Create VA object to manage the `vllm` deployment: `oc apply -f va/vllm-va.yaml`.
```yaml
apiVersion: llmd.ai/v1alpha1
kind: VariantAutoscaling
metadata:
  name: vllm
  namespace: vllm-test
  labels:
    inference.optimization/modelName: Meta-Llama-3.1-8B
    inference.optimization/acceleratorName: H100
spec:
  modelID: unsloth/Meta-Llama-3.1-8B
  sloClassRef:
    name: premium-slo
    key: opt-125m
  modelProfile:
    accelerators:
      - acc: "H100"
        accCount: 1
        alpha: "6.356"
        beta: "0.044"
        maxBatchSize: 512
        atTokens: 512
```


## Setting up load generator
We use `guidellm` as the load generator and we run our load generator from a different pod within the same OpenShift cluster. This is the best way to test the service's behavior because pods can reach other pods through the service's internal DNS name. The DNS name `vllm.vllm-test.svc.cluster.local` (or simply `vllm` within the same namespace) will resolve to the service's cluster IP, which then load balances requests across all available pods.

### Step 1 : Create an image
First create a `Dockerfile`
```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
CMD ["tail", "-f", "/dev/null"]
```

Then create a `requirements.txt` with the following contents in the same directory as your `Dockerfile`
```Plaintext
guidellm
```

Build the image for the correct target CPU architecture. 
You can get the architecture of the OpenShift node by `oc get nodes -o custom-columns=NAME:.metadata.name,ARCH:.status.nodeInfo.architecture`.
In our case, it was `linux/amd64`
```sh
docker build --platform linux/amd64 -t <image-repo>:<tag> .
```

Push the image
```sh
docker push <image-repo>:<tag>
```

Make the image **public**.


### Run the load generator
Create two jobs `guidellm-job-1.yaml` and `guidellm-job-2.yaml` based on the following template using the image created in step 1.
```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: guidellm-job
  namespace: vllm-test
spec:
  template:
    spec:
      containers:
      - name: guidellm-benchmark-container
        image: <image-repo>:<tag>
        imagePullPolicy: IfNotPresent
        env:
        - name: HF_HOME
          value: "/tmp"
        command: ["/usr/local/bin/guidellm"]
        args:
        - "benchmark"
        - "--target"
        - "http://vllm:8000"
        - "--rate-type"
        - "constant"
        - "--rate"
        - "<rate>"
        - "--max-seconds"
        - "<max-seconds>"
        - "--model"
        - "unsloth/Meta-Llama-3.1-8B"
        - "--data"
        - "prompt_tokens=128,output_tokens=512"
        - "--output-path"
        - "/tmp/benchmarks.json" 
      restartPolicy: Never
  backoffLimit: 4
```

We emulate the dynamic load as follows:
- In `guidellm-job-1.yaml`, we set `<rate>` and `<max-seconds>` is set to `8` and `720` respectively. By doing this, we force `guidellm` client to send requests at rate `8` requests per second (480 req/min) for `720` seconds (12 minutes).
- In `guidellm-job-2.yaml`, we set `<rate>` and `<max-seconds>` is set to `8` and `360` respectively. We start this job after a couple of minutes of starting `guidellm-job-1`. When both jobs are running, we are effectively sending requests at rate `8+8 = 16` requests per second (960 req/min) for `360` seconds (6 minutes).
- With this setup, `guidellm-job-2` will complete first, bringing the effective request rate back to `8` req/sec. This is followed by the completion of `guidellm-job-1` after which no further requests are sent.


## WVA Performance
In the following, we describe the observed behaviour of the autoscaler.
 
### Step 1: Start `guidellm-job-1`
```sh
{"level":"DEBUG","ts":"2025-08-20T19:37:49.282Z","msg":"Found inventory: nodeName - pokprod-b93r38s1 , model - NVIDIA-H100-80GB-HBM3 , count - 7 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.282Z","msg":"Found inventory: nodeName - pokprod-b93r38s2 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.282Z","msg":"Found inventory: nodeName - pokprod-b93r38s0 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.282Z","msg":"Found inventory: nodeName - pokprod-b93r39s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"INFO","ts":"2025-08-20T19:37:49.282Z","msg":"Found SLO for model - model: unsloth/Meta-Llama-3.1-8B, class: Premium, slo-tpot: 9, slo-ttft: 1000"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.302Z","msg":"System data prepared for optimization: - { count: [  {   type: NVIDIA-H100-80GB-HBM3,   count: 31  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.302Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  },  {   name: H100,   type: NVIDIA-H100-80GB-HBM3,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 100  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  },  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.302Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Freemium,   model: granite-13b,   priority: 10,   slo-itl: 200,   slo-ttw: 2000,   slo-tps: 0  },  {   name: Freemium,   model: llama0-7b,   priority: 10,   slo-itl: 150,   slo-ttw: 1500,   slo-tps: 0  },  {   name: Premium,   model: default/default,   priority: 1,   slo-itl: 24,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: llama0-70b,   priority: 1,   slo-itl: 80,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: unsloth/Meta-Llama-3.1-8B,   priority: 1,   slo-itl: 9,   slo-ttw: 1000,   slo-tps: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"System data prepared for optimization: - { models: [  {   name: unsloth/Meta-Llama-3.1-8B,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: MI300X,   accCount: 1,   alpha: 7.77,   beta: 0.15,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: G2,   accCount: 1,   alpha: 17.15,   beta: 0.34,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: H100,   accCount: 1,   alpha: 6.356,   beta: 0.044,   maxBatchSize: 512,   atTokens: 512  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: false,  heterogeneous: false }}"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"System data prepared for optimization: - { servers: [  {   name: vllm:vllm-test,   class: Premium,   model: unsloth/Meta-Llama-3.1-8B,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 512,   currentAlloc: {    accelerator: H100,    numReplicas: 1,    maxBatch: 256,    cost: 100,    itlAverage: 8.46,    waitAverage: 0,    load: {     arrivalRate: 480,     avgLength: 512,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"Optimization solution - system: Solution: \nc=Premium; m=unsloth/Meta-Llama-3.1-8B; rate=480; tk=512; sol=1, alloc={acc=H100; num=1; maxBatch=512; cost=100, val=0, servTime=7.8070107, waitTime=0, rho=1}; slo-itl=9, slo-ttw=1000, slo-tps=0 \nAllocationByType: \nname=NVIDIA-H100-80GB-HBM3, count=1, limit=31, cost=100 \ntotalCost=100 \n"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"Optimized allocation entry - key: vllm, value: {2025-08-20 19:37:49.303100159 +0000 UTC m=+160977.023504769 H100 1}"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.303Z","msg":"Processing variant - index: 0, variantAutoscaling-name: vllm, namespace: vllm-test, has_optimized_alloc: true"}
{"level":"INFO","ts":"2025-08-20T19:37:49.326Z","msg":"Patched Deployment: name: vllm, num-replicas: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.331Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-20T19:37:49.331Z","msg":"Emitted metrics for variantAutoscaling - variantAutoscaling-name: vllm, namespace: vllm-test"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.331Z","msg":"EmitMetrics call completed successfully for variantAutoscaling - variantAutoscaling-name: vllm"}
{"level":"DEBUG","ts":"2025-08-20T19:37:49.331Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-20T19:37:49.331Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
```



### Step 2: Start `guidellm-job-2`
1. Observation 1: ITL SLO violated. Autoscaler suggests scaling the deployment to 2 replicas.
```sh
{"level":"DEBUG","ts":"2025-08-20T19:41:49.436Z","msg":"Found inventory: nodeName - pokprod-b93r38s0 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.436Z","msg":"Found inventory: nodeName - pokprod-b93r38s1 , model - NVIDIA-H100-80GB-HBM3 , count - 7 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.436Z","msg":"Found inventory: nodeName - pokprod-b93r38s2 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.436Z","msg":"Found inventory: nodeName - pokprod-b93r39s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"INFO","ts":"2025-08-20T19:41:49.436Z","msg":"Found SLO for model - model: unsloth/Meta-Llama-3.1-8B, class: Premium, slo-tpot: 9, slo-ttft: 1000"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.454Z","msg":"System data prepared for optimization: - { count: [  {   type: NVIDIA-H100-80GB-HBM3,   count: 31  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.454Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  },  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  },  {   name: H100,   type: NVIDIA-H100-80GB-HBM3,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 100  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.454Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Freemium,   model: granite-13b,   priority: 10,   slo-itl: 200,   slo-ttw: 2000,   slo-tps: 0  },  {   name: Freemium,   model: llama0-7b,   priority: 10,   slo-itl: 150,   slo-ttw: 1500,   slo-tps: 0  },  {   name: Premium,   model: default/default,   priority: 1,   slo-itl: 24,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: llama0-70b,   priority: 1,   slo-itl: 80,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: unsloth/Meta-Llama-3.1-8B,   priority: 1,   slo-itl: 9,   slo-ttw: 1000,   slo-tps: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.454Z","msg":"System data prepared for optimization: - { models: [  {   name: unsloth/Meta-Llama-3.1-8B,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: MI300X,   accCount: 1,   alpha: 7.77,   beta: 0.15,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: G2,   accCount: 1,   alpha: 17.15,   beta: 0.34,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: H100,   accCount: 1,   alpha: 6.356,   beta: 0.044,   maxBatchSize: 512,   atTokens: 512  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.454Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: false,  heterogeneous: false }}"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.454Z","msg":"System data prepared for optimization: - { servers: [  {   name: vllm:vllm-test,   class: Premium,   model: unsloth/Meta-Llama-3.1-8B,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 512,   currentAlloc: {    accelerator: H100,    numReplicas: 1,    maxBatch: 256,    cost: 100,    itlAverage: 10.33,    waitAverage: 0,    load: {     arrivalRate: 961.33,     avgLength: 512,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.454Z","msg":"Optimization solution - system: Solution: \nc=Premium; m=unsloth/Meta-Llama-3.1-8B; rate=961.33; tk=512; sol=1, alloc={acc=H100; num=2; maxBatch=512; cost=200, val=100, servTime=7.8093896, waitTime=0, rho=1}; slo-itl=9, slo-ttw=1000, slo-tps=0 \nAllocationByType: \nname=NVIDIA-H100-80GB-HBM3, count=2, limit=31, cost=200 \ntotalCost=200 \n"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.455Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.455Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.455Z","msg":"Optimized allocation entry - key: vllm, value: {2025-08-20 19:41:49.45500309 +0000 UTC m=+161217.175407701 H100 2}"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.455Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.455Z","msg":"Processing variant - index: 0, variantAutoscaling-name: vllm, namespace: vllm-test, has_optimized_alloc: true"}
{"level":"INFO","ts":"2025-08-20T19:41:49.462Z","msg":"Patched Deployment: name: vllm, num-replicas: 2"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.473Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-20T19:41:49.473Z","msg":"Emitted metrics for variantAutoscaling - variantAutoscaling-name: vllm, namespace: vllm-test"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.473Z","msg":"EmitMetrics call completed successfully for variantAutoscaling - variantAutoscaling-name: vllm"}
{"level":"DEBUG","ts":"2025-08-20T19:41:49.473Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-20T19:41:49.473Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
```
2. Observation 2: With the current rate and deployment scaled to 2 replicas, the ITL SLO is achieved.
```sh
{"level":"DEBUG","ts":"2025-08-20T19:45:49.586Z","msg":"Found inventory: nodeName - pokprod-b93r38s0 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.586Z","msg":"Found inventory: nodeName - pokprod-b93r38s1 , model - NVIDIA-H100-80GB-HBM3 , count - 7 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.586Z","msg":"Found inventory: nodeName - pokprod-b93r38s2 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.586Z","msg":"Found inventory: nodeName - pokprod-b93r39s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"INFO","ts":"2025-08-20T19:45:49.586Z","msg":"Found SLO for model - model: unsloth/Meta-Llama-3.1-8B, class: Premium, slo-tpot: 9, slo-ttft: 1000"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"System data prepared for optimization: - { count: [  {   type: NVIDIA-H100-80GB-HBM3,   count: 31  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: H100,   type: NVIDIA-H100-80GB-HBM3,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 100  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  },  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  },  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Freemium,   model: granite-13b,   priority: 10,   slo-itl: 200,   slo-ttw: 2000,   slo-tps: 0  },  {   name: Freemium,   model: llama0-7b,   priority: 10,   slo-itl: 150,   slo-ttw: 1500,   slo-tps: 0  },  {   name: Premium,   model: default/default,   priority: 1,   slo-itl: 24,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: llama0-70b,   priority: 1,   slo-itl: 80,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: unsloth/Meta-Llama-3.1-8B,   priority: 1,   slo-itl: 9,   slo-ttw: 1000,   slo-tps: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"System data prepared for optimization: - { models: [  {   name: unsloth/Meta-Llama-3.1-8B,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: MI300X,   accCount: 1,   alpha: 7.77,   beta: 0.15,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: G2,   accCount: 1,   alpha: 17.15,   beta: 0.34,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: H100,   accCount: 1,   alpha: 6.356,   beta: 0.044,   maxBatchSize: 512,   atTokens: 512  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: false,  heterogeneous: false }}"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"System data prepared for optimization: - { servers: [  {   name: vllm:vllm-test,   class: Premium,   model: unsloth/Meta-Llama-3.1-8B,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 512,   currentAlloc: {    accelerator: H100,    numReplicas: 2,    maxBatch: 256,    cost: 200,    itlAverage: 8.44,    waitAverage: 0,    load: {     arrivalRate: 965.33,     avgLength: 512,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"Optimization solution - system: Solution: \nc=Premium; m=unsloth/Meta-Llama-3.1-8B; rate=965.33; tk=512; sol=1, alloc={acc=H100; num=2; maxBatch=512; cost=200, val=0, servTime=7.816551, waitTime=0, rho=1}; slo-itl=9, slo-ttw=1000, slo-tps=0 \nAllocationByType: \nname=NVIDIA-H100-80GB-HBM3, count=2, limit=31, cost=200 \ntotalCost=200 \n"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.604Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.605Z","msg":"Optimized allocation entry - key: vllm, value: {2025-08-20 19:45:49.604992884 +0000 UTC m=+161457.325397492 H100 2}"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.605Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.605Z","msg":"Processing variant - index: 0, variantAutoscaling-name: vllm, namespace: vllm-test, has_optimized_alloc: true"}
{"level":"INFO","ts":"2025-08-20T19:45:49.614Z","msg":"Patched Deployment: name: vllm, num-replicas: 2"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.620Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-20T19:45:49.620Z","msg":"Emitted metrics for variantAutoscaling - variantAutoscaling-name: vllm, namespace: vllm-test"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.620Z","msg":"EmitMetrics call completed successfully for variantAutoscaling - variantAutoscaling-name: vllm"}
{"level":"DEBUG","ts":"2025-08-20T19:45:49.620Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-20T19:45:49.620Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
```


### Step 3: Stopped `guidellm-job-2`
```sh
====================================
{"level":"DEBUG","ts":"2025-08-20T19:46:49.621Z","msg":"Found inventory: nodeName - pokprod-b93r39s1 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.621Z","msg":"Found inventory: nodeName - pokprod-b93r38s0 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.621Z","msg":"Found inventory: nodeName - pokprod-b93r38s1 , model - NVIDIA-H100-80GB-HBM3 , count - 7 , mem - 81559"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.621Z","msg":"Found inventory: nodeName - pokprod-b93r38s2 , model - NVIDIA-H100-80GB-HBM3 , count - 8 , mem - 81559"}
{"level":"INFO","ts":"2025-08-20T19:46:49.621Z","msg":"Found SLO for model - model: unsloth/Meta-Llama-3.1-8B, class: Premium, slo-tpot: 9, slo-ttft: 1000"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"System data prepared for optimization: - { count: [  {   type: NVIDIA-H100-80GB-HBM3,   count: 31  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"System data prepared for optimization: - { accelerators: [  {   name: G2,   type: Intel-Gaudi-2-96GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 23  },  {   name: H100,   type: NVIDIA-H100-80GB-HBM3,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 100  },  {   name: MI300X,   type: AMD-MI300X-192GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 65  },  {   name: A100,   type: NVIDIA-A100-PCIE-80GB,   multiplicity: 1,   memSize: 0,   memBW: 0,   power: {    idle: 0,    full: 0,    midPower: 0,    midUtil: 0   },   cost: 40  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"System data prepared for optimization: - { serviceClasses: [  {   name: Freemium,   model: granite-13b,   priority: 10,   slo-itl: 200,   slo-ttw: 2000,   slo-tps: 0  },  {   name: Freemium,   model: llama0-7b,   priority: 10,   slo-itl: 150,   slo-ttw: 1500,   slo-tps: 0  },  {   name: Premium,   model: default/default,   priority: 1,   slo-itl: 24,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: llama0-70b,   priority: 1,   slo-itl: 80,   slo-ttw: 500,   slo-tps: 0  },  {   name: Premium,   model: unsloth/Meta-Llama-3.1-8B,   priority: 1,   slo-itl: 9,   slo-ttw: 1000,   slo-tps: 0  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"System data prepared for optimization: - { models: [  {   name: unsloth/Meta-Llama-3.1-8B,   acc: A100,   accCount: 1,   alpha: 20.58,   beta: 0.41,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: MI300X,   accCount: 1,   alpha: 7.77,   beta: 0.15,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: G2,   accCount: 1,   alpha: 17.15,   beta: 0.34,   maxBatchSize: 4,   atTokens: 128  },  {   name: unsloth/Meta-Llama-3.1-8B,   acc: H100,   accCount: 1,   alpha: 6.356,   beta: 0.044,   maxBatchSize: 512,   atTokens: 512  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"System data prepared for optimization: - { optimizer: {  unlimited: false,  heterogeneous: false }}"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"System data prepared for optimization: - { servers: [  {   name: vllm:vllm-test,   class: Premium,   model: unsloth/Meta-Llama-3.1-8B,   keepAccelerator: true,   minNumReplicas: 1,   maxBatchSize: 512,   currentAlloc: {    accelerator: H100,    numReplicas: 2,    maxBatch: 256,    cost: 200,    itlAverage: 8.22,    waitAverage: 0,    load: {     arrivalRate: 712,     avgLength: 512,     arrivalCOV: 0,     serviceCOV: 0    }   },   desiredAlloc: {    accelerator: ,    numReplicas: 0,    maxBatch: 0,    cost: 0,    itlAverage: 0,    waitAverage: 0,    load: {     arrivalRate: 0,     avgLength: 0,     arrivalCOV: 0,     serviceCOV: 0    }   }  } ]}"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"Optimization solution - system: Solution: \nc=Premium; m=unsloth/Meta-Llama-3.1-8B; rate=712; tk=512; sol=1, alloc={acc=H100; num=1; maxBatch=512; cost=100, val=-100, servTime=8.735201, waitTime=0, rho=1}; slo-itl=9, slo-ttw=1000, slo-tps=0 \nAllocationByType: \nname=NVIDIA-H100-80GB-HBM3, count=1, limit=31, cost=100 \ntotalCost=100 \n"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"Optimization completed successfully, emitting optimization metrics"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"Optimized allocation map - numKeys: 1, updateList_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"Optimized allocation entry - key: vllm, value: {2025-08-20 19:46:49.643823182 +0000 UTC m=+161517.364227791 H100 1}"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"Optimization metrics emitted, starting to process variants - variant_count: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.643Z","msg":"Processing variant - index: 0, variantAutoscaling-name: vllm, namespace: vllm-test, has_optimized_alloc: true"}
{"level":"INFO","ts":"2025-08-20T19:46:49.655Z","msg":"Patched Deployment: name: vllm, num-replicas: 1"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.664Z","msg":"EmitReplicaMetrics completed"}
{"level":"INFO","ts":"2025-08-20T19:46:49.664Z","msg":"Emitted metrics for variantAutoscaling - variantAutoscaling-name: vllm, namespace: vllm-test"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.664Z","msg":"EmitMetrics call completed successfully for variantAutoscaling - variantAutoscaling-name: vllm"}
{"level":"DEBUG","ts":"2025-08-20T19:46:49.664Z","msg":"Completed variant processing loop"}
{"level":"INFO","ts":"2025-08-20T19:46:49.664Z","msg":"Reconciliation completed - variants_processed: 1, optimization_successful: true"}
```




