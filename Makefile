# DevCadence developer entry points.
#
# These are the checks a contributor runs locally and the ones a milestone
# verification gate runs. Keep them boring: the Makefile exists so that
# "what do I run?" has one answer, not so that it becomes a build system.

GO ?= go
BIN_DIR ?= bin

.PHONY: all build test vet race verify schemas clean

all: verify

## build: compile the CLI into bin/devcadence.
build:
	$(GO) build -o $(BIN_DIR)/devcadence ./cmd/devcadence

## test: run the full suite. No model runtime is required.
test:
	$(GO) test ./...

## vet: run go vet across the module.
vet:
	$(GO) vet ./...

## race: run the suite under the race detector.
race:
	$(GO) test -race ./...

## schemas: validate the published fixtures against the published schemas.
schemas: build
	$(BIN_DIR)/devcadence schema validate fixtures/protocol/*.valid*.json

## verify: the milestone verification gate.
verify: vet test schemas

clean:
	rm -rf $(BIN_DIR)
