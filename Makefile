BINARY    = ipgeo
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COVERAGE_DIR ?= .coverage
COVERAGE_PROFILE ?= $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML ?= $(COVERAGE_DIR)/coverage.html
COVERAGE_THRESHOLD ?= 95.0
COVERAGE_CMD_THRESHOLD ?= 40.0
COVERAGE_CMD_PROFILE ?= $(COVERAGE_DIR)/cmd.out
COVERAGE_CMD_HTML ?= $(COVERAGE_DIR)/cmd.html
LDFLAGS   = -X github.com/kibaamor/ipgeo/cmd/ipgeo/cmd.version=$(VERSION) \
            -X github.com/kibaamor/ipgeo/cmd/ipgeo/cmd.gitCommit=$(GIT_COMMIT) \
            -X github.com/kibaamor/ipgeo/cmd/ipgeo/cmd.buildDate=$(BUILD_DATE)

LINT_CMD = cd ./cmd/ipgeo && golangci-lint run

.PHONY: build clean test coverage tidy lint lint-fix

build:
	go build -C ./cmd/ipgeo -ldflags "$(LDFLAGS)" -o ../../$(BINARY) .

clean:
	rm -f $(BINARY)

test:
	go clean -testcache
	go test github.com/kibaamor/ipgeo/...
	go test -C ./cmd/ipgeo ./...

coverage:
	mkdir -p $(COVERAGE_DIR)
	go test -coverprofile=$(COVERAGE_PROFILE) github.com/kibaamor/ipgeo
	go tool cover -func=$(COVERAGE_PROFILE)
	go tool cover -html=$(COVERAGE_PROFILE) -o $(COVERAGE_HTML)
	@total=$$(go tool cover -func=$(COVERAGE_PROFILE) | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v total="$$total" -v threshold="$(COVERAGE_THRESHOLD)" 'BEGIN { if (total + 0 < threshold + 0) { printf "coverage %.1f%% is below %.1f%%\n", total, threshold; exit 1 } }'
	@echo "line coverage report: $(COVERAGE_HTML)"
	@echo ""
	@echo "--- submodule (cmd/ipgeo) coverage ---"
	go test -C ./cmd/ipgeo -coverprofile=../../$(COVERAGE_CMD_PROFILE) ./...
	cd ./cmd/ipgeo && go tool cover -func=../../$(COVERAGE_CMD_PROFILE)
	cd ./cmd/ipgeo && go tool cover -html=../../$(COVERAGE_CMD_PROFILE) -o ../../$(COVERAGE_CMD_HTML)
	@total_sub=$$(cd ./cmd/ipgeo && go tool cover -func=../../$(COVERAGE_CMD_PROFILE) | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v t="$$total_sub" -v threshold="$(COVERAGE_CMD_THRESHOLD)" 'BEGIN { if (t + 0 < threshold + 0) { printf "submodule coverage %.1f%% is below %.1f%%\n", t, threshold; exit 1 } }'
	@echo "submodule coverage report: $(COVERAGE_CMD_HTML)"

tidy:
	go mod tidy
	go mod tidy -C ./cmd/ipgeo

lint:
	golangci-lint run ./...
	$(LINT_CMD) ./...

lint-fix:
	golangci-lint run --fix ./...
	$(LINT_CMD) --fix ./...
