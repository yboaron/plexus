CONTROLLER_GEN ?= go tool controller-gen
GOLANGCI_LINT ?= golangci-lint

IMG ?= plexus-controller:latest

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Code Generation

.PHONY: generate
generate: ## Generate deepcopy methods, CRD manifests, RBAC, and sync to Helm chart
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd
	$(CONTROLLER_GEN) rbac:roleName=plexus-controller paths="./internal/controller/..." output:rbac:artifacts:config=config/rbac
	cp config/crd/*.yaml helm/plexus/crds/
	@{ \
		printf '# Auto-generated from +kubebuilder:rbac markers. Run "make generate" to update.\n'; \
		printf 'apiVersion: rbac.authorization.k8s.io/v1\n'; \
		printf 'kind: ClusterRole\n'; \
		printf 'metadata:\n'; \
		printf '  name: {{ include "plexus.name" . }}\n'; \
		printf '  labels:\n'; \
		printf '    {{- include "plexus.labels" . | nindent 4 }}\n'; \
		sed -n '/^rules:/,$$p' config/rbac/role.yaml; \
	} > helm/plexus/templates/clusterrole.yaml

.PHONY: manifests
manifests: ## Generate CRD manifests only
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd

.PHONY: verify-codegen
verify-codegen: generate ## Verify generated files are up to date
	@if [ -n "$$(git status --porcelain api/ config/crd/ config/rbac/ helm/plexus/crds/ helm/plexus/templates/clusterrole.yaml)" ]; then \
		echo "ERROR: generated files are out of date. Run 'make generate' and commit the result."; \
		git diff api/ config/crd/ config/rbac/ helm/plexus/crds/ helm/plexus/templates/clusterrole.yaml; \
		exit 1; \
	fi

ENVTEST_K8S_VERSION ?= 1.33.x
ENVTEST_ASSETS_DIR  ?= $(shell pwd)/bin/k8s

##@ Development

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: fmt vet ## Run linters
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-api
lint-api: ## Run kube-api-linter on API types
	@if [ ! -f bin/golangci-lint-kube-api-linter ]; then \
		echo "Building kube-api-linter..."; \
		$(GOLANGCI_LINT) custom --custom-gcl-config hack/lint/.custom-gcl.yml; \
	fi
	bin/golangci-lint-kube-api-linter run --config hack/lint/.golangci-api.yml ./api/...

.PHONY: test
test: ## Run all tests. Downloads envtest kube-apiserver/etcd binaries on first run.
	KUBEBUILDER_ASSETS="$$(go tool setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path)" \
	go test ./... -coverprofile cover.out -race -v

.PHONY: test-unit
test-unit: ## Run pure unit tests only (no API server, no envtest download)
	go test ./internal/backend/... -coverprofile cover.out -race -v

.PHONY: update-ovnk-crds
update-ovnk-crds: ## Generate OVN-k CRD YAMLs for envtest from the go.mod-pinned module
	hack/update-ovnk-crds.sh

##@ Build

.PHONY: build
build: ## Build controller and CLI binaries
	go build -o bin/plexus-controller ./cmd/controller/
	go build -o bin/kubectl-plexus ./cmd/kubectl-plexus/
	ln -sf kubectl-plexus bin/plexus

.PHONY: run
run: ## Run controller locally against the configured cluster
	go run ./cmd/controller/

.PHONY: docker-build
docker-build: ## Build container image
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push container image
	docker push $(IMG)

##@ Deployment

.PHONY: install
install: manifests ## Install CRDs into the configured cluster
	kubectl apply -f config/crd/

.PHONY: uninstall
uninstall: ## Uninstall CRDs from the configured cluster
	kubectl delete -f config/crd/

.PHONY: deploy
deploy: manifests ## Deploy controller to the configured cluster (raw manifests)
	kubectl apply -f config/manager/
	kubectl apply -f config/rbac/

.PHONY: undeploy
undeploy: ## Undeploy controller from the configured cluster (raw manifests)
	kubectl delete -f config/manager/
	kubectl delete -f config/rbac/

.PHONY: helm-install
helm-install: manifests ## Install Plexus via Helm
	helm upgrade --install plexus helm/plexus/

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall Plexus via Helm
	helm uninstall plexus

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/ cover.out

##@ KIND Clusters

OVN_KUBERNETES_PATH ?= $(shell cd ../ovn-kubernetes 2>/dev/null && pwd)

.PHONY: kind
kind: ## Create a single-cluster Plexus dev environment with EVPN
	OVN_KUBERNETES_PATH=$(OVN_KUBERNETES_PATH) contrib/plexus-kind.sh

.PHONY: kind-multi
kind-multi: ## Create a multi-cluster (hub + 1 spoke) Plexus dev environment
	OVN_KUBERNETES_PATH=$(OVN_KUBERNETES_PATH) contrib/plexus-kind-multi.sh --spokes 1

.PHONY: kind-delete
kind-delete: ## Tear down all Plexus KIND clusters
	@contrib/plexus-kind-multi.sh --delete 2>/dev/null; contrib/plexus-kind.sh --delete 2>/dev/null; true
