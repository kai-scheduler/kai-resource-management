# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

GO ?= go
GO_VERSION ?= 1.26.3
GO_IMAGE_VERSION ?= $(GO_VERSION)-bookworm
GOLANGCI_LINT_VERSION ?= v2.11.3
CGO_ENABLED ?= 1
GOFIPS140_VERSION ?= v1.0.0
E2E_TESTS_DIR ?= test/e2e/
TEST_TARGETS ?= $(shell $(GO) list ./... | grep -v "$(E2E_TESTS_DIR)")

LOCALBIN ?= $(CURDIR)/bin
BUILD_DIR ?= bin
BUILD_OUT_PATH_AMD = $(BUILD_DIR)/$(SERVICE_NAME)-amd64
BUILD_OUT_PATH_ARM = $(BUILD_DIR)/$(SERVICE_NAME)-arm64
BUILD_IN_PATH = cmd/$(SERVICE_NAME)/main.go
GOLANGCI_LINTER_CONFIG_PATH ?= .golangci.yaml

GOCACHE ?= $(CURDIR)/.gocache
GOTMPDIR ?= $(CURDIR)/.gotmp
COVERAGE_DIR ?= $(CURDIR)/coverage
COVERAGE_PROFILE ?= $(COVERAGE_DIR)/coverage.out
# Many tests live in a sibling _test package, so instrumenting only the package under test
# would report most of the code as uncovered. cmd is left out: it is process wiring.
COVERPKG ?= ./pkg/...
GOCACHE_DOCKER_DIR ?= /tmp/.cache
GOCACHE_HOST_DIR ?= $(HOME)/.cache/go-build-docker-gocache
GOPATH_HOST_DIR ?= $(HOME)/.cache/go-build-docker-gopath

export GOCACHE
export GOTMPDIR

DOCKER_GO_CACHING_VOLUME_AND_ENV = -v $(GOPATH_HOST_DIR):/go:z -v $(GOCACHE_HOST_DIR):$(GOCACHE_DOCKER_DIR):z -e GOPATH=/go -e GOCACHE=$(GOCACHE_DOCKER_DIR) -e GOLANGCI_LINT_CACHE=$(GOCACHE_DOCKER_DIR)
ifneq ($(GOPROXY),)
DOCKER_GO_CACHING_VOLUME_AND_ENV += -e GOPROXY=$(GOPROXY)
endif
ifneq ($(GOSUMDB),)
DOCKER_GO_CACHING_VOLUME_AND_ENV += -e GOSUMDB=$(GOSUMDB)
endif

DOCKER_GO_BASE_COMMAND = $(DOCKER_COMMAND) -e CGO_ENABLED=$(CGO_ENABLED) -e GO111MODULE=on $(DOCKER_GO_CACHING_VOLUME_AND_ENV)
# Links the binaries against the CMVP-validated Go Cryptographic Module. The
# module ships inside the toolchain and is pure Go, so neither the builder image
# nor the runtime base image needs a FIPS variant. GODEBUG=fips140 then selects
# how strictly it is used at run time; see docs/fips.md.
ifeq ($(FIPS), 1)
DOCKER_GO_BASE_COMMAND += -e GOFIPS140=$(GOFIPS140_VERSION)
endif

GO_ENV_ARCH_AMD = -e GOOS=linux -e GOARCH=amd64 -e CC=x86_64-linux-gnu-gcc -e CXX=x86_64-linux-gnu-g++
GO_ENV_ARCH_ARM = -e GOOS=linux -e GOARCH=arm64 -e CC=aarch64-linux-gnu-gcc -e CXX=aarch64-linux-gnu-g++
DOCKER_GO_COMMAND = $(DOCKER_GO_BASE_COMMAND) builder:$(GO_IMAGE_VERSION)
DOCKER_GO_COMMAND_AMD = $(DOCKER_GO_BASE_COMMAND) $(GO_ENV_ARCH_AMD) builder:$(GO_IMAGE_VERSION)
DOCKER_GO_COMMAND_ARM = $(DOCKER_GO_BASE_COMMAND) $(GO_ENV_ARCH_ARM) builder:$(GO_IMAGE_VERSION)
DOCKER_GO_LINTER_COMMAND = $(DOCKER_GO_BASE_COMMAND) -e GOFLAGS="-buildvcs=false" golangci/golangci-lint:$(GOLANGCI_LINT_VERSION)

ifeq ($(DEBUG),1)
GO_BUILD_ADDITIONAL_FLAGS = -gcflags="all=-N -l"
else
GO_BUILD_ADDITIONAL_FLAGS =
endif

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

$(GOCACHE) $(GOTMPDIR) $(COVERAGE_DIR):
	mkdir -p $@

.PHONY: gocache
gocache:
	mkdir -p $(GOCACHE_HOST_DIR)
	mkdir -p $(GOPATH_HOST_DIR)

.PHONY: fmt-go
fmt-go: ## Format Go source files.
	go fmt ./...

.PHONY: fmt-check
fmt-check: ## Verify Go source formatting without changing files.
	@source_files="$$(git ls-files --cached --others --exclude-standard -- '*.go')"; \
	if [ -n "$$source_files" ]; then \
		unformatted="$$(gofmt -l $$source_files)"; \
		if [ -n "$$unformatted" ]; then \
			printf 'Go files require formatting:\n%s\n' "$$unformatted"; \
			exit 1; \
		fi; \
	fi

.PHONY: vet-go
vet-go: | $(GOCACHE) $(GOTMPDIR)
	@packages="$$( $(GO) list ./... )"; \
	if [ -n "$$packages" ]; then \
		$(GO) vet ./...; \
	else \
		echo "No Go packages to vet."; \
	fi

# atomic rather than the default set mode: counts stay correct across parallel tests.
.PHONY: test-go
test-go: | $(GOCACHE) $(GOTMPDIR) $(COVERAGE_DIR) ## Run Go tests directly; optionally set TEST_TARGETS=./pkg/<name>/....
	@if [ -n "$(strip $(TEST_TARGETS))" ]; then \
		$(GO) test -covermode=atomic -coverpkg=$(COVERPKG) -coverprofile=$(COVERAGE_PROFILE) $(TEST_TARGETS); \
	else \
		echo "No Go packages to test."; \
	fi

.PHONY: coverage
coverage: test-go ## Print total statement coverage.
	@$(GO) tool cover -func=$(COVERAGE_PROFILE) | tail -1

# Kept out of `make test`: these mutate whatever KUBECONFIG points at.
.PHONY: test-e2e
test-e2e: | $(GOCACHE) $(GOTMPDIR) ## Run e2e suites against the current KUBECONFIG; destructive.
	$(GO) run github.com/onsi/ginkgo/v2/ginkgo -r --keep-going --randomize-all \
		--randomize-suites --trace -vv $(GINKGO_FLAGS) ./test/e2e/suites

.PHONY: lint-go
lint-go: gocache | $(GOCACHE) $(GOTMPDIR) ## Run golangci-lint for all Go packages.
	@packages="$$( $(GO) list ./... )"; \
	if [ -n "$$packages" ]; then \
		$(DOCKER_GO_LINTER_COMMAND) golangci-lint run -v -c $(GOLANGCI_LINTER_CONFIG_PATH) || $(FAILURE_MESSAGE_HANDLER); \
		$(SUCCESS_MESSAGE_HANDLER); \
	else \
		echo "No Go packages to lint."; \
	fi

.PHONY: lint-go-host
lint-go-host: | $(GOCACHE) $(GOTMPDIR) ## Run the pinned golangci-lint on the host toolchain, without Docker.
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) \
		run -c $(GOLANGCI_LINTER_CONFIG_PATH)

.PHONY: mod-check
mod-check: | $(GOCACHE) $(GOTMPDIR) ## Verify go.mod and go.sum are tidy without changing them.
	$(GO) mod tidy -diff

.PHONY: build-go build-go-amd build-go-arm
build-go: builder build-go-amd build-go-arm ## Build one service for amd64 and arm64; requires SERVICE_NAME=<cmd-directory>.

build-go-amd: gocache
	@$(ECHO_COMMAND) $(GREEN_CONSOLE) "$(CONSOLE_PREFIX) Building $(SERVICE_NAME), GOOS: linux, GOARCH: amd64" $(BASE_CONSOLE)
	$(DOCKER_GO_COMMAND_AMD) go build -buildvcs=false $(GO_BUILD_ADDITIONAL_FLAGS) -o $(BUILD_OUT_PATH_AMD) $(BUILD_IN_PATH) || $(FAILURE_MESSAGE_HANDLER)
	$(SUCCESS_MESSAGE_HANDLER)

build-go-arm: gocache
	@$(ECHO_COMMAND) $(GREEN_CONSOLE) "$(CONSOLE_PREFIX) Building $(SERVICE_NAME), GOOS: linux, GOARCH: arm64" $(BASE_CONSOLE)
	$(DOCKER_GO_COMMAND_ARM) go build -buildvcs=false $(GO_BUILD_ADDITIONAL_FLAGS) -o $(BUILD_OUT_PATH_ARM) $(BUILD_IN_PATH) || $(FAILURE_MESSAGE_HANDLER)
	$(SUCCESS_MESSAGE_HANDLER)
