PROJECT_FULL_NAME := quota-operator
REPO_ROOT := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
EFFECTIVE_VERSION := $(shell $(REPO_ROOT)/hack/common/get-version.sh)

COMMON_MAKEFILE ?= $(REPO_ROOT)/hack/common/Makefile
ifneq (,$(wildcard $(COMMON_MAKEFILE)))
include $(COMMON_MAKEFILE)
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

COMPONENTS ?= quota-operator
API_CODE_DIRS := $(REPO_ROOT)/api/...
ROOT_CODE_DIRS := $(REPO_ROOT)/cmd/... $(REPO_ROOT)/pkg/...

##@ General

ifndef HELP_TARGET
.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
endif

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate CustomResourceDefinition objects.
	@echo "> Remove existing CRD manifests"
	@rm -rf api/crds/manifests/
	@rm -rf config/crd/bases/
	@echo "> Generating CRD Manifests"
	@$(CONTROLLER_GEN) crd paths="$(REPO_ROOT)/api/..." output:crd:artifacts:config=api/crds/manifests
	@$(CONTROLLER_GEN) crd paths="$(REPO_ROOT)/api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: generate-code manifests format ## Generates code (DeepCopy stuff, CRDs), documentation index, and runs formatter.

.PHONY: generate-code
generate-code: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations. Also fetches external APIs.
	@echo "> Generating DeepCopy Methods"
	@$(CONTROLLER_GEN) object paths="$(REPO_ROOT)/api/..."

.PHONY: format
format: goimports ## Formats the imports.
	@FORMATTER=$(FORMATTER) $(REPO_ROOT)/hack/common/format.sh $(API_CODE_DIRS) $(ROOT_CODE_DIRS)

.PHONY: verify
verify: golangci-lint goimports ## Runs linter, 'go vet', and checks if the formatter has been run.
	@( echo "> Verifying api module ..." && \
		pushd $(REPO_ROOT)/api &>/dev/null && \
		go vet $(API_CODE_DIRS) && \
		$(LINTER) run -c $(REPO_ROOT)/.golangci.yaml $(API_CODE_DIRS) && \
		popd &>/dev/null )
	@( echo "> Verifying root module ..." && \
		pushd $(REPO_ROOT) &>/dev/null && \
		go vet $(ROOT_CODE_DIRS) && \
		$(LINTER) run -c $(REPO_ROOT)/.golangci.yaml $(ROOT_CODE_DIRS) && \
		popd &>/dev/null )
	@test "$(SKIP_FORMATTING_CHECK)" = "true" || \
		( echo "> Checking for unformatted files ..." && \
		FORMATTER=$(FORMATTER) $(REPO_ROOT)/hack/common/format.sh --verify $(API_CODE_DIRS) $(ROOT_CODE_DIRS) )

.PHONY: test
test: ## Run tests.
	go test $(ROOT_CODE_DIRS) -coverprofile cover.out
	go tool cover --html=cover.out -o cover.html
	go tool cover -func cover.out | tail -n 1
