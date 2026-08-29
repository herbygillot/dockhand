BINARY := dockhand

.PHONY: build clean test

build:
	go build -o $(BINARY) ./cmd/dockhand

clean:
	rm -f $(BINARY)
	go clean

test:
	go test -race ./...
