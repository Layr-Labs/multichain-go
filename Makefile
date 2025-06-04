GO = $(shell which go)
BIN = ./bin

GO_FLAGS=

# -----------------------------------------------------------------------------
# Deps and setup
# -----------------------------------------------------------------------------
.PHONY: deps
deps: deps/go
	git submodule update --init --recursive
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.61.0

.PHONY: deps/go
deps/go:
	${GO} mod tidy

# -----------------------------------------------------------------------------
# Building
# -----------------------------------------------------------------------------
.PHONY: bindings
bindings: deps
	./scripts/compileBindings.sh


# -----------------------------------------------------------------------------
# Testing, linting, formatting
# -----------------------------------------------------------------------------

.PHONY: test
test:
	GOFLAGS="-count=1" $(GO) test -v -p 1 -parallel 1 ./...

.PHONY: lint
lint:
	golangci-lint run --timeout "5m"

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmtcheck
fmtcheck:
	@unformatted_files=$$(gofmt -l .); \
	if [ -n "$$unformatted_files" ]; then \
		echo "The following files are not properly formatted:"; \
		echo "$$unformatted_files"; \
		echo "Please run 'gofmt -w .' to format them."; \
		exit 1; \
	fi

