MODULES := http jwt logger monetary osrelease postgres supabase

.PHONY: all test test-race fmt fmt-check vet lint gosec vulncheck tidy ci

all: fmt-check vet lint test-race

## test: run tests for every module
test:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && go test ./... -count=1) || exit 1; done

## test-race: run tests with the race detector for every module
test-race:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && go test ./... -race -count=1) || exit 1; done

## fmt: format every module
fmt:
	@for m in $(MODULES); do (cd $$m && gofmt -w .); done

## fmt-check: fail if any file needs formatting
fmt-check:
	@fail=0; for m in $(MODULES); do out=$$(cd $$m && gofmt -l .); if [ -n "$$out" ]; then echo "$$m: $$out"; fail=1; fi; done; exit $$fail

## vet: run go vet for every module
vet:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && go vet ./...) || exit 1; done

## lint: run golangci-lint for every module
lint:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && golangci-lint run ./...) || exit 1; done

## gosec: run gosec for every module
gosec:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && gosec -quiet ./...) || exit 1; done

## vulncheck: run govulncheck for every module
vulncheck:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && govulncheck ./...) || exit 1; done

## tidy: run go mod tidy for every module
tidy:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && go mod tidy) || exit 1; done

## ci: the full check suite
ci: fmt-check vet lint test-race
