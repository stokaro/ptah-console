GO ?= go
BIN ?= bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/stokaro/ptah-console/internal/buildinfo.version=$(VERSION) \
	-X github.com/stokaro/ptah-console/internal/buildinfo.commit=$(COMMIT)

.PHONY: build test vet fmt-check verify-go-mod check lint lint-golangci lint-qtlint lint-nolintguard lint-teststyle vulncheck verify

QTLINT_RULES := -require-qt-c-receiver -require-data-rows -require-testing-run

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

# What the Verify job runs.
check: fmt-check verify-go-mod vet build test

# Modeled on stokaro/ptah: golangci-lint with its configuration, quicktest
# style, justified nolint directives, and declarative tests.
lint: lint-qtlint lint-nolintguard lint-teststyle lint-golangci

lint-golangci:
	golangci-lint run ./...

lint-qtlint:
	$(GO) tool qtlint $(QTLINT_RULES) ./...

lint-nolintguard:
	$(GO) vet -vettool="$$($(GO) tool -n nolintguard)" -require-justification ./...

lint-teststyle:
	./hack/check-test-style.sh

vulncheck:
	$(GO) tool govulncheck ./...

# Everything CI runs.
verify: check lint vulncheck
