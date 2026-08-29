BINARY := dockhand

# The vendored tree is the tree under test: fail loudly if it is
# missing or inconsistent rather than falling back to the module cache.
export GOFLAGS := -mod=vendor

.PHONY: build clean test vet fmt check

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

check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...

clean:
	rm -f $(BINARY)
	go clean
