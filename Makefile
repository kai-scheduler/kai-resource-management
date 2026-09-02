# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

include build/makefile/index.mk

ADDLICENSE_VERSION ?= v1.2.0
CHANGIE_VERSION ?= v1.25.0

ADDLICENSE ?= $(LOCALBIN)/addlicense
CHANGIE ?= $(LOCALBIN)/changie

# Space-separated list of services to build by default
SERVICE_NAMES := nodepool-controller pod-group-assigner project-controller krm-operator

# CRDs are copied from the pinned API module; nothing here generates them.
# See docs/updating-the-api-module.md.
API_MODULE := github.com/kai-scheduler/kai-resource-management-api
API_CRD_DIR = $(shell $(GO) list -m -f '{{.Dir}}' $(API_MODULE))/config/crd
CHART_CRD_DIR := deployments/kai-resource-management-chart/crds
CRD_MANAGER_ROLE := deployments/kai-resource-management-chart/templates/rbac/crd-manager.yaml
CHART_DIR := deployments/kai-resource-management-chart

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

.PHONY: helm-deps
helm-deps: ## Fetch the chart's declared subchart dependencies into charts/.
	helm dependency build $(CHART_DIR)

.PHONY: test-chart
test-chart: helm-deps ## Run Helm chart unit tests in the pinned container.
	@echo "Running tests for Helm chart: kai-resource-management"
	helm lint ./deployments/kai-resource-management-chart
	docker run -t --rm -v ./deployments/kai-resource-management-chart:/apps helmunittest/helm-unittest:3.17.2-0.8.1 . -f 'tests/**/*_test.yaml'

.PHONY: test
test: test-chart test-go ## Run Helm and all non-e2e Go tests.

.PHONY: build
build: $(SERVICE_NAMES) ## Build all configured service images.
	$(MAKE) docker-build-helm-hooks

.PHONY: $(SERVICE_NAMES)
$(SERVICE_NAMES):
	$(MAKE) build-go SERVICE_NAME=$@
	$(MAKE) docker-build-generic SERVICE_NAME=$@

.PHONY: lint
lint: fmt-check lint-go ## Run all static checks.

.PHONY: gen-license
gen-license: addlicense ## Add missing Apache-2.0 headers to source and configuration files.
	$(ADDLICENSE) -c "NVIDIA CORPORATION" -s=only -l apache -v \
		$(LICENSE_IGNORES) .

.PHONY: license-check
license-check: addlicense ## Verify Apache-2.0 headers without changing files.
	$(ADDLICENSE) -check -c "NVIDIA CORPORATION" -s=only -l apache \
		$(LICENSE_IGNORES) .

.PHONY: notice
notice: ## Regenerate the third-party attribution in NOTICE from the linked modules.
	python3 hack/gen-notice.py

.PHONY: notice-check
notice-check: ## Verify NOTICE matches the modules linked into the binaries.
	python3 hack/gen-notice.py --check

.PHONY: sync-crds
sync-crds: ## Copy CRD manifests from the pinned API module into the chart.
	cp $(API_CRD_DIR)/*.yaml $(CHART_CRD_DIR)/
	@# The module cache is read-only, so the copies land unwritable and the
	@# next sync would fail with "Permission denied".
	chmod u+w $(CHART_CRD_DIR)/*.yaml

.PHONY: sync-crds-check
sync-crds-check: ## Verify the chart CRDs match the pinned API module.
	@# Compares against a temporary copy rather than syncing first, so a
	@# failure never leaves modified files behind. diff -r also reports CRDs
	@# added or removed by the API module, which a content-only check misses.
	@tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
	cp $(API_CRD_DIR)/*.yaml "$$tmp/"; \
	if ! diff -ru "$$tmp" $(CHART_CRD_DIR); then \
		echo "::error::Chart CRDs are out of sync with $(API_MODULE). Run 'make sync-crds' and commit the result."; \
		exit 1; \
	fi

.PHONY: crd-rbac-check
crd-rbac-check: ## Verify every chart CRD is named in the crd-manager ClusterRole.
	@# The pre-install hook applies the CRDs under a ClusterRole restricted by
	@# resourceNames. A CRD added by a future API module release would sync in
	@# here but fail to apply without a matching entry.
	@rc=0; \
	for name in $$(awk '/^metadata:/{m=1;next} m&&/^  name: /{print $$2; m=0}' $(CHART_CRD_DIR)/*.yaml); do \
		grep -q -- "- $$name" $(CRD_MANAGER_ROLE) || { \
			echo "::error::$$name is missing from resourceNames in $(CRD_MANAGER_ROLE)"; rc=1; }; \
	done; \
	exit $$rc

.PHONY: scc-check
scc-check: helm-deps ## Verify every ServiceAccount the chart renders is granted the OpenShift SCC.
	bash hack/scc-check.sh $(CHART_DIR)

.PHONY: validate
validate: mod-check lint license-check notice-check sync-crds-check crd-rbac-check scc-check ## Run all repository validation without changing tracked files; tests are separate.

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

.PHONY: changelog-release
changelog-release: changie ## Fold unreleased fragments into CHANGELOG.md; requires VERSION.
	@test -n "$(VERSION)" || { echo "VERSION is required, for example VERSION=v0.1.0"; exit 1; }
	CHANGIE=$(CHANGIE) bash hack/changelog-fold.sh $(VERSION)

.PHONY: changelog-preview
changelog-preview: changie ## Preview a release changelog; requires VERSION.
	@test -n "$(VERSION)" || { echo "VERSION is required, for example VERSION=v0.1.0"; exit 1; }
	$(CHANGIE) batch $(VERSION) --dry-run
