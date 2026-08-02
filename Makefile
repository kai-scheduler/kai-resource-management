# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

GO ?= go

GO_VERSION ?= 1.26.3
GOLANGCI_LINT_VERSION ?= v2.11.3
ADDLICENSE_VERSION ?= v1.2.0
CHANGIE_VERSION ?= v1.25.0

LOCALBIN ?= $(CURDIR)/bin
GOCACHE ?= $(CURDIR)/.gocache
GOTMPDIR ?= $(CURDIR)/.gotmp
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
ADDLICENSE ?= $(LOCALBIN)/addlicense
CHANGIE ?= $(LOCALBIN)/changie

# addlicense does not honor .gitignore. Keep source-like ignored paths here so
# validation remains safe in developer worktrees.
LICENSE_IGNORES := \
	-ignore '.changes/**' \
	-ignore '.changie.yaml' \
	-ignore 'third_party/**' \
	-ignore '.claude/**' \
	-ignore '.codex/**' \
	-ignore '.idea/**' \
	-ignore '.vscode/**' \
	-ignore 'bin/**' \
	-ignore '.gocache/**' \
	-ignore '.gotmp/**' \
	-ignore 'coverage/**' \
	-ignore 'vendor/**' \
	-ignore 'charts/*/charts/**' \
	-ignore 'charts/*/Chart.lock' \
	-ignore '*.test' \
	-ignore 'cover.out' \
	-ignore 'coverage.out' \
	-ignore 'launch.json' \
	-ignore '.DS_Store'

export GOCACHE
export GOTMPDIR

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

$(GOCACHE) $(GOTMPDIR):
	mkdir -p $@

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Install golangci-lint locally.
$(GOLANGCI_LINT): | $(LOCALBIN) $(GOCACHE) $(GOTMPDIR)
	test -s $(GOLANGCI_LINT) || GOBIN=$(LOCALBIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: addlicense
addlicense: $(ADDLICENSE) ## Install addlicense locally.
$(ADDLICENSE): | $(LOCALBIN) $(GOCACHE) $(GOTMPDIR)
	test -s $(ADDLICENSE) || GOBIN=$(LOCALBIN) $(GO) install github.com/google/addlicense@$(ADDLICENSE_VERSION)

.PHONY: changie
changie: $(CHANGIE) ## Install changie locally.
$(CHANGIE): | $(LOCALBIN) $(GOCACHE) $(GOTMPDIR)
	test -s $(CHANGIE) || GOBIN=$(LOCALBIN) $(GO) install github.com/miniscruff/changie@$(CHANGIE_VERSION)

.PHONY: test-chart
test-chart: ## Run Helm chart unit tests in the pinned container.
	@echo "Running tests for Helm chart: kai-resource-management"
	helm dependency build ./charts/kai-resource-management
	docker run -t --rm -v ./charts/kai-resource-management:/apps helmunittest/helm-unittest:3.17.2-0.8.1 . -f 'tests/**/*_test.yaml'

.PHONY: fmt-go
fmt-go: ## Format Go source files.
	find . -type f -name '*.go' -not -path './third_party/*' -exec gofmt -w {} +

.PHONY: fmt-check
fmt-check: ## Verify Go source formatting without changing files.
	@files="$$(find . -type f -name '*.go' -not -path './third_party/*' -exec gofmt -l {} +)"; \
	if [ -n "$$files" ]; then \
		printf 'Go files require formatting:\n%s\n' "$$files"; \
		exit 1; \
	fi

.PHONY: vet-go
vet-go: | $(GOCACHE) $(GOTMPDIR) ## Run go vet for all Go packages.
	@packages="$$( $(GO) list ./... )"; \
	if [ -n "$$packages" ]; then \
		$(GO) vet $$packages; \
	else \
		echo "No Go packages to vet."; \
	fi

.PHONY: lint-go
lint-go: | $(GOCACHE) $(GOTMPDIR) ## Run golangci-lint for all Go packages.
	@packages="$$( $(GO) list ./... )"; \
	if [ -n "$$packages" ]; then \
		$(MAKE) golangci-lint; \
		$(GOLANGCI_LINT) run -c .golangci.yaml; \
	else \
		echo "No Go packages to lint."; \
	fi

.PHONY: lint
lint: fmt-check vet-go lint-go ## Run all static checks.

.PHONY: test
test: test-chart | $(GOCACHE) $(GOTMPDIR) ## Run unit and integration tests; e2e is intentionally separate.
	@all_packages="$$( $(GO) list ./... )"; \
	packages="$$(printf '%s\n' "$$all_packages" | grep -v '/test/e2e' || true)"; \
	if [ -n "$$packages" ]; then \
		$(GO) test $$packages; \
	else \
		echo "No Go packages to test."; \
	fi

.PHONY: mod-check
mod-check: | $(GOCACHE) $(GOTMPDIR) ## Verify go.mod and go.sum are tidy without changing them.
	$(GO) mod tidy -diff

.PHONY: gen-license
gen-license: addlicense ## Add missing Apache-2.0 headers to source and configuration files.
	$(ADDLICENSE) -c "NVIDIA CORPORATION" -s=only -l apache -v \
		$(LICENSE_IGNORES) .

.PHONY: license-check
license-check: addlicense ## Verify Apache-2.0 headers without changing files.
	$(ADDLICENSE) -check -c "NVIDIA CORPORATION" -s=only -l apache \
		$(LICENSE_IGNORES) .

.PHONY: validate
validate: fmt-check mod-check vet-go lint-go test license-check ## Run all repository validation without changing tracked files.

.PHONY: changelog
changelog: changie ## Add a changelog fragment; agents pass KIND and BODY.
	@if [ -n "$(KIND)" ] && [ -n "$(BODY)" ]; then \
		kind_lower=$$(echo "$(KIND)" | tr '[:upper:]' '[:lower:]'); \
		timestamp=$$(date '+%Y%m%d-%H%M%S'); \
		output=".changes/unreleased/$${kind_lower}-$${timestamp}.yaml"; \
		printf 'kind: %s\nbody: |-\n  %s\n' "$(KIND)" "$(BODY)" > "$${output}"; \
		echo "Created $${output}"; \
	elif [ -n "$(KIND)" ] || [ -n "$(BODY)" ]; then \
		echo "Both KIND and BODY must be set for non-interactive mode"; \
		exit 1; \
	else \
		$(CHANGIE) new; \
	fi

.PHONY: changelog-preview
changelog-preview: changie ## Preview a release changelog; requires VERSION.
	@test -n "$(VERSION)" || { echo "VERSION is required, for example VERSION=v0.1.0"; exit 1; }
	$(CHANGIE) batch $(VERSION) --dry-run
