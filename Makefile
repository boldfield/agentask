.PHONY: build run test tidy tui check release

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
	@if [ -z "$(VERSION)" ]; then echo "ERROR: VERSION not set. Usage: make release VERSION=vX.Y.Z"; exit 1; fi
	@if ! echo "$(VERSION)" | grep -q "^v"; then echo "ERROR: VERSION must start with 'v' (e.g., vX.Y.Z)"; exit 1; fi
	@if ! git diff --quiet; then echo "ERROR: Working tree has uncommitted changes"; exit 1; fi
	@if ! git diff --cached --quiet; then echo "ERROR: Index has staged changes"; exit 1; fi
	@if [ "$$(git rev-parse --abbrev-ref HEAD)" != "main" ]; then echo "ERROR: Not on main branch"; exit 1; fi
	git tag $(VERSION)
	git push origin $(VERSION)
	@echo "CI is building ghcr.io/boldfield/agentask:$(VERSION); when green, run: make deploy VERSION=$(VERSION)"
