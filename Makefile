GO ?= go
BINARY ?= dockhand
GITHUB_OAUTH_CLIENT_ID ?=
# VERSION names the build when no Git tag can be stamped, as from a release
# tarball; a tag on a checkout wins over it. With or without the leading v.
VERSION ?=
GO_LDFLAGS ?=

# Dependencies are vendored. Go would use the vendor directory on its own, but
# naming the mode makes a missing or stale vendor directory a loud failure
# instead of a silent module download.
GOFLAGS ?= -mod=vendor
export GOFLAGS

ifneq ($(strip $(GITHUB_OAUTH_CLIENT_ID)),)
GO_LDFLAGS += -X github.com/herbygillot/dockhand/internal/github.DefaultOAuthClientID=$(strip $(GITHUB_OAUTH_CLIENT_ID))
endif
ifneq ($(strip $(VERSION)),)
GO_LDFLAGS += -X github.com/herbygillot/dockhand/internal/buildinfo.Version=$(strip $(VERSION))
endif

.PHONY: build test test-race vet lint fmt-check deadcode vendor vendor-check clean

build:
	$(GO) build $(if $(strip $(GO_LDFLAGS)),-ldflags "$(GO_LDFLAGS)") -o "$(BINARY)" ./cmd/dockhand

# The engine and command packages take minutes alone, and past go test's
# ten-minute default on a loaded Mac; the bound is room, not a target.
TEST_TIMEOUT ?= 40m

# The command line's tests set package-level seams and HOME, so they can't
# run in parallel in one process; they run as COMMAND_SHARDS processes
# (tools/shard-test.sh), beside every other package's. On a Mac with 18
# cores, 280 s became 61 s (batch 49).
COMMAND_PACKAGE := github.com/herbygillot/dockhand/internal/command
COMMAND_SHARDS ?= 6

test:
	@$(GO) test -timeout $(TEST_TIMEOUT) $$($(GO) list ./... | grep -vxF $(COMMAND_PACKAGE)) & others=$$!; \
	GO="$(GO)" tools/shard-test.sh $(COMMAND_PACKAGE) $(COMMAND_SHARDS) $(TEST_TIMEOUT); command=$$?; \
	wait $$others; others=$$?; [ $$others = 0 ] && [ $$command = 0 ]

test-race:
	@$(GO) test -race -timeout $(TEST_TIMEOUT) $$($(GO) list ./... | grep -vxF $(COMMAND_PACKAGE)) & others=$$!; \
	GO="$(GO)" tools/shard-test.sh $(COMMAND_PACKAGE) $(COMMAND_SHARDS) $(TEST_TIMEOUT) -race; command=$$?; \
	wait $$others; others=$$?; [ $$others = 0 ] && [ $$command = 0 ]

vet:
	$(GO) vet ./...

# golangci-lint, pinned, with .golangci.yml. Run by version like deadcode, and
# built with this module's Go (GOTOOLCHAIN), which it must be at least as new
# as; left alone, go run would build it with the older Go its own go.mod asks.
GOLANGCI_LINT ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
lint:
	GOFLAGS= GOTOOLCHAIN=$$($(GO) env GOVERSION) $(GO) run $(GOLANGCI_LINT) run ./...

# Fail when a Go file outside vendor is not gofmt-formatted, naming it.
fmt-check:
	@files=$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go' | grep -v '^vendor/')); \
	if [ -n "$$files" ]; then echo "not gofmt-formatted:" >&2; echo "$$files" >&2; exit 1; fi

# Whole-program reachability including tests; see docs/reviews/2026-09-17-exported-surface-audit.md.
# A tool run by version is fetched as a module of its own, which the vendor mode forbids.
# The tool exits 0 whatever it reports, so anything it prints fails the target.
deadcode:
	@out=$$(GOFLAGS= $(GO) run golang.org/x/tools/cmd/deadcode@v0.48.0 -test ./...) || exit 1; \
	if [ -n "$$out" ]; then echo "unreachable, tests included:" >&2; echo "$$out" >&2; exit 1; fi

# Refresh the vendor directory after a change to go.mod. Commit go.mod, go.sum,
# and vendor together.
vendor:
	$(GO) mod tidy
	$(GO) mod vendor

# Fail when go.mod or go.sum is untidy or the vendor directory differs from
# what go.mod requires. Nothing in the tree is modified.
vendor-check:
	$(GO) mod tidy -diff
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && $(GO) mod vendor -o "$$tmp" && \
	if diff -rq "$$tmp" vendor >/dev/null; then echo "vendor is in sync with go.mod"; \
	else diff -rq "$$tmp" vendor; echo "vendor is out of date; run make vendor and commit the result" >&2; exit 1; fi

clean:
	rm -f -- "$(BINARY)"
