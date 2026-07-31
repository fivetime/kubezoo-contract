SHELL := bash

GO ?= go
ENVTEST_K8S_VERSION ?= 1.36.x
SETUP_ENVTEST_VERSION ?= release-0.24

.DEFAULT_GOAL := test

.PHONY: test-unit
test-unit:
	@$(GO) test ./...

.PHONY: envtest
envtest:
	@GOBIN="$(CURDIR)/bin" $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION)

# The scope table says which kinds are namespaced, and every prefixing decision
# rests on it. The check compares it against a real apiserver's discovery rather
# than against a copy of itself, which is the only way it can catch the table
# going stale. -count=1 because the answer depends on the envtest binaries, not
# just on the sources.
.PHONY: test-integration
test-integration: envtest
	@KUBEBUILDER_ASSETS="$$($(CURDIR)/bin/setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		$(GO) test -count=1 ./pkg/util

.PHONY: test
test: test-unit test-integration
