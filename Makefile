BINARY := dockhand

# The vendored tree is the tree under test: fail loudly if it is
# missing or inconsistent rather than falling back to the module cache.
export GOFLAGS := -mod=vendor

.PHONY: build clean test vet fmt lint check

build:
	go build -o $(BINARY) ./cmd/dockhand

test:
	go test -race ./...

vet:
	go vet ./...

# fmt formats our trees in place; check verifies instead. Both are
# scoped to our code: vendored files are formatted by their upstreams'
# Go versions, not ours.
fmt:
	gofmt -w cmd internal

# lint runs the linters CI runs. It is a separate target because the
# tool is not part of the Go toolchain: CI installs it through its own
# action, so locally an absent linter says so rather than failing a
# check that passes everywhere else. Its findings are still real — the
# errcheck and testifylint rules catch what go vet does not.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; skipping (CI runs it regardless)"; \
	fi

check: lint
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...

clean:
	rm -f $(BINARY)
	go clean
