# vllm with wva autoscaler


Notes: 
1. The following describes setting up vllm deployment on OpenShift cluster.
2. These instructions are for standalone vllm deployments. To setup vllm with llm-d infra, refer to [Well-lit Path: Intelligent Inference Scheduling](https://github.com/llm-d/llm-d/blob/dev/guides/inference-scheduling/README.md).
3. All vLLM components will run in the `vllm-test` namespace. If the namespace doesn't already exists, create one by running `oc create ns vllm-test`.


## Setting up a vllm deployment and service
The following is largely based on existing reference material with a few tweaks. 
Refs:
1. https://docs.vllm.ai/en/v0.9.2/deployment/k8s.html#deployment-with-gpus
2. https://github.com/rh-aiservices-bu/llm-on-openshift/tree/main/llm-servers/vllm/gpu

### Step 1: Create a PVC
Create PVC (`oc apply -f pvc.yaml`) named `vllm-models-cache` with enough space to hold all the models you want to try.
```yaml
# pvc.ymal
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: vllm-models-cache
  namespace: vllm-test
spec:
  accessModes:
    - ReadWriteMany
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

Run `oc apply -f secret.yaml`
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

Run `oc apply -f deployment.yaml`.
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
          emptyDir: {}
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
Create a service to expose the vllm deployment: `oc apply -f service.yaml`
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
We need service monitor to let Prometheus scrape vllm metrics: `oc apply -f service-monitor.yaml `
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









