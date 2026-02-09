# Deployment Guide

This guide provides step-by-step instructions for deploying the OpenHands Kubernetes Remote Runtime service.

## Prerequisites

1. **Kubernetes Cluster**: Version 1.30 or higher
2. **kubectl**: Configured to access your cluster
3. **Ingress Controller**: nginx-ingress (or compatible)
4. **Domain**: Wildcard DNS configured
5. **Container Images**: Access to OpenHands runtime images

## Step 1: Install Ingress Controller

If you don't have an ingress controller, install nginx-ingress:

```bash
# Using Helm
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx --create-namespace
```

## Step 2: Configure DNS

Set up wildcard DNS for your base domain. Get your ingress controller's external IP:

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller
```

Create DNS records (example for CloudFlare/AWS Route53/etc):

```
Type: A
Name: *.sandbox.example.com
Value: <INGRESS_EXTERNAL_IP>

Type: A
Name: runtime-api.example.com
Value: <INGRESS_EXTERNAL_IP>
```

## Step 3: Generate API Key

Generate a secure API key:

```bash
# Using openssl
openssl rand -hex 32

# Or using uuidgen
uuidgen | sha256sum | head -c 64
```

Save this key - you'll need it for both the runtime API and OpenHands configuration.

## Step 4: Update Kubernetes Manifests

Edit `k8s/deployment.yaml`:

1. **Update ConfigMap** with your domain:
```yaml
data:
  BASE_DOMAIN: "your-domain.com"  # Change this
  REGISTRY_PREFIX: "your-registry/openhands"  # Change if needed
  # If your sandbox images require a pull secret (private registry), add:
  # IMAGE_PULL_SECRETS: "your-registry-pull-secret"
```

2. **Update Secret** with your API key:
```yaml
stringData:
  API_KEY: "your-generated-api-key"  # Use the key from step 3
```

3. **Update Ingress** host:
```yaml
rules:
  - host: runtime-api.your-domain.com  # Change this
```

4. **Update Image** (if using your own registry):
```yaml
image: ghcr.io/zparnold/openhands-kubernetes-remote-runtime:latest
```

## Step 5: Build and Push Docker Image (Optional)

If you want to build your own image:

```bash
# Build
make docker-build VERSION=latest

# Tag for your registry
docker tag ghcr.io/zparnold/openhands-kubernetes-remote-runtime:latest \
  your-registry/openhands-kubernetes-remote-runtime:latest

# Push
docker push your-registry/openhands-kubernetes-remote-runtime:latest
```

## Step 6: Deploy to Kubernetes

Deploy the runtime API:

```bash
# Apply all resources
kubectl apply -f k8s/deployment.yaml

# Verify deployment
kubectl get pods -n openhands
kubectl get svc -n openhands
kubectl get ingress -n openhands
```

Wait for the pod to be ready:

```bash
kubectl wait --for=condition=ready pod \
  -l app=openhands-runtime-api \
  -n openhands \
  --timeout=300s
```

## Step 7: Verify Installation

Test the health endpoint:

```bash
# Health check (no authentication required)
curl https://runtime-api.your-domain.com/health
# Expected: OK

# Liveness probe (no authentication required)
curl https://runtime-api.your-domain.com/liveness
# Expected: OK

# Readiness probe (no authentication required)
curl https://runtime-api.your-domain.com/readiness
# Expected: OK

# Registry prefix endpoint (requires authentication)
curl -H "X-API-Key: your-api-key" \
  https://runtime-api.your-domain.com/registry_prefix
# Expected: {"registry_prefix":"your-registry/openhands"}
```

## Step 8: Configure OpenHands

Update your OpenHands configuration to use the runtime:

**Option 1: config.toml**
```toml
[sandbox]
type = "remote"
api_key = "your-api-key"
remote_runtime_api_url = "https://runtime-api.your-domain.com"
runtime_container_image = "ghcr.io/openhands/runtime:latest"
```

**Option 2: Environment Variables**
```bash
export SANDBOX_TYPE=remote
export SANDBOX_API_KEY=your-api-key
export SANDBOX_REMOTE_RUNTIME_API_URL=https://runtime-api.your-domain.com
export SANDBOX_RUNTIME_CONTAINER_IMAGE=ghcr.io/openhands/runtime:latest
```

## Step 9: Test End-to-End

Create a test session:

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

# Verify pod is running
kubectl get pods -n openhands -l session-id=test-123

# Clean up test runtime
curl -X POST https://runtime-api.your-domain.com/stop \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"runtime_id": "returned-runtime-id"}'
```

## Monitoring and Logs

View runtime API logs:

```bash
kubectl logs -f -n openhands -l app=openhands-runtime-api
```

View sandbox pod logs:

```bash
# List all runtime pods
kubectl get pods -n openhands -l app=openhands-runtime

# View specific runtime pod logs
kubectl logs -n openhands runtime-<runtime-id>
```

## Troubleshooting

### Pod not starting
```bash
# Check pod events
kubectl describe pod -n openhands <pod-name>

# Check runtime API logs
kubectl logs -n openhands -l app=openhands-runtime-api
```

### Ingress not working
```bash
# Check ingress status
kubectl get ingress -n openhands

# Check ingress controller logs
kubectl logs -n ingress-nginx -l app.kubernetes.io/component=controller
```

### DNS not resolving
```bash
# Test DNS resolution
nslookup test-123.sandbox.your-domain.com
dig test-123.sandbox.your-domain.com
```

## Upgrading

To upgrade the runtime API:

```bash
# Update the image version in k8s/deployment.yaml
# Then apply:
kubectl apply -f k8s/deployment.yaml

# Or restart the deployment:
kubectl rollout restart deployment/openhands-runtime-api -n openhands
```

## Scaling

The runtime API can be scaled horizontally:

```bash
kubectl scale deployment/openhands-runtime-api \
  --replicas=3 \
  -n openhands
```

Note: The current implementation uses in-memory state, so scaling may require adding persistent storage or a shared database.

## Security Considerations

1. **API Key**: Store securely in Kubernetes Secret
2. **Network Policies**: Consider adding network policies to isolate runtime pods
3. **RBAC**: The service account has minimal required permissions
4. **TLS**: Ensure ingress controller has valid TLS certificates
5. **Runtime Classes**: Use gvisor or sysbox for additional sandbox isolation

## Next Steps

- Set up monitoring with Prometheus
- Configure autoscaling based on pod count
- Add persistent state storage (database)
- Implement pod cleanup policies
- Set up alerting for failed pods
