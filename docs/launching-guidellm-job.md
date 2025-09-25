# Scaling up using GuideLLM Jobs

To verify the behavior of the deployed Workload-Variant-Autoscaler (WVA), we can use GuideLLM to generate traffic to be sent to the vLLM pods.

**Note**: to install the WVA on emulated mode, please refer to the existing README. If you want to install it on OpenShift, please refer to the [following PR](https://github.com/llm-d-incubation/workload-variant-autoscaler/pull/150).

1. Verify that `llm-d` is correctly installed:

```bash
export NAMESPACE="llm-d-inference-scheduling"
```

```bash
kubectl get all -n $NAMESPACE 
```

```bash
NAME                                                                  READY   STATUS      RESTARTS   AGE
pod/gaie-inference-scheduling-epp-6454879768-qxfrt                    1/1     Running     0          12h
pod/infra-inference-scheduling-inference-gateway-6d88fcd976-rp58r     1/1     Running     0          12h
pod/ms-inference-scheduling-llm-d-modelservice-decode-844bf77cp5xt7   2/2     Running     0          12h

NAME                                                   TYPE           CLUSTER-IP       EXTERNAL-IP   PORT(S)             AGE
service/gaie-inference-scheduling-epp                  ClusterIP      172.30.200.101   <none>        9002/TCP,9090/TCP   12h
service/infra-inference-scheduling-inference-gateway   LoadBalancer   172.30.116.252   <pending>     80:32392/TCP        12h
service/llm-d-benchmark-harness                        ClusterIP      172.30.27.22     <none>        20873/TCP           21h
service/vllm-service                                   NodePort       172.30.31.137    <none>        8200:30000/TCP      23h

NAME                                                                READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/gaie-inference-scheduling-epp                       1/1     1            1           12h
deployment.apps/infra-inference-scheduling-inference-gateway        1/1     1            1           12h
deployment.apps/ms-inference-scheduling-llm-d-modelservice-decode   1/1     1            1           12h

NAME                                                                           DESIRED   CURRENT   READY   AGE
replicaset.apps/gaie-inference-scheduling-epp-6454879768                       1         1         1       12h
replicaset.apps/infra-inference-scheduling-inference-gateway-6d88fcd976        1         1         1       12h
replicaset.apps/ms-inference-scheduling-llm-d-modelservice-decode-844bf77c46   1         1         1       12h

NAME                                                      REFERENCE                                                      TARGETS     MINPODS   MAXPODS   REPLICAS   AGE
horizontalpodautoscaler.autoscaling/vllm-deployment-hpa   Deployment/ms-inference-scheduling-llm-d-modelservice-decode   1/1 (avg)   1         10        1          23h
```

2. Create and launch a GuideLLM Job, sending traffic to the vLLM pods through the Gateway:

```bash
export ENDPOINT="infra-inference-scheduling-inference-gateway"
```

```bash
cat <<EOF | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: guidellm-job-1
  namespace: $NAMESPACE
spec:
  template:
    spec:
      containers:
      - name: guidellm-benchmark-container
        image: quay.io/vishakharamani/guidellm:latest
        imagePullPolicy: IfNotPresent
        env:
        - name: HF_HOME
          value: "/tmp"
        command: ["/usr/local/bin/guidellm"]
        args:
        - "benchmark"
        - "--target"
        - "http://$ENDPOINT:80"
        - "--rate-type"
        - "constant"
        - "--rate"
        - "18" # req/sec
        - "--max-seconds"
        - "1800"
        - "--model"
        - "unsloth/Meta-Llama-3.1-8B"
        - "--data"
        - "prompt_tokens=128,output_tokens=512"
        - "--output-path"
        - "/tmp/benchmarks.json" 
      restartPolicy: Never
  backoffLimit: 4
EOF
```

## Notes on the GuideLLM configuration

- This sample GuideLLM Job configuration is used to trigger a scale-up by the WVA, with the specified `Meta-Llama-3.1-8B` model. If you want to use another model, please consider to change the request rate and the configuration parameters appropriately.

- This sample GuideLLM Job generates synthetic data (see `--data prompt_tokens=128,output_tokens=512`) and sends traffic at a constant load rate of 18 req/sec (see `--rate-type constant --rate 18`) for 30 minutes (see `--max-seconds 1800`). Consider changing these configuration parameters appropriately for a different experiment.
