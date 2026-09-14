GO ?= go
BINARY ?= dockhand
GITHUB_OAUTH_CLIENT_ID ?=
GO_LDFLAGS ?=

ifneq ($(strip $(GITHUB_OAUTH_CLIENT_ID)),)
GO_LDFLAGS += -X github.com/herbygillot/dockhand/internal/app.DefaultGitHubOAuthClientID=$(strip $(GITHUB_OAUTH_CLIENT_ID))
endif

.PHONY: build test test-race vet clean

build:
	$(GO) build $(if $(strip $(GO_LDFLAGS)),-ldflags "$(GO_LDFLAGS)") -o "$(BINARY)" ./cmd/dockhand

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

clean:
	rm -f -- "$(BINARY)"
