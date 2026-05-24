BINARY    = ipgeo
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COVERAGE_DIR ?= .coverage
COVERAGE_PROFILE ?= $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML ?= $(COVERAGE_DIR)/coverage.html
COVERAGE_THRESHOLD ?= 95.0
LDFLAGS   = -X github.com/kibaamor/ipgeo/cmd/ipgeo/cmd.version=$(VERSION) \
            -X github.com/kibaamor/ipgeo/cmd/ipgeo/cmd.gitCommit=$(GIT_COMMIT) \
            -X github.com/kibaamor/ipgeo/cmd/ipgeo/cmd.buildDate=$(BUILD_DATE)

.PHONY: build clean test coverage tidy lint lint-fix

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/ipgeo

clean:
	rm -f $(BINARY)

test:
	go test github.com/kibaamor/ipgeo/...
	go test github.com/kibaamor/ipgeo/cmd/ipgeo/...

coverage:
	mkdir -p $(COVERAGE_DIR)
	go test -coverprofile=$(COVERAGE_PROFILE) github.com/kibaamor/ipgeo
	go tool cover -func=$(COVERAGE_PROFILE)
	go tool cover -html=$(COVERAGE_PROFILE) -o $(COVERAGE_HTML)
	@total=$$(go tool cover -func=$(COVERAGE_PROFILE) | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v total="$$total" -v threshold="$(COVERAGE_THRESHOLD)" 'BEGIN { if (total + 0 < threshold + 0) { printf "coverage %.1f%% is below %.1f%%\n", total, threshold; exit 1 } }'
	@echo "line coverage report: $(COVERAGE_HTML)"

tidy:
	go mod tidy
	go mod tidy -C ./cmd/ipgeo

lint:
	golangci-lint run ./...
	golangci-lint run ./cmd/ipgeo/...

lint-fix:
	golangci-lint run --fix ./...
	golangci-lint run --fix ./cmd/ipgeo/...
