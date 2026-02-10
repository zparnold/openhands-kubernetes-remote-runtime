# Agent Context for OpenHands Kubernetes Remote Runtime

This document provides context for AI agents and contributors working on the OpenHands Kubernetes Remote Runtime project.

## Project Overview

The OpenHands Kubernetes Remote Runtime is a Go-based service that provisions sandbox pods in Kubernetes for OpenHands agent sessions. It implements the OpenHands Remote Runtime API contract and provides subdomain-based routing for agent servers, VSCode, and worker processes.

## Architecture

### Core Components

1. **API Server** (`pkg/api/handler.go`)
   - HTTP REST API with 11 endpoints
   - Authentication via X-API-Key header
   - Request validation and error handling
   - Structured JSON responses

2. **Kubernetes Client** (`pkg/k8s/client.go`)
   - Pod provisioning with resource limits
   - Service creation for port exposure
   - Ingress creation with subdomain routing
   - Pod status monitoring and health checks
   - **Note**: Currently has 0% test coverage due to lack of mocking infrastructure

3. **State Management** (`pkg/state/state.go`)
   - In-memory runtime state tracking
   - Thread-safe operations with mutex locks
   - Efficient lookups by runtime ID and session ID
   - **Test Coverage**: 100%

4. **Configuration** (`pkg/config/config.go`)
   - Environment-based configuration
   - Sensible defaults for all settings
   - **Test Coverage**: 100%

5. **Type Definitions** (`pkg/types/types.go`)
   - Request/response models
   - Status enumerations
   - Shared data structures

## API Contract

The service implements the exact API contract expected by OpenHands Remote Runtime:

### Key Endpoints

- `POST /start` - Start new runtime sandbox
- `POST /stop` - Stop running runtime
- `POST /pause` - Pause runtime (delete pod, keep state)
- `POST /resume` - Resume paused runtime (recreate pod)
- `GET /list` - List all runtimes
- `GET /runtime/{runtime_id}` - Get runtime details
- `GET /sessions/{session_id}` - Get session by ID
- `GET /sessions/batch` - Batch query sessions
- `GET /registry_prefix` - Get container registry prefix
- `GET /image_exists` - Check if image exists
- `GET /health` - Health check endpoint (no auth required)
- `GET /liveness` - Liveness probe endpoint (no auth required)
- `GET /readiness` - Readiness probe endpoint (no auth required)

### Response Format

All responses follow this structure:

```go
type RuntimeResponse struct {
    RuntimeID      string            `json:"runtime_id"`
    SessionID      string            `json:"session_id"`
    URL            string            `json:"url"`
    SessionAPIKey  string            `json:"session_api_key"`
    Status         RuntimeStatus     `json:"status"`
    PodStatus      PodStatus         `json:"pod_status"`
    WorkHosts      map[string]int    `json:"work_hosts"`
    RestartCount   int               `json:"restart_count,omitempty"`
    RestartReasons []string          `json:"restart_reasons,omitempty"`
}
```

## Subdomain Routing

Each sandbox gets unique subdomains:
- `{session-id}.sandbox.example.com` → Agent server (port 60000)
- `vscode-{session-id}.sandbox.example.com` → VSCode (port 60001)
- `work-1-{session-id}.sandbox.example.com` → Worker 1 (port 12000)
- `work-2-{session-id}.sandbox.example.com` → Worker 2 (port 12001)

## Pod Specification

Each sandbox pod includes:
- Agent server container with OpenHands runtime image
- 4 exposed ports (agent, vscode, worker1, worker2)
- Environment variables for session API key, webhooks, CORS
- Resource requests and limits (configurable via resource_factor)
- Readiness probe on /alive endpoint
- Support for custom runtime classes (sysbox-runc, gvisor)
- Optional imagePullSecrets when `IMAGE_PULL_SECRETS` is set (for private registries)
- Optional CA cert mount when `CA_CERT_SECRET_NAME` is set (for corporate/proxy CAs); cert is mounted at `/usr/local/share/ca-certificates/additional-ca.crt` and merged via `update-ca-certificates` at runtime startup

## Development Guidelines

### Adding New Features

1. **Follow existing patterns**: Look at similar functionality in the codebase
2. **Add tests**: Maintain or improve test coverage (current: 26.5%)
3. **Update documentation**: Keep README.md, CONTRIBUTING.md, and this file in sync
4. **Run pre-commit checks**: `make pre-commit` before committing

### Testing Strategy

- **Unit tests**: Focus on business logic and state management
- **Table-driven tests**: Preferred pattern for multiple test cases
- **Mocking**: Currently needed for Kubernetes client testing (future work)
- **Coverage threshold**: Minimum 25% (enforced by CI)

### Current Test Coverage

| Package | Coverage | Priority |
|---------|----------|----------|
| pkg/config | 100% | ✅ Complete |
| pkg/state | 100% | ✅ Complete |
| pkg/api | ~28% | ⚠️ Needs improvement |
| pkg/types | N/A | N/A |
| pkg/k8s | 0% | ⚠️ High priority - needs mocking |

### Known Limitations

1. **In-memory state**: State is not persisted. In production, consider using a database (PostgreSQL/Redis)
2. **No Kubernetes mocking**: The `pkg/k8s` package lacks tests due to complexity of mocking Kubernetes client
3. **Resume functionality**: Stores minimal info, may not perfectly recreate original pod spec
4. **Image validation**: `/image_exists` endpoint currently returns true for all images (placeholder)

## CI/CD Pipeline

### Automated Checks

All PRs must pass:
- ✅ Linting (golangci-lint with 20+ rules)
- ✅ Unit tests with race detection
- ✅ Coverage threshold (minimum 25%)
- ✅ Build verification
- ✅ Security scan (gosec)
- ✅ Code formatting (gofmt)
- ✅ Dependency verification

### Workflow Files

- `.github/workflows/ci.yml` - Main CI pipeline
- `.github/workflows/pr-checks.yml` - Additional PR validation

## Common Tasks

### Running Tests

```bash
make test              # Run all tests
make test-verbose      # Tests with race detection
make coverage          # Generate coverage report
make coverage-check    # Verify 25% threshold
```

### Building

```bash
make build             # Build binary
make docker-build      # Build Docker image
```

### Linting

```bash
make fmt               # Format code
make lint              # Run linter
make pre-commit        # All checks before commit
```

## Integration Points

### OpenHands Integration

The service integrates with OpenHands by:
1. Receiving session creation requests from OpenHands app server
2. Provisioning Kubernetes resources (Pod, Service, Ingress)
3. Returning URLs and session API keys to OpenHands
4. Managing lifecycle (pause/resume/stop) of runtime sessions

### Required Configuration

OpenHands must be configured with:
```toml
[sandbox]
type = "remote"
api_key = "your-api-key"
remote_runtime_api_url = "https://runtime-api.your-domain.com"
runtime_container_image = "ghcr.io/openhands/runtime:latest"
```

## Security Considerations

1. **API Key Authentication**: All endpoints (except /health, /liveness, /readiness) require X-API-Key header
2. **Session API Keys**: Generated per sandbox using crypto/rand with fallback
3. **RBAC**: Minimal Kubernetes permissions (pods, services, ingresses in namespace)
4. **Runtime Classes**: Support for gvisor or sysbox for additional isolation
5. **No known vulnerabilities**: CodeQL analysis shows 0 issues

## Future Enhancements

Potential improvements (not required for current functionality):

1. **Persistent state storage**: Replace in-memory state with database
2. **Kubernetes mocking**: Add test infrastructure for pkg/k8s
3. **Metrics**: Add Prometheus metrics for monitoring
4. **Autoscaling**: Implement HPA for API and sandbox pods
5. **Pod cleanup**: TTL-based cleanup of old pods
6. **Image validation**: Actually check container registry
7. **Enhanced resume**: Store full pod spec for exact recreation
8. **Integration tests**: Test against real Kubernetes cluster

## Troubleshooting

### Common Issues

1. **Tests failing**: Run `go test -v ./...` to see detailed output
2. **Coverage below threshold**: Generate report with `make coverage` and open `coverage.html`
3. **Linter errors**: Check `.golangci.yml` for rules and fix or document exceptions
4. **Kubernetes errors**: Check pod logs with `kubectl logs -n openhands <pod-name>`

## Resources

- **OpenHands Remote Runtime API**: [remote_runtime.py](https://github.com/OpenHands/OpenHands/blob/main/openhands/runtime/impl/remote/remote_runtime.py)
- **Docker Runtime Reference**: [docker_runtime.py](https://github.com/OpenHands/OpenHands/blob/main/openhands/runtime/impl/docker/docker_runtime.py)
- **Testing Guide**: See TESTING.md
- **Contributing Guide**: See CONTRIBUTING.md
- **Deployment Guide**: See DEPLOYMENT.md

## Contact

For questions or issues:
- Check existing documentation (README.md, CONTRIBUTING.md, TESTING.md)
- Review this AGENTS.md file
- Look at similar code patterns in the repository
- Open an issue for discussion

---

**Last Updated**: 2026-02-04
**Version**: 1.0.0
