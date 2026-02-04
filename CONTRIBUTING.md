# Contributing to OpenHands Kubernetes Remote Runtime

Thank you for your interest in contributing! This document provides guidelines for contributing to the project.

## Development Setup

### Prerequisites

- Go 1.21 or higher
- Docker
- Kubernetes cluster (local or remote)
- kubectl configured

### Local Development

1. **Clone the repository**:
```bash
git clone https://github.com/zparnold/openhands-kubernetes-remote-runtime.git
cd openhands-kubernetes-remote-runtime
```

2. **Install dependencies**:
```bash
make deps
```

3. **Set up environment**:
```bash
cp .env.example .env
# Edit .env with your configuration
```

4. **Build the project**:
```bash
make build
```

5. **Run tests**:
```bash
make test
```

6. **Run locally** (requires kubeconfig):
```bash
export API_KEY=test-key
export BASE_DOMAIN=localhost
make run
```

## Project Structure

```
.
├── cmd/
│   └── runtime-api/      # Main application entry point
├── pkg/
│   ├── api/              # HTTP handlers and routing
│   ├── config/           # Configuration management
│   ├── k8s/              # Kubernetes client operations
│   ├── state/            # State management
│   └── types/            # Shared type definitions
├── k8s/                  # Kubernetes manifests
├── Dockerfile            # Container image definition
├── Makefile              # Build and deployment tasks
└── README.md             # Project documentation
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting: `make fmt`
- Run linter before committing: `make lint`
- Add comments for exported functions and types

## Testing

### Unit Tests

Write unit tests for new functionality:

```bash
# Run all tests
make test

# Run specific package tests
go test -v ./pkg/api
```

### Integration Tests

Test against a real Kubernetes cluster:

```bash
# Deploy to test cluster
make deploy

# Run manual tests
curl -H "X-API-Key: test-key" http://localhost:8080/health
```

## Submitting Changes

1. **Fork the repository**

2. **Create a feature branch**:
```bash
git checkout -b feature/your-feature-name
```

3. **Make your changes**:
   - Write clear, concise commit messages
   - Include tests for new functionality
   - Update documentation as needed

4. **Test your changes**:
```bash
make test
make build
```

5. **Commit your changes**:
```bash
git commit -m "Add feature: description"
```

6. **Push to your fork**:
```bash
git push origin feature/your-feature-name
```

7. **Create a Pull Request**:
   - Provide a clear description of the changes
   - Reference any related issues
   - Ensure CI checks pass

## Pull Request Guidelines

- Keep PRs focused on a single feature or fix
- Update README.md if adding new features
- Add or update tests as needed
- Ensure all tests pass
- Update DEPLOYMENT.md if changing deployment process

## Reporting Issues

When reporting issues, please include:

- Go version (`go version`)
- Kubernetes version (`kubectl version`)
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs or error messages

## Feature Requests

Feature requests are welcome! Please:

- Check if the feature already exists or is planned
- Provide a clear use case
- Describe the expected behavior
- Consider submitting a PR if you can implement it

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.
