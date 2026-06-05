# Agentask Kubernetes Manifests

## Overview

This directory contains Kubernetes manifests to deploy Agentask as a single-replica Deployment with persistent storage.

## Prerequisites

1. A Kubernetes cluster with the `local-path-storage` StorageClass available (or another local storage provisioner)
2. The Agentask image built and available in the cluster's image registry or preloaded

## Image preparation

Before applying the manifests, build and push (or load) the Agentask image:

```bash
# Build the image
docker build -t ghcr.io/boldfield/agentask:latest .

# Push to registry
docker push ghcr.io/boldfield/agentask:latest

# Or, for local testing with kind/minikube:
kind load docker-image ghcr.io/boldfield/agentask:latest
```

## Configuration

Create the `agentask-secret` **out-of-band** with a freshly generated token. It is
deliberately not part of the kustomization (and `secret.yaml.example` is named so
`kubectl` skips it), so a real token never lives in git and `apply` can never
clobber it with a placeholder:

```bash
kubectl -n agentask create secret generic agentask-secret \
  --from-literal=token="$(openssl rand -hex 32)" \
  --dry-run=client -o yaml | kubectl apply -f -
```

The deployment reads it via `secretKeyRef` (name `agentask-secret`, key `token`).

## Deployment

Create the namespace and secret first, then apply the manifests (the secret is
NOT in the kustomization — create it out-of-band per Configuration above):

```bash
# 1. Namespace
kubectl create namespace agentask

# 2. Secret (out-of-band — see Configuration)
kubectl -n agentask create secret generic agentask-secret \
  --from-literal=token="$(openssl rand -hex 32)" \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Manifests (pvc + deployment + service)
kubectl -n agentask apply -k deploy/k8s/
# ...or individually:
#   kubectl -n agentask apply -f deploy/k8s/pvc.yaml -f deploy/k8s/deployment.yaml -f deploy/k8s/service.yaml
```

The deployment sets a `restricted`-compliant `securityContext` (non-root uid 65532,
dropped capabilities, seccomp RuntimeDefault), so it runs in PodSecurity-enforcing
namespaces.

## Verification

Check that the deployment is running:

```bash
# Watch the pod come up
kubectl get pods -l app=agentask -w

# Check the pod logs
kubectl logs -f deployment/agentask

# Verify the PVC is bound
kubectl get pvc agentask-data

# Test the healthz endpoint
kubectl port-forward svc/agentask 8080:8080
curl localhost:8080/healthz
```

## Important: Single-Replica Constraint

**DO NOT SCALE THIS DEPLOYMENT BEYOND 1 REPLICA.**

The Agentask application uses SQLite as its database, which is a single-writer system. The PersistentVolume is provisioned with `local-path` storage, which means it is node-local and cannot be shared across nodes.

Running multiple replicas will result in:
- Database corruption due to concurrent writes
- Data inconsistency across replicas
- Pod evictions and cascading failures

To scale horizontally, the storage layer must be replaced with a distributed database (PostgreSQL, CockroachDB, etc.) and the implementation updated to support multi-replica deployments. This is explicitly out of scope for the MVP.

## Data persistence

The PVC is configured with `ReadWriteOnce` access, ensuring only one pod can mount it at a time. Data is stored at `/data/agentask.db` inside the container.

When the pod is killed and rescheduled on the same or different node, the PVC will be remounted and data will be preserved (assuming the PV remains bound to a node with the data).

## External access

The `Service` is `ClusterIP` (in-cluster only). To reach Agentask from outside the
cluster, either `kubectl port-forward svc/agentask 8080:8080`, or expose it via an
ingress. `ingress.yaml.example` is a Traefik sample (built-in `letsencrypt` cert
resolver) — adapt the host/class/TLS to your environment, create a DNS record for
the host, and apply it. Agentask's only auth is a single bearer token, so prefer a
private network / tailnet over public exposure.

## Cleanup

To remove the deployment and free resources:

```bash
kubectl delete deployment agentask
kubectl delete service agentask
kubectl delete secret agentask-secret
kubectl delete pvc agentask-data
```

Note: Deleting the PVC will delete the data if the underlying PV is also deleted.
