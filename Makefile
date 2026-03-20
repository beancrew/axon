.PHONY: build test lint proto clean

build:
	go build -o bin/axon ./cmd/axon
	go build -o bin/axon-server ./cmd/axon-server
	go build -o bin/axon-agent ./cmd/axon-agent

test:
	go test ./... -race -cover

lint:
	golangci-lint run

proto:
	@echo "proto generation not yet implemented"

clean:
	rm -rf bin/
