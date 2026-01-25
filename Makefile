.PHONY: build build-server run test check-main-branch docker-build docker-push docker-build-dev docker-push-dev k8s-deploy helm-install helm-upgrade helm-uninstall helm-install-dev helm-upgrade-dev helm-uninstall-dev helm-template-dev helm-lint helm-template helm-values helm-package helm-secrets-install helm-secrets-upgrade helm-secrets-install-dev helm-secrets-upgrade-dev helm-secrets-uninstall-dev helm-secrets-lint helm-secrets-template monitoring-deploy clean ui-build ui-dev ui-clean

# Variables - Production (Live)
APP_NAME=osm-device-adapter
DOCKER_REGISTRY?=k8s.localdev:32000
DOCKER_TAG?=latest
IMAGE=$(DOCKER_REGISTRY)/$(APP_NAME):$(DOCKER_TAG)
HELM_RELEASE?=osm-device-adapter
HELM_SECRETS_RELEASE?=osm-secrets
HELM_NAMESPACE?=osm-adapter
MONITORING_NAMESPACE?=monitoring
CHART_DIR=./charts/osm-device-adapter
SECRETS_CHART_DIR=./charts/osm-secrets
VALUES_FILE?=$(CHART_DIR)/values-live.yaml
SECRETS_VALUES_FILE?=$(SECRETS_CHART_DIR)/values-live.yaml

# Variables - Development
DOCKER_TAG_DEV?=dev
IMAGE_DEV=$(DOCKER_REGISTRY)/$(APP_NAME):$(DOCKER_TAG_DEV)
HELM_RELEASE_DEV?=osm-device-adapter-dev
HELM_SECRETS_RELEASE_DEV?=osm-secrets-dev
HELM_NAMESPACE_DEV?=osm-adapter-dev
VALUES_FILE_DEV?=$(CHART_DIR)/values-dev.yaml
SECRETS_VALUES_FILE_DEV?=$(SECRETS_CHART_DIR)/values-dev.yaml

# Safety check: Ensure we're on main branch for live deployments
check-main-branch:
	@CURRENT_BRANCH=$$(git rev-parse --abbrev-ref HEAD); \
	if [ "$$CURRENT_BRANCH" != "main" ] && [ "$$CURRENT_BRANCH" != "master" ]; then \
		echo "Error: Live deployments must be from main/master branch"; \
		echo "Current branch: $$CURRENT_BRANCH"; \
		echo "Please checkout main branch or use dev targets (e.g., make helm-upgrade-dev)"; \
		exit 1; \
	fi

# Build the full application (frontend + backend)
build: ui-build
	go build -o bin/server ./cmd/server

# Build only the Go backend (faster for backend-only changes)
build-server:
	go build -o bin/server ./cmd/server

# Run the application locally
run:
	go run ./cmd/server

# Run tests
test:
	go test -v ./...

# Build Docker image
docker-build:
	docker build -t $(IMAGE) .

# Push Docker image to registry
docker-push: check-main-branch docker-build
	docker push $(IMAGE)

# Build Docker image for dev
docker-build-dev:
	docker build -t $(IMAGE_DEV) .

# Push Docker image to registry (dev)
docker-push-dev: docker-build-dev
	docker push $(IMAGE_DEV)

# Deploy to Kubernetes
# Helm: Install the main application chart
helm-install: check-main-branch
	@if [ -f "$(VALUES_FILE)" ]; then \
		echo "Using values file: $(VALUES_FILE)"; \
		helm install $(HELM_RELEASE) $(CHART_DIR) \
			-f $(VALUES_FILE) \
			--namespace $(HELM_NAMESPACE) \
			--create-namespace; \
	else \
		echo "Error: Values file not found: $(VALUES_FILE)"; \
		echo "Usage: make helm-install VALUES_FILE=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm: Upgrade the main application chart
helm-upgrade: check-main-branch
	@if [ -f "$(VALUES_FILE)" ]; then \
		echo "Using values file: $(VALUES_FILE)"; \
		helm upgrade $(HELM_RELEASE) $(CHART_DIR) \
			-f $(VALUES_FILE) \
			--namespace $(HELM_NAMESPACE) \
			--install; \
	else \
		echo "Error: Values file not found: $(VALUES_FILE)"; \
		echo "Usage: make helm-upgrade VALUES_FILE=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm: Uninstall the main application chart
helm-uninstall:
	helm uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

# Helm Dev: Install the main application chart (dev)
helm-install-dev:
	@if [ -f "$(VALUES_FILE_DEV)" ]; then \
		echo "Using dev values file: $(VALUES_FILE_DEV)"; \
		helm install $(HELM_RELEASE_DEV) $(CHART_DIR) \
			-f $(VALUES_FILE_DEV) \
			--namespace $(HELM_NAMESPACE_DEV) \
			--create-namespace; \
	else \
		echo "Error: Dev values file not found: $(VALUES_FILE_DEV)"; \
		echo "Usage: make helm-install-dev VALUES_FILE_DEV=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm Dev: Upgrade the main application chart (dev)
helm-upgrade-dev:
	@if [ -f "$(VALUES_FILE_DEV)" ]; then \
		echo "Using dev values file: $(VALUES_FILE_DEV)"; \
		helm upgrade $(HELM_RELEASE_DEV) $(CHART_DIR) \
			-f $(VALUES_FILE_DEV) \
			--namespace $(HELM_NAMESPACE_DEV) \
			--install; \
	else \
		echo "Error: Dev values file not found: $(VALUES_FILE_DEV)"; \
		echo "Usage: make helm-upgrade-dev VALUES_FILE_DEV=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm Dev: Uninstall the main application chart (dev)
helm-uninstall-dev:
	helm uninstall $(HELM_RELEASE_DEV) --namespace $(HELM_NAMESPACE_DEV)

# Helm Dev: Template the main application chart (dev, dry-run)
helm-template-dev:
	@if [ -f "$(VALUES_FILE_DEV)" ]; then \
		echo "Using dev values file: $(VALUES_FILE_DEV)"; \
		helm template $(HELM_RELEASE_DEV) $(CHART_DIR) \
			-f $(VALUES_FILE_DEV) \
			--namespace $(HELM_NAMESPACE_DEV); \
	else \
		echo "Error: Dev values file not found: $(VALUES_FILE_DEV)"; \
		echo "Usage: make helm-template-dev VALUES_FILE_DEV=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm: Lint the main application chart
helm-lint:
	helm lint $(CHART_DIR)

# Helm: Package the main application chart
helm-package:
	helm package $(CHART_DIR)

# Helm: Template the main application chart (dry-run)
helm-template:
	@if [ -f "$(VALUES_FILE)" ]; then \
		echo "Using values file: $(VALUES_FILE)"; \
		helm template $(HELM_RELEASE) $(CHART_DIR) \
			-f $(VALUES_FILE) \
			--namespace $(HELM_NAMESPACE); \
	else \
		echo "Error: Values file not found: $(VALUES_FILE)"; \
		echo "Usage: make helm-template VALUES_FILE=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm: Show main application chart values
helm-values:
	helm show values $(CHART_DIR)

# Helm Secrets: Install the secrets chart
helm-secrets-install: check-main-branch
	@if [ -f "$(SECRETS_VALUES_FILE)" ]; then \
		echo "Using secrets values file: $(SECRETS_VALUES_FILE)"; \
		helm install $(HELM_SECRETS_RELEASE) $(SECRETS_CHART_DIR) \
			-f $(SECRETS_VALUES_FILE) \
			--namespace $(HELM_NAMESPACE) \
			--create-namespace; \
	else \
		echo "Error: Secrets values file not found: $(SECRETS_VALUES_FILE)"; \
		echo "Create a values file with your secrets first"; \
		echo "Usage: make helm-secrets-install SECRETS_VALUES_FILE=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm Secrets: Upgrade the secrets chart
helm-secrets-upgrade: check-main-branch
	@if [ -f "$(SECRETS_VALUES_FILE)" ]; then \
		echo "Using secrets values file: $(SECRETS_VALUES_FILE)"; \
		helm upgrade $(HELM_SECRETS_RELEASE) $(SECRETS_CHART_DIR) \
			-f $(SECRETS_VALUES_FILE) \
			--namespace $(HELM_NAMESPACE) \
			--install; \
	else \
		echo "Error: Secrets values file not found: $(SECRETS_VALUES_FILE)"; \
		echo "Usage: make helm-secrets-upgrade SECRETS_VALUES_FILE=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm Secrets Dev: Install the secrets chart (dev)
helm-secrets-install-dev:
	@if [ -f "$(SECRETS_VALUES_FILE_DEV)" ]; then \
		echo "Using dev secrets values file: $(SECRETS_VALUES_FILE_DEV)"; \
		helm install $(HELM_SECRETS_RELEASE_DEV) $(SECRETS_CHART_DIR) \
			-f $(SECRETS_VALUES_FILE_DEV) \
			--namespace $(HELM_NAMESPACE_DEV) \
			--create-namespace; \
	else \
		echo "Error: Dev secrets values file not found: $(SECRETS_VALUES_FILE_DEV)"; \
		echo "Create a dev values file with your secrets first"; \
		echo "Usage: make helm-secrets-install-dev SECRETS_VALUES_FILE_DEV=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm Secrets Dev: Upgrade the secrets chart (dev)
helm-secrets-upgrade-dev:
	@if [ -f "$(SECRETS_VALUES_FILE_DEV)" ]; then \
		echo "Using dev secrets values file: $(SECRETS_VALUES_FILE_DEV)"; \
		helm upgrade $(HELM_SECRETS_RELEASE_DEV) $(SECRETS_CHART_DIR) \
			-f $(SECRETS_VALUES_FILE_DEV) \
			--namespace $(HELM_NAMESPACE_DEV) \
			--install; \
	else \
		echo "Error: Dev secrets values file not found: $(SECRETS_VALUES_FILE_DEV)"; \
		echo "Usage: make helm-secrets-upgrade-dev SECRETS_VALUES_FILE_DEV=path/to/values.yaml"; \
		exit 1; \
	fi

# Helm Secrets Dev: Uninstall the secrets chart (dev)
helm-secrets-uninstall-dev:
	helm uninstall $(HELM_SECRETS_RELEASE_DEV) --namespace $(HELM_NAMESPACE_DEV)

# Helm Secrets: Lint the secrets chart
helm-secrets-lint:
	helm lint $(SECRETS_CHART_DIR)

# Helm Secrets: Template the secrets chart (for testing)
helm-secrets-template:
	@if [ -z "$(SECRETS_VALUES_FILE)" ]; then \
		echo "Using example values for template rendering"; \
		helm template $(HELM_SECRETS_RELEASE) $(SECRETS_CHART_DIR) \
			-f $(SECRETS_CHART_DIR)/values-example.yaml \
			--namespace $(HELM_NAMESPACE); \
	else \
		helm template $(HELM_SECRETS_RELEASE) $(SECRETS_CHART_DIR) \
			-f $(SECRETS_VALUES_FILE) \
			--namespace $(HELM_NAMESPACE); \
	fi

# Deploy/upgrade Prometheus monitoring stack
monitoring-deploy:
	helm upgrade monitoring prometheus-community/kube-prometheus-stack \
		--namespace $(MONITORING_NAMESPACE) \
		--create-namespace \
		--install \
		-f k8s/monitoring/kube-prometheus-stack-values.yaml

# Clean build artifacts
clean:
	rm -rf bin/

# Install dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Frontend build targets

# Build the admin SPA frontend
ui-build:
	cd web/admin && npm ci && npm run build

# Run the frontend dev server (hot reload at localhost:5173)
ui-dev:
	cd web/admin && npm run dev

# Clean frontend build artifacts
ui-clean:
	rm -rf web/admin/dist web/admin/node_modules
