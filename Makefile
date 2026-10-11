# council build targets
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BIN     ?= council

.PHONY: all build test test-race cover cover-html vet fmt dist zip source-zip release demo clean

all: vet test build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X github.com/Parsa2820/council/internal/cli.Version=$(VERSION)" -o $(BIN) ./cmd/council

test:
	$(GO) test ./...

# The race detector needs cgo (a C compiler).
test-race:
	$(GO) test -race ./...

# Unit test coverage: per-package figures, then per-function and the total.
# COVERFLAGS=-race adds the race detector (needs cgo).
COVERPROFILE ?= cover.out
COVERFLAGS   ?=
cover:
	$(GO) test $(COVERFLAGS) -count=1 -covermode=atomic -coverprofile=$(COVERPROFILE) ./...
	$(GO) tool cover -func=$(COVERPROFILE)

# Open an annotated HTML coverage report in the browser.
cover-html: cover
	$(GO) tool cover -html=$(COVERPROFILE)

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

# Cross-compile darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64 into dist/
dist:
	VERSION=$(VERSION) ./scripts/build.sh

# ../council-binaries.zip with everything in dist/
zip: dist
	rm -f ../council-binaries.zip
	cd dist && zip -q -9 ../../council-binaries.zip *

# ../council-source.zip from git (committed files only)
source-zip:
	rm -f ../council-source.zip
	git archive --format=zip --prefix=council/ -o ../council-source.zip HEAD

release: vet test zip source-zip

# Run a consensus round against the bundled fake agents
demo: build
	./$(BIN) ask --config examples/fake-council.yaml --verbose "What is 6 * 7?"

clean:
	rm -rf dist $(BIN) $(COVERPROFILE)
