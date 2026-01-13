.PHONY: build run test docker-build docker-push k8s-deploy helm-install helm-upgrade helm-uninstall helm-lint helm-template helm-values helm-package monitoring-deploy clean

# Variables
APP_NAME=osm-device-adapter
DOCKER_REGISTRY?=k8s.localdev:32000
DOCKER_TAG?=latest
IMAGE=$(DOCKER_REGISTRY)/$(APP_NAME):$(DOCKER_TAG)
HELM_RELEASE?=osm-device-adapter
HELM_NAMESPACE?=osm-adapter
MONITORING_NAMESPACE?=monitoring

# Build the Go application
build:
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
# Helm: Install the chart
helm-install:
	helm install $(HELM_RELEASE) ./chart \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace

# Helm: Upgrade the chart
helm-upgrade:
	helm upgrade $(HELM_RELEASE) ./chart \
		--namespace $(HELM_NAMESPACE) \
		--install

# Helm: Uninstall the chart
helm-uninstall:
	helm uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

# Helm: Lint the chart
helm-lint:
	helm lint ./chart

# Helm: Package the chart
helm-package:
	helm package ./chart

# Helm: Template the chart (dry-run)
helm-template:
	helm template $(HELM_RELEASE) ./chart --namespace $(HELM_NAMESPACE)

# Helm: Show values
helm-values:
	helm show values ./chart

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
