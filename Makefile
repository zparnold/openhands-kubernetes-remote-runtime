.PHONY: build test clean docker-build docker-push deploy coverage test-verbose

# Variables
BINARY_NAME=runtime-api
DOCKER_IMAGE?=ghcr.io/zparnold/openhands-kubernetes-remote-runtime
VERSION?=latest
NAMESPACE?=openhands
COVERAGE_THRESHOLD?=25

# Build the Go binary
build:
	go build -o $(BINARY_NAME) ./cmd/runtime-api

# Run tests
test:
	go test -v ./...

# Run tests with verbose output
test-verbose:
	go test -v -race ./...

# Run tests with coverage
coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Check coverage threshold
coverage-check: coverage
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	if awk -v cov="$$COVERAGE" -v thr="$(COVERAGE_THRESHOLD)" 'BEGIN {exit !(cov < thr)}'; then \
		echo "Coverage $$COVERAGE% is below threshold $(COVERAGE_THRESHOLD)%"; \
		exit 1; \
	else \
		echo "Coverage $$COVERAGE% meets threshold $(COVERAGE_THRESHOLD)%"; \
	fi

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	go clean

# Build Docker image
docker-build: build
	docker buildx build --platform linux/amd64 -t $(DOCKER_IMAGE):$(VERSION) .
	docker buildx build --platform linux/amd64 --push -t $(DOCKER_IMAGE):$(VERSION) .

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

# Pre-commit checks (run before committing)
pre-commit: fmt lint test coverage-check
	@echo "Pre-commit checks passed!"
