.PHONY: build test clean docker-build docker-push deploy

# Variables
BINARY_NAME=runtime-api
DOCKER_IMAGE?=ghcr.io/zparnold/openhands-kubernetes-remote-runtime
VERSION?=latest
NAMESPACE?=openhands

# Build the Go binary
build:
	go build -o $(BINARY_NAME) ./cmd/runtime-api

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	go clean

# Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):$(VERSION) .

# Push Docker image
docker-push:
	docker push $(DOCKER_IMAGE):$(VERSION)

# Deploy to Kubernetes
deploy:
	kubectl apply -f k8s/deployment.yaml

# Delete from Kubernetes
undeploy:
	kubectl delete -f k8s/deployment.yaml

# Run locally (requires kubeconfig)
run: build
	./$(BINARY_NAME)

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run

# Install dependencies
deps:
	go mod download
	go mod tidy

# Generate API documentation
docs:
	@echo "API Documentation available in README.md"
