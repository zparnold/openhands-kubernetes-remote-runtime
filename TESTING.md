# Testing Guide

This document provides comprehensive information about testing in the OpenHands Kubernetes Remote Runtime project.

## Overview

The project uses Go's built-in testing framework with additional tools for coverage reporting and linting. We maintain a minimum coverage threshold of 25% to ensure code quality.

## Test Structure

### Test Files

All test files follow the Go convention of `*_test.go` and are located in the same package as the code they test:

```
pkg/
├── api/
│   ├── handler.go
│   └── handler_test.go       # Tests for API handlers
├── config/
│   ├── config.go
│   └── config_test.go        # Tests for configuration
├── state/
│   ├── state.go
│   └── state_test.go         # Tests for state management
└── types/
    ├── types.go
    └── types_test.go         # Tests for type definitions
```

### Current Coverage

| Package | Coverage | Status |
|---------|----------|--------|
| pkg/config | 100% | ✅ |
| pkg/state | 100% | ✅ |
| pkg/api | ~28% | ⚠️ |
| pkg/types | N/A | N/A |
| pkg/k8s | 0% | ⚠️ |
| **Overall** | **26.5%** | ✅ |

## Running Tests

### Basic Test Commands

```bash
# Run all tests
make test

# Run tests with verbose output
make test-verbose

# Run tests with race detection
go test -v -race ./...

# Run tests for a specific package
go test -v ./pkg/api
go test -v ./pkg/state
```

### Coverage Commands

```bash
# Generate coverage report
make coverage

# Check if coverage meets threshold (25%)
make coverage-check

# View coverage in browser
make coverage
open coverage.html

# Get coverage for specific package
go test -coverprofile=coverage.out ./pkg/state
go tool cover -func=coverage.out
```

### Pre-commit Checks

Run all checks before committing:

```bash
make pre-commit
```

This runs:
1. `go fmt` - Format code
2. `golangci-lint` - Run linter
3. `go test` - Run all tests
4. Coverage check - Verify 25% threshold

## Writing Tests

### Test Naming Conventions

```go
// Test function names should start with Test
func TestFeatureName(t *testing.T) {}

// Use descriptive names for table-driven tests
func TestFeature_SpecificScenario(t *testing.T) {}

// Subtests use t.Run with descriptive names
t.Run("specific case description", func(t *testing.T) {})
```

### Table-Driven Tests

Preferred pattern for multiple test cases:

```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {
            name:     "valid input",
            input:    "test",
            expected: "result",
            wantErr:  false,
        },
        {
            name:     "invalid input",
            input:    "",
            expected: "",
            wantErr:  true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Feature(tt.input)
            
            if tt.wantErr && err == nil {
                t.Error("expected error but got none")
            }
            
            if !tt.wantErr && err != nil {
                t.Errorf("unexpected error: %v", err)
            }
            
            if result != tt.expected {
                t.Errorf("expected %s, got %s", tt.expected, result)
            }
        })
    }
}
```

### Testing HTTP Handlers

Example from `pkg/api/handler_test.go`:

```go
func TestAuthMiddleware(t *testing.T) {
    handler, _ := setupTestHandler()

    t.Run("Valid API key", func(t *testing.T) {
        req := httptest.NewRequest("GET", "/test", nil)
        req.Header.Set("X-API-Key", "test-api-key")
        rr := httptest.NewRecorder()
        
        nextCalled := false
        next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            nextCalled = true
        })
        
        handler.AuthMiddleware(next).ServeHTTP(rr, req)
        
        if !nextCalled {
            t.Error("Expected next handler to be called")
        }
    })
}
```

### Environment Variable Testing

Restore environment variables after tests:

```go
func TestConfig(t *testing.T) {
    // Save original
    orig := os.Getenv("VAR")
    defer os.Setenv("VAR", orig)
    
    // Set test value
    os.Setenv("VAR", "test-value")
    
    // Run test
    // ...
}
```

## Continuous Integration

### GitHub Actions Workflows

Two workflows run automatically:

#### 1. CI Workflow (`.github/workflows/ci.yml`)

Triggers on:
- Push to `main` or `develop` branches
- Pull requests to `main` or `develop`

Jobs:
- **Lint**: Runs golangci-lint
- **Test**: Runs tests with race detection and coverage
- **Build**: Verifies binary builds

#### 2. PR Checks (`.github/workflows/pr-checks.yml`)

Additional checks for pull requests:
- **Formatting**: Verifies code is formatted
- **Dependencies**: Checks go.mod/go.sum integrity
- **Security**: Runs gosec security scanner
- **PR Title**: Validates conventional commit format

### Coverage Threshold

The CI pipeline enforces a minimum coverage threshold of 25%. If coverage falls below this, the build fails.

To check coverage locally:

```bash
make coverage-check
```

## Linting

### golangci-lint Configuration

The project uses golangci-lint with configuration in `.golangci.yml`.

Enabled linters:
- gofmt - Code formatting
- govet - Static analysis
- errcheck - Error checking
- staticcheck - Additional static checks
- gosec - Security issues
- gocyclo - Cyclomatic complexity
- dupl - Code duplication
- goconst - Repeated constants
- misspell - Spelling
- and more...

### Running the Linter

```bash
# Run linter
make lint

# Or directly with golangci-lint
golangci-lint run
```

### Common Linter Issues

1. **Unused variables**: Remove or use with `_`
2. **Error not checked**: Always check error returns
3. **Cyclomatic complexity**: Break down complex functions
4. **Code duplication**: Extract common code to functions

## Best Practices

### DO

✅ Write tests for new features
✅ Use table-driven tests for multiple cases
✅ Test error conditions
✅ Use descriptive test names
✅ Clean up resources in tests (defer)
✅ Run tests before committing
✅ Check coverage for new code
✅ Use `t.Helper()` for test helper functions

### DON'T

❌ Skip error checking in tests
❌ Use sleep for timing (use channels/waitgroups)
❌ Test implementation details
❌ Write tests without assertions
❌ Commit without running tests
❌ Ignore linter warnings

## Troubleshooting

### Tests Failing

1. Check error messages carefully
2. Run with verbose output: `go test -v`
3. Run specific test: `go test -v -run TestName`
4. Check for race conditions: `go test -race`

### Coverage Too Low

1. Identify uncovered code: `make coverage`
2. Open `coverage.html` in browser
3. Write tests for uncovered paths
4. Focus on critical code paths first

### Linter Errors

1. Read the error message and suggested fix
2. Check `.golangci.yml` for disabled rules
3. Fix or add `//nolint:rule` comment if justified
4. Document why linter rule is disabled

## Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [golangci-lint](https://golangci-lint.run/)
- [Testify (assertion library)](https://github.com/stretchr/testify)

## Getting Help

If you have questions about testing:

1. Check this guide
2. Look at existing tests for examples
3. Review CONTRIBUTING.md
4. Open an issue for discussion
