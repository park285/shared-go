GO ?= go
GO_TOOLING ?= $(CURDIR)/scripts/ci/go-tooling.sh
GOLANGCI_LINT ?= bash $(GO_TOOLING) golangci-lint
GOVULNCHECK ?= bash $(GO_TOOLING) govulncheck
GOLANGCI_CONFIG ?= .golangci.yml

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

# race 계측은 할당 횟수를 바꾸므로 별도로 측정한다.
.PHONY: test-allocations
test-allocations:
	$(GO) test -count=1 -run '^TestPromptGuardAllocationCeilings$$' ./pkg/promptguard
	$(GO) test -count=1 -run '^TestLoggingAllocationCeilings$$' ./pkg/logging

.PHONY: vulncheck
vulncheck:
	$(GOVULNCHECK) ./...

.PHONY: build
build: lint
	$(GO) build ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy
