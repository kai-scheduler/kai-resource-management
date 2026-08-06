# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

GO ?= go
GO_VERSION ?= 1.26.3
GO_IMAGE_VERSION ?= $(GO_VERSION)-bookworm
GOLANGCI_LINT_VERSION ?= v2.11.3
CGO_ENABLED ?= 1
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
# --- BEGIN private API module access ---------------------------------------
# TEMPORARY: needed only while kai-resource-management-api is a private
# repository. The containers resolve modules against their own GOPATH volume,
# not the host module cache, so they must be able to fetch it themselves.
# Remove both blocks once the repositories are public.
ifneq ($(GOPRIVATE),)
DOCKER_GO_CACHING_VOLUME_AND_ENV += -e GOPRIVATE=$(GOPRIVATE)
endif
# Credentials travel via GIT_CONFIG_GLOBAL rather than $(HOME)/.gitconfig: the
# container runs as a numeric uid with no passwd entry, so HOME is "/" and git
# would look for "//.gitconfig". Mounted read-only outside the repo mount so a
# credential can never land in the checkout.
ifneq ($(GIT_CONFIG_GLOBAL),)
DOCKER_GO_CACHING_VOLUME_AND_ENV += -v $(GIT_CONFIG_GLOBAL):/tmp/gitconfig:ro -e GIT_CONFIG_GLOBAL=/tmp/gitconfig
endif
# --- END private API module access -----------------------------------------

DOCKER_GO_BASE_COMMAND = $(DOCKER_COMMAND) -e CGO_ENABLED=$(CGO_ENABLED) -e GO111MODULE=on $(DOCKER_GO_CACHING_VOLUME_AND_ENV)
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

$(GOCACHE) $(GOTMPDIR):
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
vet-go:
	@packages="$$( $(GO) list ./... )"; \
	if [ -n "$$packages" ]; then \
		$(GO) vet ./...; \
	else \
		echo "No Go packages to vet."; \
	fi

.PHONY: test-go
test-go: ## Run Go tests directly; optionally set TEST_TARGETS=./pkg/<name>/....
	@if [ -n "$(strip $(TEST_TARGETS))" ]; then \
		$(GO) test $(TEST_TARGETS); \
	else \
		echo "No Go packages to test."; \
	fi

.PHONY: lint-go
lint-go: gocache ## Run golangci-lint for all Go packages.
	@packages="$$( $(GO) list ./... )"; \
	if [ -n "$$packages" ]; then \
		$(DOCKER_GO_LINTER_COMMAND) golangci-lint run -v -c $(GOLANGCI_LINTER_CONFIG_PATH) || $(FAILURE_MESSAGE_HANDLER); \
		$(SUCCESS_MESSAGE_HANDLER); \
	else \
		echo "No Go packages to lint."; \
	fi

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
