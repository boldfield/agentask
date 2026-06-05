---
id: T15
title: k8s manifests — single-replica Deployment + local-path PVC + Service
state: in_progress
document: DESIGN.md
depends_on: [T14]
---

## Spec
Manifests under `deploy/k8s/`:

- `PersistentVolumeClaim` using the `local-path` StorageClass, `ReadWriteOnce`.
- `Deployment` with **`replicas: 1`** (mandatory — SQLite single-writer + node-local PV;
  add a comment in the manifest explaining why it must never be scaled).
- Mount the PVC at the DB path; set `AGENTASK_DB` to a file on it.
- `AGENTASK_TOKEN` from a `Secret`.
- `Service` (ClusterIP) exposing the API port. Liveness/readiness probes on `/healthz`.

## Acceptance criteria
- `kubectl apply` brings up one running pod backed by the PVC.
- Killing the pod and rescheduling preserves data (same PVC).
- Manifest comments document the replicas:1 constraint.
