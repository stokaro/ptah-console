GO ?= go
BIN ?= bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/stokaro/ptah-console/internal/buildinfo.version=$(VERSION) \
	-X github.com/stokaro/ptah-console/internal/buildinfo.commit=$(COMMIT)

.PHONY: build test vet fmt-check verify-go-mod verify

# Into bin/, never the repository root: a binary written there gets committed.
build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ptah-console ./cmd/ptah-console

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt would change:"; echo "$$out"; exit 1; fi

verify-go-mod:
	$(GO) mod tidy -diff

# The checks CI runs.
verify: fmt-check verify-go-mod vet build test
