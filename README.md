# inferno-autoscaler
The inferno-autoscaler assigns GPU types to inference model servers and decides on the number of replicas for each model for a given request traffic load and classes of service, as well as the batch size.

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

## Getting Started

### Prerequisites
- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

## Optimizer Options

The inferno-autoscaler supports two optimization engines:

### **Go Optimizer (Default)**
- **Description**: Uses the existing `inferno-optimizer-light` library
- **Benefits**: Lightweight, fast, minimal dependencies
- **Use cases**: Production deployments, resource-constrained environments
- **Prerequisites**: Go build environment only
- **Image size**: ~20MB (distroless)

### **Python Optimizer**
- **Description**: Advanced mathematical optimization using Python-based solvers
- **Benefits**: More sophisticated optimization algorithms, flexibility for research
- **Use cases**: Advanced optimization scenarios, research environments
- **Prerequisites**: Python 3.11+ with dependencies (numpy, pandas, scipy, docplex)
- **Image size**: ~200MB (includes Python runtime)

## Docker Build Options

Choose the appropriate Docker build based on your optimizer needs:

### **Option 1: Go Optimizer Only (Lightweight)**
```bash
# Build minimal Go-only image
docker build -f Dockerfile_GO -t your-registry/inferno:go .

# Deploy with Go optimizer
kubectl set env deployment/inferno-autoscaler INFERNO_OPTIMIZER_TYPE=go
```

### **Option 2: Full Support (Go + Python)**
```bash
# Build full-featured image (default)
docker build -t your-registry/inferno:full .

# Or use make command
make docker-build IMG=your-registry/inferno:full
```

### **Environment Configuration**

Control optimizer behavior with environment variables:

```bash
# Optimizer selection
INFERNO_OPTIMIZER_TYPE=go          # "go" or "python" (default: go)

# Python optimizer configuration (when INFERNO_OPTIMIZER_TYPE=python)
INFERNO_PYTHON_PATH=python3        # Python executable path
INFERNO_PYTHON_SCRIPT=/autoscaler/cmd_folder/go_autoscaler_wrapper.py
INFERNO_WORKING_DIR=/tmp           # Temporary files directory

# Common configuration
PROMETHEUS_BASE_URL=http://localhost:9090
GLOBAL_OPT_INTERVAL=60s
GLOBAL_OPT_TRIGGER=false
```

### **Quick Start Guide**

**For Go Optimizer (Recommended for most users):**
```bash
# Use lightweight image
docker build -f Dockerfile_GO -t inferno:go .
# Default environment variables work out of the box
```

**For Python Optimizer:**
```bash
# Use full image with Python support
docker build -t inferno:full .
# Set optimizer type
export INFERNO_OPTIMIZER_TYPE=python
```

**Development Setup:**
```bash
# Copy environment template
cp .env.example .env
# Edit .env to choose your optimizer and configuration
# Source the environment
source .env
```

## Quickstart: Emulated Deployment on Kind
- Emulated deployment, creates fake gpu resources on the node and deploys inferno on the cluster where inferno consumes fake gpu resources. As well as the emulated vllm server (vllme).

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/inferno-controller:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

Use this target to spin up a complete local test environment:

```sh
make deploy-inferno-emulated-on-kind IMG=<some-registry>/inferno-controller:tag KIND_ARGS="-t mix -n 3 -g 4"

# -t mix - mix vendors
# -n - number of nodes
# -g - number of gpus per node 

# prebuilt image
# make deploy-inferno-emulated-on-kind 
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

The default set up:
- Deploys a 3 Kind nodes, 2 GPUs per node, mixed vendors with fake GPU resources
- Preloaded Inferno image
- CRDs and controller deployment
- Apply configuration data
- Install Prometheus via Helm
- vLLM emulator and load generator (OpenAI-based)

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


### To Uninstall

**Delete the APIs(CRDs) from the cluster:**

```sh
make undeploy-inferno-on-kind
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**Delete cluster**

```sh
make destroy-kind-cluster
```

## Local development vllme setup

Local development will need emulated vllm server, prometheus installed in KinD cluster. 

This script already deploys emulated vllm server:
```sh
make deploy-inferno-emulated-on-kind
```

**Expose the prometheus server**

```sh
# Wait for all pods to be ready before port forwarding
sleep 30 && kubectl get pods -A | grep -E "(inferno|vllme|prometheus)"

# Port forward Prometheus
kubectl port-forward svc/prometheus-operated 9090:9090 -n inferno-autoscaler-monitoring
# server can be accessed at location: http://localhost:9090
```

**Important**: Always ensure pods are ready before attempting port forwarding to avoid connection errors.

**Check vllm emulated deployment**

```sh
kubectl get deployments -n llm-d-sim
NAME               READY   UP-TO-DATE   AVAILABLE   AGE
vllme-deployment   1/1     1            1           35s
```

**Expose the vllme server**
```sh
# Note: Ensure pods are ready before port forwarding (see Prometheus section above)
kubectl port-forward svc/vllme-service 8000:80
```

**Sanity checks**

Go to http://localhost:8000/metrics and check if you see metrics starting with vllm:. Refresh to see the values changing with the load generator on.

**Create variant autoscaling object for controller**
```sh
kubectl apply -f hack/vllme/deploy/vllme-setup/vllme-variantautoscaling.yaml

# view status of the variant autoscaling object to get status of optimization
kubectl get variantautoscaling vllme-deployment -o yaml
```

**Load generation**

- Generates synthetic load by launching client.py repeatedly
- Takes requests-per-minute (RPM) as input
- Default = 20 RPM
- Can be overridden by running:

```sh
#run script
cd ./hack/vllme/vllm_emulator
pip install -r requirements.txt
sh loadgen.sh 40

# rpm - request per minute (default = 20)

```

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

**Note**: The vllme ServiceMonitor is automatically created in the correct namespace (`inferno-autoscaler-monitoring`) with the proper labels for Prometheus discovery.

**Important**: The inferno-autoscaler ServiceMonitor must be deployed in the `inferno-autoscaler-monitoring` namespace (not `inferno-autoscaler-system`) for Prometheus to discover it. The ServiceMonitor includes a `namespaceSelector` to target services in the `inferno-autoscaler-system` namespace.

**Common Issue**: If the inferno-autoscaler target doesn't appear in Prometheus, check that the ServiceMonitor includes the `namespaceSelector` configuration. Without it, Prometheus won't discover services across different namespaces.

**Timing Note**: ServiceMonitor discovery can take 1-2 minutes after applying. Don't worry if targets don't appear immediately - this is normal behavior.

**Custom Metrics Verification**

To verify the custom metrics are working:

1. **Check Prometheus targets**: Ensure both targets are active
   ```sh
   # Check inferno-autoscaler target
   curl -s "http://localhost:9090/api/v1/targets" | jq '.data.activeTargets[] | select(.labels.job | contains("inferno"))'
   
   # Check vllme target
   curl -s "http://localhost:9090/api/v1/targets" | jq '.data.activeTargets[] | select(.labels.job | contains("vllme"))'
   ```
   
   **Note**: If targets don't appear immediately, wait 1-2 minutes for ServiceMonitor discovery to complete.

     **If targets still don't appear after waiting:**
     ```sh
     # Check Prometheus operator logs for discovery events
     kubectl logs -n inferno-autoscaler-monitoring deployment/kube-prometheus-stack-operator --tail=10

     # Check Prometheus pod logs for configuration reloads
     kubectl logs -n inferno-autoscaler-monitoring prometheus-kube-prometheus-stack-prometheus-0 -c prometheus --tail=10
     ```

2. **Query custom metrics**: Check if inferno metrics are being scraped
   ```sh
   # Core inferno metrics
   curl -s "http://localhost:9090/api/v1/query?query=inferno_replica_scaling_total"
   curl -s "http://localhost:9090/api/v1/query?query=inferno_current_replicas"
   curl -s "http://localhost:9090/api/v1/query?query=inferno_desired_replicas"
   
   # vLLM metrics
   curl -s "http://localhost:9090/api/v1/query?query=vllm:gpu_cache_usage_perc"
   ```

3. **Direct metrics endpoint access**: Verify metrics are exposed
   ```sh
   # Note: Ensure pods are ready before port forwarding
   # Inferno controller metrics
   kubectl port-forward svc/inferno-autoscaler-controller-manager-metrics-service 8080:8080 -n inferno-autoscaler-system &
   curl -s "http://localhost:8080/metrics" | grep -E "(inferno_|# HELP inferno)"
   
   # vLLM emulator metrics
   kubectl port-forward svc/vllme-service 8000:80 &
   curl -s "http://localhost:8000/metrics" | grep -E "(vllm|# HELP vllm)"
   ```

For detailed information about the custom metrics, see [Custom Metrics Documentation](docs/custom-metrics.md).

**Accessing the Grafana**


```sh
# username:admin
# password: prom-operator
kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80 -n inferno-autoscaler-monitoring
```

**Running the controller locally for dev**

If running the controller locally using `make run`, make sure to install [prerequisites](https://github.com/llm-inferno/optimizer?tab=readme-ov-file#prerequisites) first.

**For Python Optimizer (local development):**
```bash
# Install Python dependencies
cd autoscaler
pip install -r requirements.txt
cd ..

# Set environment variables
export INFERNO_OPTIMIZER_TYPE=python
export INFERNO_PYTHON_SCRIPT=$PWD/autoscaler/cmd_folder/go_autoscaler_wrapper.py
```

Once you've forwarded prometheus to localhost:9090, the command to run:

```shell
make run PROMETHEUS_BASE_URL=http://localhost:9090
```

**Note**: The controller will automatically detect the optimizer type from environment variables. See [Optimizer Options](#optimizer-options) for configuration details.

**Prometheus Configuration**

The controller supports flexible Prometheus configuration through multiple methods (in order of precedence):

1. **Environment Variable** (highest priority):
   ```shell
   PROMETHEUS_BASE_URL=http://localhost:9090
   ```

2. **ConfigMap Configuration**:
   The `inferno-autoscaler-variantautoscaling-config` ConfigMap in the `inferno-autoscaler-system` namespace contains:
   ```yaml
   data:
     PROMETHEUS_BASE_URL: "http://prometheus-operated.inferno-autoscaler-monitoring.svc.cluster.local:9090"
     GLOBAL_OPT_INTERVAL: "60s"
     GLOBAL_OPT_TRIGGER: "false"
   ```

3. **Default In-Cluster Address** (fallback):
   ```shell
   http://prometheus-operated.inferno-autoscaler-monitoring.svc.cluster.local:9090
   ```

**Customizing Prometheus URL for Different Environments:**

**Local Development:**
```shell
# When running locally with port-forwarded Prometheus
PROMETHEUS_BASE_URL=http://localhost:9090
```

**Different Cluster Namespace:**
```shell
# If Prometheus is in a different namespace
PROMETHEUS_BASE_URL=http://prometheus-operated.monitoring.svc.cluster.local:9090
```

**External Prometheus:**
```shell
# For external Prometheus instances
PROMETHEUS_BASE_URL=https://prometheus.example.com:9090
```

**Custom Prometheus Installation:**
```shell
# For custom Prometheus deployments
PROMETHEUS_BASE_URL=http://my-prometheus.my-namespace.svc.cluster.local:9090
```

**Note**: The `PROMETHEUS_BASE_URL` is now automatically set in the deployment configuration for in-cluster deployments. The controller includes retry logic to handle Prometheus startup delays and will wait up to 5 minutes for Prometheus to become available.

## Security Considerations

### Metrics Endpoint Security
The controller's metrics endpoint is currently configured for HTTP access on port 8080 without TLS encryption. This configuration is suitable for development and testing environments but should be secured for production deployments.

**For Production Deployments:**
1. Enable secure metrics by setting `--metrics-secure=true`
2. Configure TLS certificates for the metrics endpoint
3. Use network policies to restrict access to the metrics port
4. Consider using a reverse proxy with TLS termination

**Example Production Configuration:**
```yaml
# In config/default/manager_metrics_patch.yaml
- op: replace
  path: /spec/template/spec/containers/0/args/1
  value: --metrics-secure=true

## Integration with llm-d 

Use this target to spin up a local test environment integrated with llm-d core components:

```sh
make deploy-llm-d-inferno-emulated-on-kind
```

This target deploys an environment ready for testing, integrating the llm-d infrastructure and the Inferno-autoscaler.

The default set up:
- Deploys a 3 Kind nodes, 2 GPUs per node, mixed vendors with fake GPU resources
- Includes the Inferno autoscaler
- Installs the [llm-d core infrastructure for simulation purposes](https://github.com/llm-d-incubation/llm-d-infra/blob/main/quickstart/examples/sim/README.md)
- Includes vLLM emulator and load generator (OpenAI-based)

To curl the Gateway:
1. Find the gateway service:
```sh
kubectl get services -n llm-d-sim
NAME                          TYPE        CLUSTER-IP    EXTERNAL-IP   PORT(S)             AGE
gaie-sim-epp                  ClusterIP   10.16.2.6     <none>        9002/TCP,9090/TCP   42s
infra-sim-inference-gateway   NodePort    10.16.2.157   <none>        80:37479/TCP        64s
```

2. Then `port-forward` the gateway service to we can curl it:
```sh
kubectl port-forward -n llm-d-sim service/infra-sim-inference-gateway 8000:80
```

3. Launch the `loadgen.py` load generator to send requests to the `v1/chat/completions` endpoint:
```sh
cd hack/vllme/vllm_emulator
python loadgen.py
```

- To request the gateway, as '*server base URL*' use **http://localhost:8000/v1** [**option 3**]
- As '*model name*', insert: "**vllm**"

**Note**: since the environment uses vllm-emulator, the **Criticality** parameter is set to `critical` for emulation purposes.

### Uninstalling llm-d and Inferno-autoscaler 
Use this target to undeploy the integrated test environment and related resources:

```sh
make undeploy-llm-d-inferno-emulated-on-kind
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/inferno-autoscaler:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/inferno-autoscaler/<tag or branch>/dist/install.yaml
```

## Contributing

Please join llmd autoscaling community meetings and feel free to submit github issues and PRs. 

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

