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
CONTROLLER_GEN := $(GO) tool -modfile=tools/controller-gen/go.mod controller-gen
# The contract a change is compared with.
BUF_BASE ?= origin/main
GENERATED := internal/gen web/app/src/gen charts/ptah-console/files/operator-roles.yaml \
	charts/ptah-console-agent/files/operator-roles.yaml \
	api/v1alpha1/zz_generated.deepcopy.go charts/ptah-console/crds

# envtest, pinned as stokaro/ptah-operator pins it: setup-envtest by version,
# its release index by commit, and the Kubernetes release inside the
# operator's support window.
SETUP_ENVTEST_VERSION ?= v0.25.1
ENVTEST_KUBERNETES_VERSION ?= 1.37.0
ENVTEST_INDEX ?= https://raw.githubusercontent.com/kubernetes-sigs/controller-tools/1031496fc98a4f51010c3bdfdeb57b5d67bea7bd/envtest-releases.yaml
ENVTEST_BIN_DIR ?=

.PHONY: release-check chart-conform build build-linux image image-agent require-docker-context build-go test test-web-go vet fmt-check verify-go-mod check \
	web-storybook lint lint-golangci lint-qtlint lint-nolintguard lint-teststyle vulncheck \
	web-install web-build web-lint web-test web-size web-check embed-smoke \
	generate verify-generated buf-lint buf-breaking contract test-envtest verify \
	e2e-up e2e e2e-rows e2e-verify e2e-film e2e-down

# The binary with the web app embedded, into bin/. Never the repository root:
# a binary written there gets committed.
build: web-build
	$(GO) build -trimpath -tags with_frontend -ldflags "$(LDFLAGS)" -o $(BIN)/ptah-console ./cmd/ptah-console
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ptah-console-agent ./cmd/ptah-console-agent

# The binary with the web app for Linux, for an image: ARCH is the image's
# architecture, the build machine's by default.
ARCH ?= $(shell $(GO) env GOARCH)
build-linux: web-build
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) $(GO) build -trimpath -tags with_frontend -ldflags "$(LDFLAGS)" \
		-o $(BIN)/linux_$(ARCH)/ptah-console ./cmd/ptah-console

# The image, built on the Docker context DOCKER_CONTEXT names, from a context
# that holds the Dockerfile and the binary alone.
IMAGE ?= ptah-console:dev
image: require-docker-context build-linux
	@context=$$(mktemp -d); \
	mkdir -p "$$context/linux/$(ARCH)" && \
	cp Dockerfile "$$context"/ && cp $(BIN)/linux_$(ARCH)/ptah-console "$$context/linux/$(ARCH)/" && \
	docker build --platform linux/$(ARCH) --build-arg VERSION=$(VERSION) --build-arg REVISION=$(COMMIT) \
		--tag $(IMAGE) "$$context"; \
	status=$$?; rm -rf "$$context"; exit $$status

# The agent's image, the same way, from Dockerfile.agent. The agent serves no
# web app, so it is built without one.
AGENT_IMAGE ?= ptah-console-agent:dev
image-agent: require-docker-context
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) $(GO) build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(BIN)/linux_$(ARCH)/ptah-console-agent ./cmd/ptah-console-agent
	@context=$$(mktemp -d); \
	mkdir -p "$$context/linux/$(ARCH)" && \
	cp Dockerfile.agent "$$context/Dockerfile" && cp $(BIN)/linux_$(ARCH)/ptah-console-agent "$$context/linux/$(ARCH)/" && \
	docker build --platform linux/$(ARCH) --build-arg VERSION=$(VERSION) --build-arg REVISION=$(COMMIT) \
		--tag $(AGENT_IMAGE) "$$context"; \
	status=$$?; rm -rf "$$context"; exit $$status

# Docker work names its context: never whichever one happens to be current.
require-docker-context:
	@[ -n "$$DOCKER_CONTEXT" ] || { echo "name the Docker context to build on in DOCKER_CONTEXT"; exit 1; }

# The binary without the web app: no Node toolchain needed.
build-go:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ptah-console ./cmd/ptah-console
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ptah-console-agent ./cmd/ptah-console-agent

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
	cd tools/chart && $(GO) mod tidy -diff

# What the Verify job runs.
check: fmt-check verify-go-mod vet build-go test release-check

# support/compatibility.json against the chart, go.mod and the operator's
# Kubernetes window. A release runs it once more and requires a released
# operator.
release-check:
	$(GO) run ./hack/releasecheck

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

web-storybook:
	cd web && $(NPM) run build-storybook --workspace app

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
	$(GO) run ./hack/chartroles
	$(CONTROLLER_GEN) object paths=./api/...
	$(CONTROLLER_GEN) crd paths=./api/... output:crd:artifacts:config=charts/ptah-console/crds

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
# The chart's renders against the API schemas of every Kubernetes minor the
# operator supports. Its golden renders and refusals are Go tests under
# test/chart, which make test runs.
chart-conform:
	./hack/chart-conform.sh

verify: check lint vulncheck web-check embed-smoke contract test-envtest chart-conform

# The acceptance environment: the operator's own demonstration lab on kind, at
# the operator commit go.mod requires, with this repository's personas and
# fixtures (hack/e2e). The lab runs on DOCKER_CONTEXT, remote-dev-container by
# default; the Acceptance workflow runs it on a GitHub runner.
e2e-up:
	./hack/e2e/up.sh

# The rows of stokaro/ptah-operator#587 against it, recorded, and then the
# cluster's own account of what they decided.
e2e: e2e-rows e2e-verify

# E2E_ROWS names some of the row files, to run those alone.
e2e-rows: build
	cd web && npx playwright test -c e2e/playwright.config.ts $(E2E_ROWS)

e2e-verify:
	./hack/e2e/verify.sh

# The rows' recordings, joined into one film with the waits cut out.
e2e-film:
	./hack/e2e/film.sh

e2e-down:
	./hack/e2e/down.sh
