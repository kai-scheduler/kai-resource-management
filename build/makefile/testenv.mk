# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

ENVTEST_K8S_VERSION ?= 1.35.0
ENVTEST_VERSION ?= release-0.23
ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: envtest-docker-go
envtest-docker-go: gocache
	@if [ -n "$(strip $(TEST_TARGETS))" ]; then \
		$(MAKE) builder; \
		$(DOCKER_GO_COMMAND) make envtest-go TEST_TARGETS="$(TEST_TARGETS)" || { \
			$(ECHO_COMMAND) $(RED_CONSOLE) "$(CONSOLE_PREFIX) Failed to run Go tests" $(BASE_CONSOLE); \
			exit 1; \
		}; \
		$(SUCCESS_MESSAGE_HANDLER); \
	else \
		echo "No Go packages to test."; \
	fi

.PHONY: envtest-go
envtest-go: envtest
	@$(ECHO_COMMAND) $(GREEN_CONSOLE) "$(CONSOLE_PREFIX) Running unit tests" $(BASE_CONSOLE)
	mkdir -p coverage
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path --bin-dir $(LOCALBIN))" \
		$(GO) test $(TEST_TARGETS) -timeout 30m -coverprofile=coverage/coverage.out || $(FAILURE_MESSAGE_HANDLER)
	$(SUCCESS_MESSAGE_HANDLER)

.PHONY: envtest
envtest: ## Install setup-envtest locally.
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)
