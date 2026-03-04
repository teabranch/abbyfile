.PHONY: all fmt fmtcheck vet test build build-agentfile agents install clean integration

all: fmtcheck vet test build

fmt:
	gofmt -w .

fmtcheck:
	@test -z "$$(gofmt -l .)" || { echo "Files need gofmt:"; gofmt -l .; exit 1; }

vet:
	go vet ./...

test:
	go test -race ./...

build: build-agentfile

build-agentfile:
	@mkdir -p build
	go build -o build/agentfile ./cmd/agentfile

agents: build-agentfile
	./build/agentfile build

install: build-agentfile
	@mkdir -p $(or $(PREFIX),/usr/local)/bin
	cp build/agentfile $(or $(PREFIX),/usr/local)/bin/agentfile
	@echo "Installed agentfile → $(or $(PREFIX),/usr/local)/bin/agentfile"

integration:
	go test -tags integration -race -count=1 -timeout 120s ./internal/integration/

clean:
	rm -rf build/ .agentfile/ .mcp.json
	go clean ./...
