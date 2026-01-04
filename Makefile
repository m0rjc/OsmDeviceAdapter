.PHONY: build run test docker-build docker-push k8s-deploy clean

# Variables
APP_NAME=osm-device-adapter
DOCKER_REGISTRY?=your-registry
DOCKER_TAG?=latest
IMAGE=$(DOCKER_REGISTRY)/$(APP_NAME):$(DOCKER_TAG)

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
k8s-deploy:
	kubectl apply -f deployments/k8s/configmap.yaml
	kubectl apply -f deployments/k8s/secret.yaml
	kubectl apply -f deployments/k8s/deployment.yaml
	kubectl apply -f deployments/k8s/service.yaml
	kubectl apply -f deployments/k8s/ingress.yaml

# Delete from Kubernetes
k8s-delete:
	kubectl delete -f deployments/k8s/ingress.yaml
	kubectl delete -f deployments/k8s/service.yaml
	kubectl delete -f deployments/k8s/deployment.yaml
	kubectl delete -f deployments/k8s/secret.yaml
	kubectl delete -f deployments/k8s/configmap.yaml

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
