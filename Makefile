.PHONY: build run test tidy check

build:
	mkdir -p bin
	go build -o bin/agentask ./cmd/agentask

run: build
	./bin/agentask

test:
	go test ./...

tidy:
	go mod tidy

check:
	@echo "Running gofmt check..."
	@gofmt -l . | tee /dev/stderr | (! read) || exit 1
	@echo "Running go vet..."
	@go vet ./...
