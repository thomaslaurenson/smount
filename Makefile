SHELL := /bin/bash

BINARY  := smount
MODULE  := github.com/thomaslaurenson/smount
VERSION := $(shell git describe --tags --always --dirty --match 'v*' 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X $(MODULE)/cmd.Version=$(VERSION)

TAG ?= $(shell git describe --tags --abbrev=0 --match 'v*' 2>/dev/null)

.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

# BUILD
.PHONY: build
build: ## Build the binary for the current platform
	go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY) .

.PHONY: snapshot
snapshot: ## Build binaries for every platform with goreleaser
	goreleaser release --snapshot --clean

# LINT
.PHONY: format
format: ## Format Go source files
	gofmt -w .

.PHONY: check_format
check_format: ## Fail if any Go source file is unformatted
	@out="$$(gofmt -l .)"; \
	if [[ -n "$$out" ]]; then \
	  printf 'Unformatted Go files:\n%s\n' "$$out"; \
	  exit 1; \
	fi

.PHONY: check_mod
check_mod: ## Fail if go.mod or go.sum is untidy
	go mod tidy
	git diff --exit-code go.mod go.sum

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: check_all
check_all: check_format check_mod vet ## Run every static check

.PHONY: vuln
vuln: ## Scan dependencies and the standard library for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# TEST
.PHONY: test
test: ## Run all tests with the race detector
	go test -race -count=1 ./...

.PHONY: test_coverage
test_coverage: ## Report test coverage over the internal packages
	go test -race -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	rm coverage.out

# GET
.PHONY: get_changelog
get_changelog: ## Print release notes for TAG (default: latest tag; override with TAG=v1.0.0)
	@tag="$(TAG)"; tag="$${tag#v}"; \
	if [[ -z "$$tag" ]]; then \
	  printf 'get_changelog: TAG is empty; pass TAG=v1.0.0 or create a git tag\n' >&2; \
	  exit 1; \
	fi; \
	notes="$$(awk -v tag="$$tag" ' \
	  /^## / { if (found) exit; if (index($$0,"## "tag" ")==1 || $$0=="## "tag) found=1; next } \
	  found { lines[n++]=$$0 } \
	  END { \
	    s=0; while (s<n && lines[s]~/^[[:space:]]*$$/) s++; \
	    e=n-1; while (e>=s && lines[e]~/^[[:space:]]*$$/) e--; \
	    for (i=s;i<=e;i++) print lines[i] \
	  }' CHANGELOG.md)"; \
	if [[ -z "$$notes" ]]; then \
	  printf 'get_changelog: no CHANGELOG entry for %s\n' "$$tag" >&2; \
	  exit 1; \
	fi; \
	printf '%s\n' "$$notes"

# CI
.PHONY: ci
ci: check_format check_mod vet test ## Run every check the lint and test workflows run

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf dist/ install.sh install.ps1 checksums.txt checksums.txt.sigstore.json coverage.out
