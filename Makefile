GO ?= go
GOLANGCI_LINT ?= $$($(GO) env GOPATH)/bin/golangci-lint
GOLANGCI_LINT_VERSION ?= v1.30.0

LDFLAGS ?= -s -w

.PHONY: lint
lint: linter
	$(GOLANGCI_LINT) run

.PHONY: linter
linter:
	test -f $(GOLANGCI_LINT) || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$($(GO) env GOPATH)/bin $(GOLANGCI_LINT_VERSION)

.PHONY: test
test:
	$(GO) test -v ./... -timeout 5m

.PHONY: test-race
test-race:
	$(GO) test -v ./... -race -timeout 20m

dist:
	mkdir $@

.PHONY: clean
clean:
	$(GO) clean
	rm -rf dist/

.PHONY: binary
binary: export CGO_ENABLED=1
binary: dist
	$(GO) version
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/bee-afs ./cmd
