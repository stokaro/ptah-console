GO ?= go
NPM ?= npm
BIN ?= bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/stokaro/ptah-console/internal/buildinfo.version=$(VERSION) \
	-X github.com/stokaro/ptah-console/internal/buildinfo.commit=$(COMMIT)

QTLINT_RULES := -require-qt-c-receiver -require-data-rows -require-testing-run

.PHONY: build build-go test test-web-go vet fmt-check verify-go-mod check \
	lint lint-golangci lint-qtlint lint-nolintguard lint-teststyle vulncheck \
	web-install web-build web-lint web-test web-size web-check embed-smoke verify

# The binary with the web app embedded, into bin/. Never the repository root:
# a binary written there gets committed.
build: web-build
	$(GO) build -trimpath -tags with_frontend -ldflags "$(LDFLAGS)" -o $(BIN)/ptah-console ./cmd/ptah-console

# The binary without the web app: no Node toolchain needed.
build-go:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ptah-console ./cmd/ptah-console

test: test-web-go
	$(GO) test -race -count=1 ./...

# web/ is a module of its own. Only its package is named: ./... there would
# walk node_modules.
test-web-go:
	cd web && $(GO) test -count=1 .

vet:
	$(GO) vet ./...
	cd web && $(GO) vet .

# Only the files git knows: node_modules may carry .go files of its own.
fmt-check:
	@out="$$(git ls-files --cached --others --exclude-standard -- '*.go' | xargs gofmt -l)"; \
	if [ -n "$$out" ]; then echo "gofmt would change:"; echo "$$out"; exit 1; fi

verify-go-mod:
	$(GO) mod tidy -diff
	cd web && $(GO) mod tidy -diff

# What the Verify job runs.
check: fmt-check verify-go-mod vet build-go test

# Modeled on stokaro/ptah: golangci-lint with its configuration, quicktest
# style, justified nolint directives, and declarative tests.
lint: lint-qtlint lint-nolintguard lint-teststyle lint-golangci

lint-golangci:
	golangci-lint run ./...
	cd web && golangci-lint run --config ../.golangci.yml .

lint-qtlint:
	$(GO) tool qtlint $(QTLINT_RULES) ./...

lint-nolintguard:
	$(GO) vet -vettool="$$($(GO) tool -n nolintguard)" -require-justification ./...

lint-teststyle:
	./hack/check-test-style.sh

vulncheck:
	$(GO) tool govulncheck ./...

web-install:
	cd web && $(NPM) ci

web-build:
	cd web && $(NPM) run build

# Modeled on denisvmedia/inventario: ESLint, Prettier and the type checker.
web-lint:
	cd web && $(NPM) run lint && $(NPM) run format:check && $(NPM) run typecheck

web-test:
	cd web && $(NPM) test

web-size: web-build
	cd web && $(NPM) run size

web-check: web-lint web-test web-size

# The release binary carries the app it was built with, and every file its
# index.html loads.
embed-smoke: build
	./$(BIN)/ptah-console version -o json | grep -q '"webApp": true'
	cd web && $(GO) test -count=1 -tags with_frontend .

# Everything CI runs.
verify: check lint vulncheck web-check embed-smoke
