# Image URL to use all building/pushing image targets
IMAGE_TAG_BASE ?= quay.io/infernoautoscaler
IMG_TAG ?= 0.0.1-multi-arch
IMG ?= $(IMAGE_TAG_BASE)/inferno-controller:$(IMG_TAG)
KIND_ARGS ?= -t mix -n 3 -g 2   # Default: 3 nodes, 2 GPUs per node, mixed vendors

# Optimizer configuration
OPTIMIZER ?= full  # Options: "go" (lightweight), "full" (go+python)
DOCKERFILE ?= $(if $(filter go,$(OPTIMIZER)),Dockerfile_GO,Dockerfile)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: install-python-deps
install-python-deps: ## Install Python dependencies for Python optimizer
	@echo "Installing Python dependencies..."
	@cd autoscaler && pip install -r requirements.txt

.PHONY: setup-env-go
setup-env-go: ## Set up environment for Go optimizer
	@echo "Setting up environment for Go optimizer..."
	@echo "export INFERNO_OPTIMIZER_TYPE=go" > .env.local
	@echo "export INFERNO_WORKING_DIR=/tmp" >> .env.local
	@echo "Environment configured for Go optimizer. Source with: source .env.local"

.PHONY: setup-env-python
setup-env-python: install-python-deps ## Set up environment for Python optimizer
	@echo "Setting up environment for Python optimizer..."
	@echo "export INFERNO_OPTIMIZER_TYPE=python" > .env.local
	@echo "export INFERNO_PYTHON_PATH=python3" >> .env.local
	@echo "export INFERNO_PYTHON_SCRIPT=$$(pwd)/autoscaler/cmd_folder/go_autoscaler_wrapper.py" >> .env.local
	@echo "export INFERNO_WORKING_DIR=/tmp" >> .env.local
	@echo "Environment configured for Python optimizer. Source with: source .env.local"

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# Creates a multi-node Kind cluster
# Adds emulated GPU labels and capacities per node 
.PHONY: create-kind-cluster
create-kind-cluster:
	export KIND=$(KIND) KUBECTL=$(KUBECTL) && \
		hack/create-kind-gpu-cluster.sh $(KIND_ARGS)

# Destroys the Kind cluster created by `create-kind-cluster`
.PHONY: destroy-kind-cluster
destroy-kind-cluster:
	export KIND=$(KIND) KUBECTL=$(KUBECTL) && \
        hack/destroy-kind-cluster.sh

# Create Kind cluster (if needed)
# Deploys the Inferno Autoscaler on a Kind cluster with emulated GPU support.
# This target assumes that the Kind cluster has been created and is running.
.PHONY: deploy-inferno-emulated-on-kind
deploy-inferno-emulated-on-kind:
	@echo ">>> Deploying Inferno-autoscaler (cluster args: $(KIND_ARGS), image: $(IMG))"
	export KIND=$(KIND) KUBECTL=$(KUBECTL) IMG=$(IMG) && \
		hack/deploy-inferno-emulated-on-kind.sh $(KIND_ARGS)

# Deploy controller in emulator mode
.PHONY: deploy-emulated
deploy-emulated: deploy

.PHONY: undeploy-inferno-on-kind
undeploy-inferno-on-kind:
	make undeploy
	kubectl delete ns/inferno-autoscaler-system --ignore-not-found
	kubectl delete ns/inferno-autoscaler-monitoring --ignore-not-found

# Creates Kind cluster with emulated GPU support (if needed)
# Deploys the Inferno Autoscaler on a Kind cluster
# Deploys the llm-d components in the same Kind cluster
.PHONY: deploy-llm-d-inferno-emulated-on-kind
deploy-llm-d-inferno-emulated-on-kind:
	@echo ">>> Deploying integrated llm-d and Inferno-autoscaler (cluster args: $(KIND_ARGS), image: $(IMG))"
	export KIND=$(KIND) KUBECTL=$(KUBECTL) IMG=$(IMG) && \
		hack/deploy-llm-d-inferno-emulated-on-kind.sh $(KIND_ARGS)

.PHONY: undeploy-llm-d-inferno-emulated-on-kind
undeploy-llm-d-inferno-emulated-on-kind:
	@echo ">>> Undeploying llm-d and Inferno-autoscaler"
	hack/undeploy-llm-d-inferno-emulated-on-kind.sh

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
.PHONY: test-e2e
test-e2e: manifests generate fmt vet ## Run the e2e tests. Expected an isolated environment using Kind.
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@$(KIND) get clusters | grep -q 'kind' || { \
		echo "No Kind cluster is running. Please start a Kind cluster before running the e2e tests."; \
		exit 1; \
	}
	go test ./test/e2e/ -v -ginkgo.v

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Optimizer Setup

.PHONY: setup-go
setup-go: setup-env-go ## Complete setup for Go optimizer development
	@echo "✅ Go optimizer setup complete!"
	@echo "💡 Tip: Run 'source .env.local && make run' to start with Go optimizer"

.PHONY: setup-python  
setup-python: setup-env-python ## Complete setup for Python optimizer development
	@echo "✅ Python optimizer setup complete!"
	@echo "💡 Tip: Run 'source .env.local && make run' to start with Python optimizer"

.PHONY: build-and-deploy-go
build-and-deploy-go: docker-build-go docker-push deploy ## Build Go image and deploy
	@echo "✅ Go optimizer deployed successfully!"

.PHONY: build-and-deploy-full
build-and-deploy-full: docker-build-full docker-push deploy ## Build full image and deploy
	@echo "✅ Full optimizer deployed successfully!"

.PHONY: clean-env
clean-env: ## Clean up generated environment files
	@rm -f .env.local
	@echo "🧹 Cleaned up environment files"

.PHONY: status
status: ## Show current optimizer configuration
	@echo "🔧 Current Configuration:"
	@echo "  OPTIMIZER: $(OPTIMIZER)"
	@echo "  DOCKERFILE: $(DOCKERFILE)"
	@echo "  IMG: $(IMG)"
	@echo ""
	@if [ -f .env.local ]; then \
		echo "📋 Local Environment (.env.local):"; \
		cat .env.local | sed 's/^/  /'; \
	else \
		echo "📋 No local environment file found. Run 'make setup-go' or 'make setup-python'"; \
	fi

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager. Use OPTIMIZER=go for lightweight build.
	@echo "Building $(OPTIMIZER) optimizer image using $(DOCKERFILE)..."
	$(CONTAINER_TOOL) build -f $(DOCKERFILE) -t ${IMG} .

.PHONY: docker-build-go
docker-build-go: ## Build lightweight Go-only optimizer image (~20MB)
	@echo "Building Go-only optimizer image..."
	$(CONTAINER_TOOL) build -f Dockerfile_GO -t ${IMG} .

.PHONY: docker-build-full
docker-build-full: ## Build full optimizer image with Go and Python support (~200MB)
	@echo "Building full optimizer image with Python support..."
	$(CONTAINER_TOOL) build -f Dockerfile -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	@echo "Building $(OPTIMIZER) optimizer for multiple platforms using $(DOCKERFILE)..."
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' $(DOCKERFILE) > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name inferno-autoscaler-builder
	$(CONTAINER_TOOL) buildx use inferno-autoscaler-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm inferno-autoscaler-builder
	rm Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.6.0
CONTROLLER_TOOLS_VERSION ?= v0.17.2
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v1.63.4

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
