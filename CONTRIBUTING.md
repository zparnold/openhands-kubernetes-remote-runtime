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

Write unit tests for new functionality. All packages should have corresponding test files:

```bash
# Run all tests
make test

# Run tests with verbose output
make test-verbose

# Run tests with coverage
make coverage

# Run specific package tests
go test -v ./pkg/api
go test -v ./pkg/state
```

### Test Coverage

We maintain a minimum test coverage threshold of 25%. Check coverage with:

```bash
# Generate coverage report
make coverage

# Check if coverage meets threshold
make coverage-check

# View coverage in browser
make coverage
open coverage.html
```

Current coverage by package:
- `pkg/config`: 100% ✅
- `pkg/state`: 100% ✅
- `pkg/api`: ~28%
- `pkg/k8s`: 0% (requires Kubernetes mocking)
- Overall: ~26%

### Writing Tests

Follow these guidelines when writing tests:

1. **Test file naming**: `*_test.go` in the same package
2. **Test function naming**: `TestFunctionName` or `Test_DescriptiveScenario`
3. **Table-driven tests**: Use for multiple test cases
4. **Mocking**: Create mock implementations for external dependencies
5. **Coverage**: Aim for high coverage on new code

Example test structure:

```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"case 1", "input1", "expected1"},
        {"case 2", "input2", "expected2"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Feature(tt.input)
            if result != tt.expected {
                t.Errorf("Expected %s, got %s", tt.expected, result)
            }
        })
    }
}
```

### Pre-commit Checks

Before committing, run all pre-commit checks:

```bash
# Format, lint, test, and check coverage
make pre-commit
```

This will:
1. Format code with `gofmt`
2. Run linter (`golangci-lint`)
3. Run all tests
4. Verify coverage meets threshold

### Integration Tests

Test against a real Kubernetes cluster:

```bash
# Deploy to test cluster
make deploy

# Run manual tests
curl -H "X-API-Key: test-key" http://localhost:8080/health
```

### CI/CD Pipeline

All pull requests must pass:
- ✅ Linter checks (golangci-lint)
- ✅ Unit tests with race detection
- ✅ Build verification
- ✅ Coverage threshold (minimum 25%)

The CI pipeline runs automatically on:
- Push to `main` or `develop` branches
- Pull requests to `main` or `develop` branches

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
