# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

include build/makefile/index.mk

ADDLICENSE_VERSION ?= v1.2.0
CHANGIE_VERSION ?= v1.25.0

ADDLICENSE ?= $(LOCALBIN)/addlicense
CHANGIE ?= $(LOCALBIN)/changie

# Space-separated list of services to build by default
SERVICE_NAMES := nodepool-controller pod-group-assigner project-controller

# addlicense does not honor .gitignore. Keep source-like ignored paths here so
# validation remains safe in developer worktrees.
LICENSE_IGNORES := \
	-ignore '.changes/**' \
	-ignore '.changie.yaml' \
	-ignore '.claude/**' \
	-ignore '.codex/**' \
	-ignore '.idea/**' \
	-ignore '.vscode/**' \
	-ignore 'bin/**' \
	-ignore '.gocache/**' \
	-ignore '.gotmp/**' \
	-ignore 'coverage/**' \
	-ignore 'vendor/**' \
	-ignore 'deployments/*/charts/**' \
	-ignore 'deployments/*/Chart.lock' \
	-ignore '*.test' \
	-ignore 'cover.out' \
	-ignore 'coverage.out' \
	-ignore 'launch.json' \
	-ignore '.DS_Store'

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

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
	helm dependency build ./deployments/kai-resource-management-chart
	helm lint ./deployments/kai-resource-management-chart
	docker run -t --rm -v ./deployments/kai-resource-management-chart:/apps helmunittest/helm-unittest:3.17.2-0.8.1 . -f 'tests/**/*_test.yaml'

.PHONY: test
test: test-chart test-go ## Run Helm and all non-e2e Go tests.

.PHONY: build
build: $(SERVICE_NAMES) ## Build all configured service images.
	@if [ -z "$(strip $(SERVICE_NAMES))" ]; then \
		echo "No Go services are configured in SERVICE_NAMES."; \
	fi

.PHONY: $(SERVICE_NAMES)
$(SERVICE_NAMES):
	$(MAKE) build-go SERVICE_NAME=$@
	$(MAKE) docker-build-generic SERVICE_NAME=$@

.PHONY: lint
lint: fmt-check vet-go lint-go ## Run all static checks.

.PHONY: gen-license
gen-license: addlicense ## Add missing Apache-2.0 headers to source and configuration files.
	$(ADDLICENSE) -c "NVIDIA CORPORATION" -s=only -l apache -v \
		$(LICENSE_IGNORES) .

.PHONY: license-check
license-check: addlicense ## Verify Apache-2.0 headers without changing files.
	$(ADDLICENSE) -check -c "NVIDIA CORPORATION" -s=only -l apache \
		$(LICENSE_IGNORES) .

.PHONY: validate
validate: mod-check lint test license-check ## Run all repository validation without changing tracked files.

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
