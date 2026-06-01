GO ?= go
GOLANGCI_LINT ?= golangci-lint
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

.PHONY: vulncheck
vulncheck:
	govulncheck ./...

.PHONY: build
build: lint
	$(GO) build ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy
