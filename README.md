# inferno-autoscaler
The inferno-autoscaler assigns GPU types to inference model servers and decides on the number of replicas for each model for a given request traffic load and classes of service, as well as the batch size.

## Description

The inferno-autoscaler is a Kubernetes controller that performs optimizated autoscaling using the below components:

![Diagram](docs/diagrams/inferno-WVA-design.png)

Reconciler:

The controller is implemented using the controller-runtime framework, which reconciles the namespace-scoped Optimizer objects created by the platform administrator, one per model.Due to runtime variability in model behavior (e.g., differences in prompt lengths, output sizes, or server-level contention), we treat model analysis as a continuously reconciled step during every autoscaler loop.

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

### Create cluster with fake GPUs

```sh
bash deploy/local-cluster.sh
```

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/inferno-autoscaler:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Install the configmap to run optimizer loop:**

```sh
kubectl apply -f deploy/ticker-configmap.yaml
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/inferno-autoscaler:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the sample:

```sh
kubectl apply -k samples/input_sample.yaml
```

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -f amples/input_sample.yaml
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

**Delete cluster**

```sh
kind delete cluster -n a100-cluster
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

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

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

