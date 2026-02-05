# OpenHands Kubernetes Remote Runtime

[![CI](https://github.com/zparnold/openhands-kubernetes-remote-runtime/actions/workflows/ci.yml/badge.svg)](https://github.com/zparnold/openhands-kubernetes-remote-runtime/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/zparnold/openhands-kubernetes-remote-runtime)](https://goreportcard.com/report/github.com/zparnold/openhands-kubernetes-remote-runtime)
[![codecov](https://codecov.io/gh/zparnold/openhands-kubernetes-remote-runtime/branch/main/graph/badge.svg)](https://codecov.io/gh/zparnold/openhands-kubernetes-remote-runtime)

A Kubernetes-compatible runtime service for OpenHands that provisions sandbox pods for agent sessions. This service implements the OpenHands Remote Runtime API contract and supports Kubernetes versions 1.30+.

## Features

- ✅ Complete OpenHands Remote Runtime API implementation
- ✅ Subdomain-based routing for agent server, VSCode, and worker ports
- ✅ Kubernetes pod provisioning with proper resource management
- ✅ Session management with pause/resume capabilities
- ✅ API key authentication
- ✅ Support for custom runtime classes (sysbox-runc, gvisor)
- ✅ Structured logging and error handling
- ✅ Health checks and readiness probes

## Architecture

The service creates the following Kubernetes resources for each sandbox:

1. **Pod**: Runs the OpenHands agent server with exposed ports
   - Port 60000: Agent server
   - Port 60001: VSCode
   - Port 12000: Worker 1
   - Port 12001: Worker 2

2. **Service**: ClusterIP service to expose pod ports

3. **Ingress**: Subdomain-based routing for each port
   - `{session-id}.sandbox.example.com` → Agent server
   - `vscode-{session-id}.sandbox.example.com` → VSCode
   - `work-1-{session-id}.sandbox.example.com` → Worker 1
   - `work-2-{session-id}.sandbox.example.com` → Worker 2

## Prerequisites

- Kubernetes cluster version 1.30 or higher
- `kubectl` configured to access your cluster
- Ingress controller installed (e.g., nginx-ingress)
- Wildcard DNS configured for your base domain
- Container registry access for OpenHands runtime images

## Installation

### 1. Configure DNS

Set up wildcard DNS for your base domain. For example, if using `sandbox.example.com`:

```
*.sandbox.example.com → Your Ingress Controller IP
```

### 2. Update Configuration

Edit `k8s/deployment.yaml` and update the following:

```yaml
# In ConfigMap
BASE_DOMAIN: "your-domain.com"  # Change to your domain
REGISTRY_PREFIX: "your-registry/openhands"  # Change to your registry

# In Secret
API_KEY: "your-secure-api-key"  # Generate a secure key

# In Ingress
host: runtime-api.your-domain.com  # Change to your API endpoint
```

### 3. Deploy to Kubernetes

```bash
# Apply the manifests
kubectl apply -f k8s/deployment.yaml

# Verify deployment
kubectl get pods -n openhands
kubectl get svc -n openhands
kubectl get ingress -n openhands
```

### 4. Verify Installation

```bash
# Check if the API is running (no authentication required)
curl https://runtime-api.your-domain.com/health

# Get registry prefix
curl -H "X-API-Key: your-api-key" https://runtime-api.your-domain.com/registry_prefix
```

## API Endpoints

All endpoints require the `X-API-Key` header for authentication.

### POST /start
Start a new runtime sandbox.

**Request:**
```json
{
  "image": "ghcr.io/openhands/runtime:latest",
  "command": "/usr/local/bin/openhands-agent-server --port 60000",
  "working_dir": "/openhands/code/",
  "environment": {
    "DEBUG": "true"
  },
  "session_id": "abc123",
  "resource_factor": 1.0,
  "runtime_class": "sysbox-runc"
}
```

**Response:**
```json
{
  "runtime_id": "def456",
  "session_id": "abc123",
  "url": "https://abc123.sandbox.example.com",
  "session_api_key": "session-key-here",
  "status": "running",
  "pod_status": "ready",
  "work_hosts": {
    "https://work-1-abc123.sandbox.example.com": 12000,
    "https://work-2-abc123.sandbox.example.com": 12001
  }
}
```

### POST /stop
Stop a running runtime.

**Request:**
```json
{
  "runtime_id": "def456"
}
```

### POST /pause
Pause a running runtime (deletes pod, keeps state).

**Request:**
```json
{
  "runtime_id": "def456"
}
```

### POST /resume
Resume a paused runtime (recreates pod).

**Request:**
```json
{
  "runtime_id": "def456"
}
```

### GET /list
List all runtimes.

**Response:**
```json
{
  "runtimes": [...]
}
```

### GET /runtime/{runtime_id}
Get details of a specific runtime.

### GET /sessions/{session_id}
Get runtime by session ID.

### GET /sessions/batch?ids=session1,session2
Batch query multiple sessions.

### GET /registry_prefix
Get the container registry prefix.

**Response:**
```json
{
  "registry_prefix": "ghcr.io/openhands"
}
```

### GET /image_exists?image=ghcr.io/openhands/runtime:latest
Check if a container image exists.

**Response:**
```json
{
  "exists": true
}
```

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `8080` | HTTP server port |
| `API_KEY` | (required) | API authentication key |
| `NAMESPACE` | `openhands` | Kubernetes namespace for sandboxes |
| `INGRESS_CLASS` | `nginx` | Ingress class to use |
| `BASE_DOMAIN` | `sandbox.example.com` | Base domain for subdomain routing |
| `REGISTRY_PREFIX` | `ghcr.io/openhands` | Container registry prefix |
| `DEFAULT_IMAGE` | `ghcr.io/openhands/runtime:latest` | Default runtime image |
| `AGENT_SERVER_PORT` | `60000` | Agent server port in pods |
| `VSCODE_PORT` | `60001` | VSCode port in pods |
| `WORKER_1_PORT` | `12000` | Worker 1 port in pods |
| `WORKER_2_PORT` | `12001` | Worker 2 port in pods |
| `APP_SERVER_URL` | (optional) | OpenHands app server URL for webhooks |
| `APP_SERVER_PUBLIC_URL` | (optional) | Public URL for CORS configuration |

## Integration with OpenHands

Configure your OpenHands instance to use this runtime:

```toml
# config.toml
[sandbox]
api_key = "your-api-key"
remote_runtime_api_url = "https://runtime-api.your-domain.com"
runtime_container_image = "ghcr.io/openhands/runtime:latest"
```

Or using environment variables:

```bash
export SANDBOX_API_KEY="your-api-key"
export SANDBOX_REMOTE_RUNTIME_API_URL="https://runtime-api.your-domain.com"
export SANDBOX_RUNTIME_CONTAINER_IMAGE="ghcr.io/openhands/runtime:latest"
```

## Development

### Building Locally

```bash
# Build the binary
go build -o runtime-api ./cmd/runtime-api

# Run locally (requires kubeconfig)
export API_KEY="test-key"
export BASE_DOMAIN="localhost"
./runtime-api
```

### Building Docker Image

```bash
# Build
docker build -t openhands-kubernetes-remote-runtime:latest .

# Push to registry
docker tag openhands-kubernetes-remote-runtime:latest your-registry/openhands-kubernetes-remote-runtime:latest
docker push your-registry/openhands-kubernetes-remote-runtime:latest
```

### Testing

```bash
# Start a runtime
curl -X POST https://runtime-api.your-domain.com/start \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "image": "ghcr.io/openhands/runtime:latest",
    "command": "/usr/local/bin/openhands-agent-server --port 60000",
    "working_dir": "/openhands/code/",
    "session_id": "test-123"
  }'

# Check runtime status
curl https://runtime-api.your-domain.com/sessions/test-123 \
  -H "X-API-Key: your-api-key"

# Stop runtime
curl -X POST https://runtime-api.your-domain.com/stop \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"runtime_id": "returned-runtime-id"}'
```

## Security

- All API endpoints require `X-API-Key` authentication
- Session API keys are generated for each sandbox
- Pods are isolated in the `openhands` namespace
- Optional support for gvisor or sysbox runtime classes for additional isolation

## Troubleshooting

### Pods not starting

```bash
# Check pod status
kubectl get pods -n openhands -l app=openhands-runtime

# View pod logs
kubectl logs -n openhands <pod-name>

# Describe pod for events
kubectl describe pod -n openhands <pod-name>
```

### Ingress not routing correctly

```bash
# Check ingress configuration
kubectl get ingress -n openhands

# View ingress controller logs
kubectl logs -n ingress-nginx <ingress-controller-pod>
```

### DNS not resolving

Ensure your wildcard DNS is configured correctly:

```bash
# Test DNS resolution
nslookup test.sandbox.example.com

# Check if it points to your ingress controller
```

## License

See [LICENSE](LICENSE) file.
