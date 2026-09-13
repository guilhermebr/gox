# gox is a multi-module repository: the root module plus one module per
# feature package and one for examples. Every target below iterates over all
# of them so `make ci` is the single command CI and contributors run.

MODULES := $(shell find . -name go.mod -not -path './.git/*' -exec dirname {} \; | sort)
LINT_CONFIG := $(CURDIR)/.golangci.yml
GOLANGCI_LINT ?= golangci-lint

define foreach_module
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && $(1)) || exit 1; done
endef

.PHONY: all build test test-integration lint fmt fmt-check vet tidy vulncheck check-deps check-lint-rules ci help

all: ci

## build: compile every package in every module (binaries go to a temp dir)
build:
	$(call foreach_module,go build -o "$$(mktemp -d)" ./...)

## test: run tests with the race detector in every module
test:
	$(call foreach_module,go test ./... -race -count=1)

## test-integration: run tests tagged `integration` (needs DATABASE_URL or a container runtime)
test-integration:
	$(call foreach_module,go test ./... -race -count=1 -tags integration)

## lint: run golangci-lint with the shared config in every module
lint:
	$(call foreach_module,$(GOLANGCI_LINT) run --config $(LINT_CONFIG) ./...)

## fmt: format every module (gofumpt + goimports via golangci-lint)
fmt:
	$(call foreach_module,$(GOLANGCI_LINT) fmt --config $(LINT_CONFIG) ./...)

## fmt-check: fail if any file needs formatting
fmt-check:
	$(call foreach_module,$(GOLANGCI_LINT) fmt --config $(LINT_CONFIG) --diff ./...)

## vet: run go vet in every module
vet:
	$(call foreach_module,go vet ./...)

## tidy: run go mod tidy in every module
tidy:
	$(call foreach_module,GOWORK=off go mod tidy)

## vulncheck: run govulncheck in every module
vulncheck:
	$(call foreach_module,govulncheck ./...)

## check-deps: prove the import-as-opt-in rule on the example binaries
check-deps:
	@scripts/check-deps.sh examples/minimal absent github.com/jackc/pgx/v5 github.com/supabase-community/supabase-go github.com/golang-jwt/jwt/v5 github.com/a-h/templ

## check-lint-rules: prove the depguard dependency rules fire on a planted violation
check-lint-rules:
	@scripts/check-lint-rules.sh

## ci: the full check suite
ci: fmt-check vet lint test check-deps check-lint-rules

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | column -t -s ':'
