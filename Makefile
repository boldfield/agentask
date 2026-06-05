.PHONY: build run test tidy

build:
	mkdir -p bin
	go build -o bin/agentask ./cmd/agentask

run: build
	./bin/agentask

test:
	go test ./...

tidy:
	go mod tidy
