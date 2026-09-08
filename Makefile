BINARY := dockhand
VERSION ?= 0.0.0-dev

# The vendored tree is the tree under test: fail loudly if it is
# missing or inconsistent rather than falling back to the module cache.
# Appends to the caller's own GOFLAGS rather than replacing them. The
# -ldflags value uses the = form throughout because GOFLAGS entries
# cannot contain spaces.
export GOFLAGS := $(GOFLAGS) -mod=vendor -ldflags=-X=main.Version=$(VERSION)

.PHONY: build clean generate test vet fmt lint check

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

# generate re-derives the code the tree derives from its own
# declarations. There is one such file today — info's comparison table,
# written from info.Semantic — and it is regenerated rather than trusted
# because the thing it enforces is that nobody edited it by hand: the
# unkeyed pins inside it catch a field ADDED to the struct, and this
# catches the generated file drifting from the struct in any other way.
generate:
	go generate ./...

# check runs generate and requires the tree's generated files to be
# clean afterwards — UNCHANGED and TRACKED, which is why it asks git
# status rather than git diff: a generated file nobody committed would
# pass a diff while being absent from every other checkout. A dirty one
# means the committed table is not what the struct says, which is
# exactly the omission the generation exists to make impossible, so it
# fails the check rather than waiting for a reviewer.
#
# Guarded on git the way lint is guarded on golangci-lint: outside a
# checkout the answer is "cannot tell", and saying so beats failing a
# check that passes everywhere else.
check: lint generate
	test -z "$$(gofmt -l cmd internal)"
	@if git rev-parse --git-dir >/dev/null 2>&1; then \
		test -z "$$(git status --porcelain -- '*_gen.go')" || \
			{ git status --short -- '*_gen.go'; \
			  echo "a generated file is not what the generator writes; commit the regenerated result"; \
			  exit 1; }; \
	else \
		echo "not a git checkout; skipping the generated-file check"; \
	fi
	go vet ./...

clean:
	rm -f $(BINARY)
	go clean

# liveproof drives the built binary over real ports in a local
# macports-ports checkout and diffs every byte of its output against
# the recorded baseline (scripts/liveproof.sh). It needs MacPorts and
# that checkout, which is why it is not part of check.
.PHONY: liveproof
liveproof:
	scripts/liveproof.sh check
