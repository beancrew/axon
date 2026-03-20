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
	bash scripts/proto-gen.sh

clean:
	rm -rf bin/
