GO ?= go
override export GOWORK := off
unexport CDPATH
$(foreach var,GO_TOOLING GO_LINT_MATRIX GOLANGCI_LINT GOVULNCHECK \
              GOLANGCI_LINT_BIN GOVULNCHECK_BIN GOLANGCI_LINT_VERSION GOVULNCHECK_VERSION \
              SELECT_MODULES MODULE_LIST run_in_modules use_source_workspace, \
  $(if $(filter-out undefined,$(origin $(var))),$(error unsupported Go tool override: $(var))))
GO_LINT_MATRIX := scripts/ci/python-runner.sh -- -I scripts/ci/go-lint-matrix.py
ifneq ($(origin MODULES),undefined)
override export MODULES := $(value MODULES)
SELECT_MODULES = set -f; if [ -n "$$MODULES" ] && [ -f "$$MODULES/go.mod" ]; then set -- "$$MODULES"; else set -- $$MODULES; fi
else
SELECT_MODULES = set -- $${GO_MODULE_DIR:+"$$GO_MODULE_DIR"}
endif
MODULE_LIST = $(SELECT_MODULES); if [ -n "$${1-}" ]; then printf '%s\n' "$$@"; else $(GO_LINT_MATRIX) --list-modules; fi

ifeq ($(origin SOURCE_GOWORK),command line)
override export SOURCE_GOWORK := $(value SOURCE_GOWORK)
override use_source_workspace = $(if $(value SOURCE_GOWORK),GOWORK="$$(bash "$(abspath scripts/ci/go-tooling.sh)" workspace-path "$$SOURCE_GOWORK")" &&)
else
override use_source_workspace :=
endif

define run_in_modules
	@set -e; directories="$$($(MODULE_LIST))"; \
	printf '%s\n' "$$directories" | while IFS= read -r module; do (cd -- "$$module" && $(1)); done
endef

.PHONY: lint
lint:
	@set -e; $(SELECT_MODULES); repo=.; if [ "$$#" -eq 1 ]; then repo="$$1"; fi; \
	bin="$$(bash scripts/ci/go-tooling.sh golangci-lint-path "$$repo")"; \
	version="$$(bash scripts/ci/go-tooling.sh golangci-lint-version)"; \
	if [ -n "$${1-}" ]; then set -- --module "$$@"; fi; \
	$(GO_LINT_MATRIX) --golangci-lint "$$bin" --golangci-lint-version "$$version" \
		$(if $(value LINT_GOWORK),--gowork "$$LINT_GOWORK") "$$@"

.PHONY: fmt
fmt:
	$(call run_in_modules,bash "$(abspath scripts/ci/go-tooling.sh)" golangci-lint run -c "$(abspath .golangci.yml)" --fix ./...)

.PHONY: test
test:
	$(call run_in_modules,$(GO) test ./...)

.PHONY: test-race
test-race:
	$(call run_in_modules,$(use_source_workspace) $(GO) test -race -count=1 ./...)

# race 계측은 할당 횟수를 바꾸므로 별도로 측정한다.
.PHONY: test-allocations
test-allocations:
	$(use_source_workspace) $(GO) test -count=1 -run '^Test(PromptGuard|Logging)AllocationCeilings$$' ./pkg/promptguard ./pkg/logging

.PHONY: vulncheck
vulncheck:
	$(call run_in_modules,$(use_source_workspace) bin="$$(bash "$(abspath scripts/ci/go-tooling.sh)" govulncheck-path)" && "$$bin" ./...)

.PHONY: build
build: lint
	$(call run_in_modules,$(GO) build ./...)

.PHONY: tidy
tidy:
	$(call run_in_modules,$(GO) mod tidy)

.PHONY: tidy-check
tidy-check:
	$(call run_in_modules,$(GO) mod tidy -diff)

.PHONY: source-tidy-check
source-tidy-check:
	@$(SELECT_MODULES); bash scripts/ci/go-tooling.sh tidy-workspace "$$@"
