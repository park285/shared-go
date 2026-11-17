GO ?= go
GO_TOOLING ?= $(CURDIR)/scripts/ci/go-tooling.sh
GOLANGCI_LINT ?= bash $(GO_TOOLING) golangci-lint
GOVULNCHECK ?= bash $(GO_TOOLING) govulncheck
GOLANGCI_CONFIG ?= $(CURDIR)/.golangci.yml
MODULES ?= . core postgres transport openai

.PHONY: lint
lint:
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) ./...); done

.PHONY: fmt
fmt:
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GOLANGCI_LINT) run -c $(GOLANGCI_CONFIG) --fix ./...); done

.PHONY: test
test:
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GO) test ./...); done

.PHONY: test-race
test-race:
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GO) test -race -count=1 ./...); done

# race 계측은 할당 횟수를 바꾸므로 별도로 측정한다.
.PHONY: test-allocations
test-allocations:
	$(GO) test -count=1 -run '^Test(PromptGuard|Logging)AllocationCeilings$$' ./pkg/promptguard ./pkg/logging

.PHONY: vulncheck
vulncheck:
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GOVULNCHECK) ./...); done

.PHONY: build
build: lint
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GO) build ./...); done

.PHONY: tidy
tidy:
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GO) mod tidy); done

.PHONY: tidy-check
tidy-check:
	@set -e; for module in $(MODULES); do (cd "$$module" && $(GO) mod tidy -diff); done

.PHONY: source-tidy-check
source-tidy-check:
	bash $(GO_TOOLING) tidy-workspace $(MODULES)
