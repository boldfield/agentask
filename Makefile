.PHONY: build run test tidy tui check release deploy

VERSION ?= $(shell git describe --tags --always --dirty)

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
	git tag $(VERSION)
	git push origin $(VERSION)
	@echo "CI is building ghcr.io/boldfield/agentask:$(VERSION); when green, run: make deploy VERSION=$(VERSION)"

deploy:
	@if [ -z "$(VERSION)" ]; then echo "ERROR: VERSION is required. Usage: make deploy VERSION=vX.Y.Z"; exit 1; fi
	@echo "Resolving image digest for ghcr.io/boldfield/agentask:$(VERSION)..."
	@DIGEST=$$(docker buildx imagetools inspect "ghcr.io/boldfield/agentask:$(VERSION)" --format '{{.Manifest.Digest}}' 2>/dev/null || echo ""); \
	if [ -z "$$DIGEST" ]; then DIGEST=$$(docker manifest inspect -v "ghcr.io/boldfield/agentask:$(VERSION)" 2>/dev/null | grep -i 'Digest.*sha256' | head -1 | grep -oE 'sha256:[a-f0-9]+' || echo ""); fi; \
	if [ -z "$$DIGEST" ]; then echo "ERROR: Image tag $(VERSION) not found in registry"; exit 1; fi; \
	echo "Deploying ghcr.io/boldfield/agentask@$$DIGEST"; \
	kubectl -n agentask set image deploy/agentask agentask="ghcr.io/boldfield/agentask@$$DIGEST"; \
	kubectl -n agentask rollout status deploy/agentask --timeout=180s
