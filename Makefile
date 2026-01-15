.PHONY: build build-server run test docker-build docker-push k8s-deploy helm-install helm-upgrade helm-uninstall helm-lint helm-template helm-values helm-package helm-secrets-install helm-secrets-upgrade helm-secrets-lint helm-secrets-template monitoring-deploy clean ui-build ui-dev ui-clean

# Variables
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
VALUES_FILE?=$(CHART_DIR)/values-dev.yaml
SECRETS_VALUES_FILE?=$(SECRETS_CHART_DIR)/values-dev.yaml

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
docker-push: docker-build
	docker push $(IMAGE)

# Deploy to Kubernetes
# Helm: Install the main application chart
helm-install:
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
helm-upgrade:
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
helm-secrets-install:
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
helm-secrets-upgrade:
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
