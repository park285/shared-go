GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_CONFIG ?= .golangci.yml
PERF_GATE_BASELINE ?= artifacts/perf/baseline/main
PERF_GATE_CANDIDATE ?= artifacts/perf/pr
PERF_GATE_BENCHTIME ?= 100ms
PERF_GATE_COLLECT_ARGS := --policy perf-budget.yaml --candidate $(PERF_GATE_CANDIDATE) --gate pr
ifneq ($(strip $(PERF_GATE_COUNT)),)
PERF_GATE_COLLECT_ARGS += --count $(PERF_GATE_COUNT)
endif
ifneq ($(strip $(PERF_GATE_BENCHTIME)),)
PERF_GATE_COLLECT_ARGS += --benchtime $(PERF_GATE_BENCHTIME)
endif

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) ./...

.PHONY: fmt
fmt:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) --fix ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race -count=1 ./...

.PHONY: perf-gate
perf-gate:
	./scripts/perf/check-bench-regression.sh collect $(PERF_GATE_COLLECT_ARGS)
	./scripts/perf/check-bench-regression.sh --policy perf-budget.yaml --baseline $(PERF_GATE_BASELINE) --candidate $(PERF_GATE_CANDIDATE) --gate pr

.PHONY: vulncheck
vulncheck:
	govulncheck ./...

.PHONY: build
build: lint
	$(GO) build ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy
