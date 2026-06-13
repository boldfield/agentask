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
make fleet-builder                     # ONCE: buildx builder that can push to the insecure (HTTP) registry
make merger-image                      # buildx → docker.summercamp.eastharbor.casa:32050/agentask/merger:latest
make merger-image FLEET_TAG=v1         # pin a tag
```

The internal registry serves **HTTP**, and multi-arch `--push` requires the `docker-container`
buildx driver — so a `daemon.json` `insecure-registries` entry is **not** enough; buildkit itself
must be told the registry is http. `make fleet-builder` creates a builder with that config (a
`buildkitd.toml` + `docker buildx create --driver docker-container`). `merger-image` then builds
`linux/amd64,linux/arm64` so the same tag runs on both clusters.

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

## Workers + reviewers (amd64 / cp cluster)

These run `claude -p` against a real build, so they use the heavier `Dockerfile.fleet` (claude CLI +
Go/Rust/Python/C toolchains + git/gh + the harness + agentask CLI). **amd64-only for now** — the
arm64 (Pi) build comes later with the cross-arch build/test dimension. The merger stays multi-arch
and keeps running on the Pis.

### 1. Build + push the fleet image

```sh
make fleet-builder   # once, if you haven't already (insecure-registry buildx builder)
make fleet-image     # builds linux/amd64, pushes to the internal registry
```

### 2. Subscription auth secret (token never goes through git/logs)

Each worker/reviewer authenticates claude with a long-lived **`claude setup-token`** value (a
subscription OAuth token, NOT an API key). Generate it on your laptop and create the secret directly
so the value never transits anything else:

```sh
claude setup-token   # prints a token; copy it
kubectl --context admin@summercamp-cp -n agentask-fleet \
  create secret generic claude-oauth --from-literal=token='<paste-token>'
```

The `agentask-fleet` (server API token) and `agentask-forge-tokens` secrets from the merger setup are
reused — create them in this namespace on the cp cluster too if they aren't there yet.

### 3. Deploy

```sh
kubectl --context admin@summercamp-cp apply -f deploy/fleet/namespace.yaml
kubectl --context admin@summercamp-cp apply -f deploy/fleet/worker-deployment.yaml
kubectl --context admin@summercamp-cp apply -f deploy/fleet/reviewer-deployment.yaml
```

4 workers + 4 reviewers, matching the laptop fleet. They poll the public server, claim
`implement` / `review` tasks across all boards, clone+build in an ephemeral `emptyDir` HOME, and
open/PR-review as usual. `replicas` is the only knob to match local concurrency.
