# DevCadence developer entry points.
#
# These are the checks contributors, coding agents, hooks, and CI run. Keep the
# policy in these targets so local verification and GitHub Actions cannot drift.

GO ?= go
BIN_DIR ?= bin
COVERAGE_PROFILE ?= coverage.out
COVERAGE_REPORT ?= coverage.txt

.PHONY: all build hooks-check fmt-check diff-check mod-check test vet race schemas docs-check coverage verify ci precommit prepush hooks-install clean

all: verify

## hooks-check: warn (never fail) when this clone has not enabled the repository hooks.
hooks-check:
	@if [ -z "$$CI" ] && git rev-parse --git-dir >/dev/null 2>&1 && \
	   [ "$$(git config --get core.hooksPath)" != ".githooks" ]; then \
		echo "warning: repository health hooks are not enabled in this clone; run 'make hooks-install' once" >&2; \
	fi

## build: compile the CLI into bin/devcadence.
build: hooks-check
	$(GO) build -o $(BIN_DIR)/devcadence ./cmd/devcadence

## fmt-check: fail if Go files changed from the selected merge base are not gofmt-clean.
fmt-check:
	@base="$(FMT_BASE)"; \
	if [ -z "$$base" ] && git rev-parse --verify origin/main >/dev/null 2>&1; then \
		base="$$(git merge-base HEAD origin/main)"; \
	fi; \
	if [ -n "$$base" ]; then \
		files="$$(git diff --name-only --diff-filter=ACMR "$$base...HEAD" -- '*.go')"; \
	else \
		files="$$(git diff --name-only --diff-filter=ACMR HEAD -- '*.go')"; \
	fi; \
	if [ -z "$$files" ]; then exit 0; fi; \
	out="$$(printf '%s\n' "$$files" | xargs gofmt -l)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt required for changed files:"; \
		echo "$$out"; \
		exit 1; \
	fi

## diff-check: fail on whitespace errors. Set DIFF_BASE in clean CI checkouts.
diff-check:
	@if [ -n "$(DIFF_BASE)" ]; then \
		git diff --check "$(DIFF_BASE)...HEAD"; \
	else \
		git diff --check; \
	fi

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
verify: hooks-check fmt-check diff-check mod-check vet test schemas docs-check

## ci: full local equivalent of the blocking CI health gate. The coverage run
## executes the whole suite, so the plain test target is not repeated.
ci: fmt-check diff-check mod-check vet schemas docs-check race coverage

## precommit: validate the exact staged snapshot against HEAD, including coverage regression.
precommit:
	sh scripts/health/precommit.sh

## prepush: validate committed HEAD with the full CI-equivalent gate.
prepush:
	sh scripts/health/prepush.sh

## hooks-install: enable the versioned Git hooks for this clone (core.hooksPath=.githooks).
hooks-install:
	@chmod +x .githooks/pre-commit .githooks/pre-push; \
	git config core.hooksPath .githooks; \
	hooks_dir="$$(git rev-parse --git-common-dir)/hooks"; \
	for hook in pre-commit pre-push; do \
		if [ -f "$$hooks_dir/$$hook" ]; then \
			echo "note: $$hooks_dir/$$hook is ignored while core.hooksPath=.githooks; remove it if it is not needed" >&2; \
		fi; \
	done; \
	echo "Enabled DevCadence hooks: core.hooksPath=.githooks (pre-commit, pre-push). Hook updates apply on git pull."

clean:
	rm -rf $(BIN_DIR) $(COVERAGE_PROFILE) $(COVERAGE_REPORT)
