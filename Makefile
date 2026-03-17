.PHONY: all fmt fmtcheck vet test build build-abby agents install clean integration bench bench-integration bench-report bench-all

all: fmtcheck vet test build

fmt:
	gofmt -w .

fmtcheck:
	@test -z "$$(gofmt -l .)" || { echo "Files need gofmt:"; gofmt -l .; exit 1; }

vet:
	go vet ./...

test:
	go test -race ./...

build: build-abby

build-abby:
	@mkdir -p build
	go build -o build/abby ./cmd/abby

agents: build-abby
	./build/abby build

install: build-abby
	@mkdir -p $(or $(PREFIX),/usr/local)/bin
	cp build/abby $(or $(PREFIX),/usr/local)/bin/abby
	@echo "Installed abby → $(or $(PREFIX),/usr/local)/bin/abby"

integration:
	go test -tags integration -race -count=1 -timeout 120s ./internal/integration/

bench:
	go test -bench=. -benchmem -count=3 ./benchmarks/

bench-integration:
	go test -tags integration -bench=. -benchmem -count=3 -timeout 120s ./internal/integration/

bench-report:
	go test -run "TestScalingCurve|TestComparisonWithArticle|TestAntiPatternThreshold|TestTokenizerComparison|TestMultiTurnProjection|TestClaudeCodeBaseline|TestArticleMethodology" -v ./benchmarks/

bench-all: bench bench-integration bench-report

clean:
	rm -rf build/ .abbyfile/ .mcp.json .codex/config.toml .gemini/settings.json
	go clean ./...
