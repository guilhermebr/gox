# gox is a multi-module repository: the root module plus one module per
# feature package and one for examples. Every target below iterates over all
# of them so `make ci` is the single command CI and contributors run.

MODULES := $(shell find . -name go.mod -not -path './.git/*' -exec dirname {} \; | sort)
LINT_CONFIG := $(CURDIR)/.golangci.yml
GOLANGCI_LINT ?= golangci-lint

define foreach_module
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && $(1)) || exit 1; done
endef

.PHONY: all build test test-integration lint fmt fmt-check vet tidy vulncheck check-deps check-lint-rules generate generate-check llm llm-check check-recipes ci help

TEMPL_VERSION := $(shell grep -E 'github.com/a-h/templ ' web/go.mod | awk '{print $$2}')
TEMPL := go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)

all: ci

## build: compile every package in every module (binaries go to a temp dir)
build:
	$(call foreach_module,go build -o "$$(mktemp -d)" ./...)

## test: run tests with the race detector in every module
test:
	$(call foreach_module,go test ./... -race -count=1)

## test-integration: run tests tagged `integration` (needs DATABASE_URL: a throwaway database the tests own)
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

## generate: regenerate templ code (the templ CLI version is pinned to web/go.mod)
generate:
	@cd examples && $(TEMPL) generate -path ./web
	@$(TEMPL) generate -path ./cmd/gox/internal/scaffold/_template/web

## generate-check: fail if generated templ code is stale
generate-check: generate
	@git diff --exit-code -- '*_templ.go' || (echo "generated templ code is stale: run make generate" && exit 1)

## llm: regenerate llm.txt from source (docs/llm fragments + go/doc)
llm:
	@go run ./cmd/gox docs

## llm-check: fail if llm.txt is stale
llm-check:
	@go run ./cmd/gox docs -check

## check-recipes: every recipe's Go block must build
check-recipes:
	@scripts/check-recipes.sh

## check-deps: prove the import-as-opt-in rule on the example binaries
check-deps:
	@scripts/check-deps.sh examples/minimal absent github.com/jackc/pgx/v5 github.com/supabase-community/supabase-go github.com/workos/workos-go/v10 go.temporal.io/sdk github.com/aws/aws-sdk-go-v2 github.com/mailgun/mailgun-go/v5 github.com/pb33f/libopenapi github.com/riverqueue/river github.com/golang-jwt/jwt/v5 github.com/a-h/templ
	@scripts/check-deps.sh examples/http absent github.com/jackc/pgx/v5 github.com/supabase-community/supabase-go github.com/workos/workos-go/v10 go.temporal.io/sdk github.com/aws/aws-sdk-go-v2 github.com/mailgun/mailgun-go/v5 github.com/pb33f/libopenapi github.com/riverqueue/river github.com/golang-jwt/jwt/v5 github.com/a-h/templ
	@scripts/check-deps.sh examples/postgres present github.com/jackc/pgx/v5
	@scripts/check-deps.sh examples/postgres absent github.com/supabase-community/supabase-go github.com/workos/workos-go/v10 go.temporal.io/sdk github.com/aws/aws-sdk-go-v2 github.com/mailgun/mailgun-go/v5 github.com/pb33f/libopenapi github.com/riverqueue/river github.com/golang-jwt/jwt/v5 github.com/a-h/templ
	@scripts/check-deps.sh examples/web present github.com/a-h/templ
	@scripts/check-deps.sh examples/web absent github.com/jackc/pgx/v5 github.com/supabase-community/supabase-go github.com/workos/workos-go/v10 go.temporal.io/sdk github.com/aws/aws-sdk-go-v2 github.com/mailgun/mailgun-go/v5 github.com/pb33f/libopenapi github.com/riverqueue/river github.com/golang-jwt/jwt/v5

## check-lint-rules: prove the depguard dependency rules fire on a planted violation
check-lint-rules:
	@scripts/check-lint-rules.sh

## ci: the full check suite
ci: fmt-check vet lint test generate-check llm-check check-recipes check-deps check-lint-rules

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | column -t -s ':'
