# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

ARCH ?= $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')

RED_CONSOLE = "\033[0;31m"
GREEN_CONSOLE = "\033[0;32m"
BASE_CONSOLE = "\033[0m"
CONSOLE_PREFIX = "[$(shell date +'%Y-%m-%d %H:%M:%S')]"
# Braces, not a subshell: exit 1 must leave the recipe's own shell.
FAILURE_MESSAGE_HANDLER = { $(ECHO_COMMAND) $(RED_CONSOLE) "$(CONSOLE_PREFIX) Failed to run make target $@" $(BASE_CONSOLE); exit 1; }
SUCCESS_MESSAGE_HANDLER = ($(ECHO_COMMAND) $(GREEN_CONSOLE) "$(CONSOLE_PREFIX) Successfully ran make target" $(BASE_CONSOLE))

DOCKER_SOCK_PATH = /var/run/docker.sock
DOCKERFILE_PATH ?= ./Dockerfile
HELM_HOOKS_DOCKERFILE_PATH = ./deployments/helm-hooks/Dockerfile

DOCKER_TAG ?= 0.0.0
VERSION ?= $(DOCKER_TAG)

# FIPS images are published alongside the regular ones under the same version,
# distinguished only by this suffix. Overrides rather than defaults VERSION so
# the suffix survives an explicit VERSION= on the command line, which is how CI
# passes the release tag. Must stay above DOCKER_IMAGE_NAME.
FIPS ?= 0
ifeq ($(FIPS), 1)
override VERSION := $(VERSION)-fips
endif

DOCKER_REPO_BASE ?= registry/local/kai-resource-management
DOCKER_REPO_FULL ?= $(DOCKER_REPO_BASE)/$(SERVICE_NAME)
DOCKER_IMAGE_NAME ?= $(DOCKER_REPO_FULL):$(VERSION)
DOCKER_BUILD_PLATFORM ?= linux/$(ARCH)

ifeq ($(DEBUG), 1)
DOCKER_BUILD_ADDITIONAL_ARGS = --target debug
else
DOCKER_BUILD_ADDITIONAL_ARGS = --target prod
endif

DOCKER_BUILDX_ADDITIONAL_ARGS ?=

################### OS DEPENDENCY ##########################
# Build the project
UNAME_S := $(shell uname -s)

ifeq ($(UNAME_S),Linux)
ECHO_COMMAND = echo -e
OS = linux


else ifeq ($(UNAME_S),Darwin)
ECHO_COMMAND = echo
OS ?= darwin

else
$(error "The OS is not supported")
endif

## Commands
DOCKER_WORK_DIR ?= /local
DOCKER_COMMAND = docker run --rm -w $(DOCKER_WORK_DIR) -v "$(CURDIR)/:/local:z" -u $(shell id -u):$(shell id -g)

### Targets
.PHONY: builder
builder: ## Build the pinned Go builder image.
	DOCKER_BUILDKIT=1 docker buildx build -f build/builder/Dockerfile --load -t builder:$(GO_IMAGE_VERSION) .

.PHONY: docker-build-generic
docker-build-generic: ## Build one service image; requires SERVICE_NAME=<cmd-directory>.
	DOCKER_BUILDKIT=1 docker buildx build $(DOCKER_BUILD_ADDITIONAL_ARGS) --build-arg SERVICE_NAME=$(SERVICE_NAME) -f $(DOCKERFILE_PATH) -t $(DOCKER_IMAGE_NAME) $(DOCKER_BUILDX_ADDITIONAL_ARGS) --platform $(DOCKER_BUILD_PLATFORM) .

.PHONY: docker-build-helm-hooks
docker-build-helm-hooks: ## Build the shared Helm hook image (kubectl + the packaged CRDs).
	$(MAKE) docker-build-generic DOCKERFILE_PATH=$(HELM_HOOKS_DOCKERFILE_PATH) DOCKER_BUILD_ADDITIONAL_ARGS="" SERVICE_NAME=helm-hooks
