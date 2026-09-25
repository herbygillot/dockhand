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
GO_LDFLAGS += -X github.com/herbygillot/dockhand/internal/version.Version=$(strip $(VERSION))
endif

.PHONY: build test test-race vet fmt-check deadcode vendor vendor-check clean

build:
	$(GO) build $(if $(strip $(GO_LDFLAGS)),-ldflags "$(GO_LDFLAGS)") -o "$(BINARY)" ./cmd/dockhand

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Fail when a Go file outside vendor is not gofmt-formatted, naming it.
fmt-check:
	@files=$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go' | grep -v '^vendor/')); \
	if [ -n "$$files" ]; then echo "not gofmt-formatted:" >&2; echo "$$files" >&2; exit 1; fi

# Whole-program reachability including tests; see docs/reviews/2026-09-17-exported-surface-audit.md.
# A tool run by version is fetched as a module of its own, which the vendor mode forbids.
deadcode:
	GOFLAGS= $(GO) run golang.org/x/tools/cmd/deadcode@v0.48.0 -test ./...

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
