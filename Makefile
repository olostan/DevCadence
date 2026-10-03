# DevCadence developer entry points.
#
# These are the checks contributors, coding agents, hooks, and CI run. Keep the
# policy in these targets so local verification and GitHub Actions cannot drift.

GO ?= go
BIN_DIR ?= bin
COVERAGE_PROFILE ?= coverage.out
COVERAGE_REPORT ?= coverage.txt

.PHONY: all build fmt-check diff-check mod-check test vet race schemas docs-check coverage verify ci precommit prepush hooks-install clean

all: verify

## build: compile the CLI into bin/devcadence.
build:
	$(GO) build -o $(BIN_DIR)/devcadence ./cmd/devcadence

## fmt-check: fail if any tracked Go source is not gofmt-clean.
fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt required for:"; \
		echo "$$out"; \
		exit 1; \
	fi

## diff-check: fail on whitespace errors in the working tree/index diff.
diff-check:
	git diff --check

## mod-check: verify go.mod/go.sum already match go mod tidy.
mod-check:
	$(GO) mod tidy -diff

## test: run the full suite. No model runtime is required.
test:
	$(GO) test -count=1 ./...

## vet: run go vet across the module.
vet:
	$(GO) vet ./...

## race: run the suite under the race detector.
race:
	$(GO) test -race -count=1 ./...

## schemas: validate the published fixtures against the published schemas.
schemas: build
	$(BIN_DIR)/devcadence schema validate fixtures/protocol/*.valid*.json

## docs-check: validate documentation/code drift plus internal links, anchors and normative references.
docs-check:
	$(GO) test -count=1 ./tests -run 'Test(DocumentedEventTypesAreImplemented|RepositoryMarkdownLinksAndAnchors|DocumentationDCIReferencesResolve|DocumentationADRReferencesResolve)'

## coverage: run the suite with whole-module statement coverage and write a readable report.
coverage:
	$(GO) test -count=1 -covermode=atomic -coverpkg=./... -coverprofile=$(COVERAGE_PROFILE) ./...
	$(GO) tool cover -func=$(COVERAGE_PROFILE) | tee $(COVERAGE_REPORT)

## verify: deterministic repository verification suitable for normal development.
verify: fmt-check diff-check mod-check vet test schemas docs-check

## ci: full local equivalent of the blocking CI health gate.
ci: verify race coverage

## precommit: validate the exact staged snapshot against HEAD, including coverage regression.
precommit:
	sh scripts/health/precommit.sh

## prepush: validate committed HEAD with the full CI-equivalent gate.
prepush:
	sh scripts/health/prepush.sh

## hooks-install: install tiny Git hooks that delegate to versioned repository policy.
hooks-install:
	@hooks_dir="$$(git rev-parse --git-path hooks)"; \
	mkdir -p "$$hooks_dir"; \
	cp .githooks/pre-commit "$$hooks_dir/pre-commit"; \
	cp .githooks/pre-push "$$hooks_dir/pre-push"; \
	chmod +x "$$hooks_dir/pre-commit" "$$hooks_dir/pre-push"; \
	echo "Installed DevCadence hooks in $$hooks_dir"

clean:
	rm -rf $(BIN_DIR) $(COVERAGE_PROFILE) $(COVERAGE_REPORT)
