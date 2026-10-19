GO ?= go
NPM ?= npm
BIN ?= bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/stokaro/ptah-console/internal/buildinfo.version=$(VERSION) \
	-X github.com/stokaro/ptah-console/internal/buildinfo.commit=$(COMMIT)

QTLINT_RULES := -require-qt-c-receiver -require-data-rows -require-testing-run

# buf is a tool of its own module, tools/buf, so its dependency graph stays out
# of go.mod and its go.sum pins every module it needs: a build verifies them
# against that file instead of asking the checksum database each time. The
# code generators it calls are pinned in go.mod and web/package-lock.json.
BUF := $(GO) tool -modfile=tools/buf/go.mod buf
# The contract a change is compared with.
BUF_BASE ?= origin/main
GENERATED := internal/gen web/app/src/gen

# envtest, pinned as stokaro/ptah-operator pins it: setup-envtest by version,
# its release index by commit, and the Kubernetes release inside the
# operator's support window.
SETUP_ENVTEST_VERSION ?= v0.25.1
ENVTEST_KUBERNETES_VERSION ?= 1.37.0
ENVTEST_INDEX ?= https://raw.githubusercontent.com/kubernetes-sigs/controller-tools/1031496fc98a4f51010c3bdfdeb57b5d67bea7bd/envtest-releases.yaml
ENVTEST_BIN_DIR ?=

.PHONY: build build-go test test-web-go vet fmt-check verify-go-mod check \
	lint lint-golangci lint-qtlint lint-nolintguard lint-teststyle vulncheck \
	web-install web-build web-lint web-test web-size web-check embed-smoke \
	generate verify-generated buf-lint buf-breaking contract test-envtest verify

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
	cd tools/buf && $(GO) mod tidy -diff

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

# Go and TypeScript from the proto files. protoc-gen-es comes from
# web/node_modules, so run make web-install first.
generate:
	$(BUF) generate

# Generated code is committed, and has to be what the generators write today.
verify-generated: generate
	@git diff --exit-code -- $(GENERATED) || { echo "generated code is stale: run make generate"; exit 1; }
	@untracked="$$(git ls-files --others --exclude-standard -- $(GENERATED))"; \
	if [ -n "$$untracked" ]; then echo "generated files git does not track:"; echo "$$untracked"; exit 1; fi

buf-lint:
	$(BUF) lint

# A change to the API is compared with BUF_BASE. Before the first proto file
# reached it there is nothing to break, and the target says so rather than
# passing silently.
buf-breaking:
	@if git cat-file -e "$(BUF_BASE):proto" 2>/dev/null; then \
		$(BUF) breaking --against '.git#ref=$(BUF_BASE)'; \
	else \
		echo "buf-breaking: $(BUF_BASE) has no proto/ yet, so there is no contract to compare with"; \
	fi

contract: buf-lint buf-breaking verify-generated

# The packages under test/envtest against a real kube-apiserver and etcd. A
# missing control plane fails here instead of skipping.
test-envtest:
	@assets="$$($(GO) run sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION) \
		use $(ENVTEST_KUBERNETES_VERSION) --index "$(ENVTEST_INDEX)" \
		$(if $(ENVTEST_BIN_DIR),--bin-dir "$(abspath $(ENVTEST_BIN_DIR))") -p path)" || exit $$?; \
	KUBEBUILDER_ASSETS="$$assets" PTAH_CONSOLE_REQUIRE_ENVTEST=1 $(GO) test -count=1 -timeout=10m ./test/envtest/...

# Everything CI runs.
verify: check lint vulncheck web-check embed-smoke contract test-envtest
