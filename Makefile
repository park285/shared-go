GO ?= go
GO_TOOLING ?= $(CURDIR)/scripts/ci/go-tooling.sh
GOLANGCI_LINT ?= bash $(GO_TOOLING) golangci-lint
GOVULNCHECK ?= bash $(GO_TOOLING) govulncheck
GOLANGCI_CONFIG ?= .golangci.yml
PERF_GATE_BASELINE ?= artifacts/perf/baseline/main
PERF_GATE_CANDIDATE ?= artifacts/perf/pr
PERF_GATE_BENCHTIME ?= 100ms
PERF_GATE_ID ?= perf-gate
GUARD_PERF_BASELINE ?= artifacts/perf/baseline/guards-main-sla-v5
GUARD_PERF_CANDIDATE ?= artifacts/perf/guards-pr
GUARD_PERF_GATE_ID ?= guard-perf-gate
PERF_GATE_COLLECT_ARGS := --policy perf-budget.yaml --candidate $(PERF_GATE_CANDIDATE) --gate-id $(PERF_GATE_ID) --gate pr
ifneq ($(strip $(PERF_GATE_COUNT)),)
PERF_GATE_COLLECT_ARGS += --count $(PERF_GATE_COUNT)
endif
ifneq ($(strip $(PERF_GATE_BENCHTIME)),)
PERF_GATE_COLLECT_ARGS += --benchtime $(PERF_GATE_BENCHTIME)
endif

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) ./...
	( cd scripts/perf/benchgate && GOWORK=off $(GOLANGCI_LINT) run )

.PHONY: fmt
fmt:
	$(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) --fix ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race -count=1 ./...

.PHONY: perf-gate-test
perf-gate-test:
	bash scripts/perf/check-bench-regression_test.sh

.PHONY: perf-gate
perf-gate: perf-gate-test
	./scripts/perf/check-bench-regression.sh collect $(PERF_GATE_COLLECT_ARGS)
	./scripts/perf/check-bench-regression.sh --policy perf-budget.yaml --baseline $(PERF_GATE_BASELINE) --candidate $(PERF_GATE_CANDIDATE) --gate-id $(PERF_GATE_ID) --gate pr

.PHONY: guard-perf-gate
guard-perf-gate:
	./scripts/perf/check-bench-regression.sh collect --policy perf-budget-guards.yaml --candidate $(GUARD_PERF_CANDIDATE) --gate-id $(GUARD_PERF_GATE_ID) --gate pr --count 6 --benchtime 100ms
	./scripts/perf/check-bench-regression.sh --policy perf-budget-guards.yaml --baseline $(GUARD_PERF_BASELINE) --candidate $(GUARD_PERF_CANDIDATE) --gate-id $(GUARD_PERF_GATE_ID) --gate pr

.PHONY: vulncheck
vulncheck:
	$(GOVULNCHECK) ./...

.PHONY: build
build: lint
	$(GO) build ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy
