GO ?= go
BINARY ?= dockhand

.PHONY: build test test-race vet clean

build:
	$(GO) build -o "$(BINARY)" ./cmd/dockhand

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

clean:
	rm -f -- "$(BINARY)"
