# Agentask fleet on Kubernetes

Run the worker/reviewer/merger fleet in-cluster instead of on a laptop. **Merger-first**: the merger
is non-LLM and repo-less (`agentask merge` is pure REST), so it proves the in-cluster plumbing with
zero subscription-auth risk. Workers and reviewers (which need `claude` + a build toolchain) come
next, on a separate, heavier image.

## Topology

- Two **separate** clusters: `summercamp-cp` (3× amd64) and `summercamp-lab` (8× arm64 Pis). They
  are not one mixed cluster, so fleet pods reach the server via its **public ingress**
  (`https://agentask.summercamp.eastharbor.casa`), not a cluster-DNS service name.
- **Placement:** mergers are cheap → run them on the **Pi (lab)** cluster. Workers/reviewers are
  memory-hungry (they build/test the target repo) → the **amd64 (cp)** cluster, with the 8 GB Pi as
  overflow. The real cap is the **subscription rate**, not node count — keep claude-agent replicas
  modest (the harness backs off on rate-limit).
- Images live in the internal registry `docker.summercamp.eastharbor.casa:32050/agentask/*`.

## Build + push the merger image (multi-arch)

```bash
make merger-image                      # buildx → docker.summercamp.eastharbor.casa:32050/agentask/merger:latest
make merger-image FLEET_TAG=v1         # pin a tag
```

Requires `docker buildx` and that your builder can push to the internal registry (if it's insecure,
configure buildx/daemon to allow it). The build is `linux/amd64,linux/arm64` so the same tag runs on
both clusters.

## Deploy the merger (to the Pi cluster)

```bash
LAB=admin@summercamp-lab
kubectl --context $LAB apply -f deploy/fleet/namespace.yaml

# Secrets (tokens never touch a committed file — see secret.example.yaml):
TOKEN=$(kubectl --context admin@summercamp-cp -n agentask get secret agentask-secret -o jsonpath='{.data.token}' | base64 -d)
kubectl --context $LAB -n agentask-fleet create secret generic agentask-fleet --from-literal=token="$TOKEN"
kubectl --context $LAB -n agentask-fleet create secret generic agentask-forge-tokens \
  --from-file=forge-tokens="$HOME/.agentask/forge-tokens"

kubectl --context $LAB apply -f deploy/fleet/merger-deployment.yaml
kubectl --context $LAB -n agentask-fleet logs -l app.kubernetes.io/component=merger -f
```

You should see it poll, claim `merge`-kind tasks across all boards, and squash-merge — no claude
involved.

## Prerequisites to verify on the lab cluster (first time)

- It can **reach the public server URL** (it can — that's how the laptop fleet connects).
- Its container runtime can **pull from `docker.summercamp.eastharbor.casa:32050`** (if the registry
  is insecure/no-auth, Talos needs the registry allow-listed in machine config; the cp cluster
  already pulls from it). If pulls fail with TLS/forbidden, that's the thing to fix.

## What's NOT here yet (next phase: workers + reviewers)

- A heavier `Dockerfile.fleet` (claude + node + git/gh + go/rust/python toolchains + the harness),
  modeled on `nightshift/k8s/nightshift/Dockerfile`.
- Subscription auth via `claude setup-token` → a `claude-oauth` secret → `CLAUDE_CODE_OAUTH_TOKEN`
  env on each worker/reviewer pod (fans out cleanly; no shared credentials file).
- worker + reviewer Deployments (4 + 4, matching the laptop fleet), on the amd64 cluster.
