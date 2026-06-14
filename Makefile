.PHONY: build run test tidy tui check release deploy fleet-builder merger-image fleet-image fleet-deploy merger-deploy

VERSION ?= $(shell git describe --tags --always --dirty)

# --- fleet images (internal registry, multi-arch for the amd64 + arm64 clusters) ---
FLEET_REGISTRY  ?= docker.summercamp.eastharbor.casa:32050
FLEET_TAG       ?= latest
FLEET_PLATFORMS ?= linux/amd64,linux/arm64
FLEET_BUILDER   ?= agentask-fleet

# Cluster contexts + namespace for fleet rollouts. Worker/reviewer run on the amd64 cp
# cluster; the merger runs on the arm64 lab (Pi) cluster. Override per-environment.
CP_CONTEXT      ?= admin@summercamp-cp
LAB_CONTEXT     ?= admin@summercamp-lab
FLEET_NAMESPACE ?= agentask-fleet

build:
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/agentask ./cmd/agentask

tui:
	mkdir -p bin
	go build -o bin/agentask-tui ./cmd/agentask-tui

run: build
	./bin/agentask

test:
	go test ./...

tidy:
	go mod tidy

check:
	@echo "Running gofmt check..."
	@out=$$(gofmt -e -l . 2>&1); rc=$$?; if [ "$$rc" -ne 0 ] || [ -n "$$out" ]; then echo "gofmt issues:"; echo "$$out"; exit 1; fi
	@echo "Running go vet..."
	@go vet ./...
	@echo "Checking go mod tidy..."
	@go mod tidy -diff || (echo "go.mod/go.sum not tidy; run 'make tidy'"; exit 1)

release:
	@if ! echo "$(VERSION)" | grep -qE "^v[0-9]+\.[0-9]+\.[0-9]+$$"; then echo "ERROR: VERSION must be a semantic version (e.g., v0.8.0). Usage: make release VERSION=vX.Y.Z"; exit 1; fi
	@if ! git diff --quiet; then echo "ERROR: Working tree has uncommitted changes"; exit 1; fi
	@if ! git diff --cached --quiet; then echo "ERROR: Index has staged changes"; exit 1; fi
	@if [ "$$(git rev-parse --abbrev-ref HEAD)" != "main" ]; then echo "ERROR: Not on main branch"; exit 1; fi
	docker build --platform linux/amd64 --build-arg VERSION=$(VERSION) -t ghcr.io/boldfield/agentask:$(VERSION) .
	docker push ghcr.io/boldfield/agentask:$(VERSION)
	git tag $(VERSION)
	git push origin $(VERSION)
	@echo "Released ghcr.io/boldfield/agentask:$(VERSION) (linux/amd64); deploy with: make deploy VERSION=$(VERSION)"

# One-time: create a buildx builder that can push to the INSECURE (HTTP) internal registry.
# Multi-arch `--push` needs the docker-container driver, so a daemon.json insecure-registries entry
# isn't enough — buildkit itself must mark the registry as http. Re-run any time to recreate it.
fleet-builder:
	@printf '[registry."%s"]\n  http = true\n  insecure = true\n' '$(FLEET_REGISTRY)' > /tmp/agentask-buildkitd.toml
	-docker buildx rm $(FLEET_BUILDER) 2>/dev/null
	docker buildx create --name $(FLEET_BUILDER) --driver docker-container \
	  --config /tmp/agentask-buildkitd.toml --bootstrap
	@echo "buildx builder '$(FLEET_BUILDER)' ready (insecure HTTP push to $(FLEET_REGISTRY))"

# Build + push the multi-arch MERGER fleet image to the internal registry.
# Requires `docker buildx` and the builder from `make fleet-builder` (run that once first).
merger-image:
	docker buildx build --builder $(FLEET_BUILDER) --platform $(FLEET_PLATFORMS) \
	  --build-arg VERSION=$(VERSION) \
	  -t $(FLEET_REGISTRY)/agentask/merger:$(FLEET_TAG) \
	  -f deploy/fleet/Dockerfile.merger --push .
	@echo "Pushed $(FLEET_REGISTRY)/agentask/merger:$(FLEET_TAG) ($(FLEET_PLATFORMS))"

# Build + push the heavy WORKER/REVIEWER fleet image (claude + toolchains). amd64-only for now (the
# cp cluster); arm64 comes with the cross-arch build/test dimension. Needs `make fleet-builder` once.
fleet-image:
	docker buildx build --builder $(FLEET_BUILDER) --platform linux/amd64 \
	  --build-arg VERSION=$(VERSION) \
	  -t $(FLEET_REGISTRY)/agentask/fleet:$(FLEET_TAG) \
	  -f deploy/fleet/Dockerfile.fleet --push .
	@echo "Pushed $(FLEET_REGISTRY)/agentask/fleet:$(FLEET_TAG) (linux/amd64)"

# Build + push the fleet image, then roll the cp-cluster worker + reviewer onto it. Like
# `make deploy`, this pins the resolved image DIGEST (not the reused :latest tag) into the
# pod spec — that spec change is what forces a rollout and a fresh pull. Assumes the
# deployments already exist (first-time setup — namespace, secrets, apply — is in
# deploy/fleet/README.md). To re-roll WITHOUT rebuilding, instead run:
#   kubectl --context $(CP_CONTEXT) -n $(FLEET_NAMESPACE) rollout restart deploy/worker deploy/reviewer
fleet-deploy: fleet-image
	@echo "Resolving digest for $(FLEET_REGISTRY)/agentask/fleet:$(FLEET_TAG)..."
	@DIGEST=$$(docker buildx imagetools inspect --builder $(FLEET_BUILDER) "$(FLEET_REGISTRY)/agentask/fleet:$(FLEET_TAG)" 2>/dev/null | awk '/^Digest:/{print $$2; exit}'); \
	if ! echo "$$DIGEST" | grep -qE '^sha256:[a-f0-9]{64}$$'; then echo "ERROR: could not resolve fleet image digest (got '$$DIGEST')"; exit 1; fi; \
	REF="$(FLEET_REGISTRY)/agentask/fleet@$$DIGEST"; \
	echo "Deploying $$REF to worker + reviewer ($(CP_CONTEXT))"; \
	kubectl --context $(CP_CONTEXT) -n $(FLEET_NAMESPACE) set image deploy/worker   worker="$$REF"; \
	kubectl --context $(CP_CONTEXT) -n $(FLEET_NAMESPACE) set image deploy/reviewer reviewer="$$REF"; \
	kubectl --context $(CP_CONTEXT) -n $(FLEET_NAMESPACE) rollout status deploy/worker   --timeout=300s; \
	kubectl --context $(CP_CONTEXT) -n $(FLEET_NAMESPACE) rollout status deploy/reviewer --timeout=300s

# Build + push the multi-arch merger image, then roll the lab-cluster merger onto it
# (digest-pinned, same mechanism as fleet-deploy). Re-roll without rebuild:
#   kubectl --context $(LAB_CONTEXT) -n $(FLEET_NAMESPACE) rollout restart deploy/merger
merger-deploy: merger-image
	@echo "Resolving digest for $(FLEET_REGISTRY)/agentask/merger:$(FLEET_TAG)..."
	@DIGEST=$$(docker buildx imagetools inspect --builder $(FLEET_BUILDER) "$(FLEET_REGISTRY)/agentask/merger:$(FLEET_TAG)" 2>/dev/null | awk '/^Digest:/{print $$2; exit}'); \
	if ! echo "$$DIGEST" | grep -qE '^sha256:[a-f0-9]{64}$$'; then echo "ERROR: could not resolve merger image digest (got '$$DIGEST')"; exit 1; fi; \
	REF="$(FLEET_REGISTRY)/agentask/merger@$$DIGEST"; \
	echo "Deploying $$REF to merger ($(LAB_CONTEXT))"; \
	kubectl --context $(LAB_CONTEXT) -n $(FLEET_NAMESPACE) set image deploy/merger merger="$$REF"; \
	kubectl --context $(LAB_CONTEXT) -n $(FLEET_NAMESPACE) rollout status deploy/merger --timeout=300s

deploy:
	@echo "Resolving image digest for ghcr.io/boldfield/agentask:$(VERSION)..."
	@DIGEST=$$(docker buildx imagetools inspect "ghcr.io/boldfield/agentask:$(VERSION)" 2>/dev/null | awk '/^Digest:/{print $$2; exit}' || echo ""); \
	if [ -z "$$DIGEST" ]; then echo "ERROR: Image tag $(VERSION) not found in registry"; exit 1; fi; \
	if ! echo "$$DIGEST" | grep -qE '^sha256:[a-f0-9]{64}$$'; then echo "ERROR: Invalid digest format: $$DIGEST"; exit 1; fi; \
	echo "Deploying ghcr.io/boldfield/agentask@$$DIGEST"; \
	kubectl -n agentask set image deploy/agentask agentask="ghcr.io/boldfield/agentask@$$DIGEST"; \
	kubectl -n agentask rollout status deploy/agentask --timeout=180s
