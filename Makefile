VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
BINARIES := axon axon-server axon-agent

.PHONY: build build-cli build-server build-agent test lint proto clean release

build: build-cli build-server build-agent

build-cli:
	go build $(LDFLAGS) -o bin/axon ./cmd/axon

build-server:
	go build $(LDFLAGS) -o bin/axon-server ./cmd/axon-server

build-agent:
	go build $(LDFLAGS) -o bin/axon-agent ./cmd/axon-agent

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

proto:
	bash scripts/proto-gen.sh

clean:
	rm -rf bin/ dist/

release:
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%%/*}; \
		arch=$${platform##*/}; \
		for bin in $(BINARIES); do \
			cmd_dir=cmd/$${bin}; \
			output=dist/$${bin}-$${os}-$${arch}; \
			if [ "$${os}" = "windows" ]; then output=$${output}.exe; fi; \
			echo "Building $${output}..."; \
			GOOS=$${os} GOARCH=$${arch} go build $(LDFLAGS) -o $${output} ./$${cmd_dir}; \
		done; \
	done
	@echo "Release binaries in dist/"
