VERSION ?= dev
REPOSITORY ?=
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(DATE) -X main.releaseRepo=$(REPOSITORY)

.PHONY: build install fmt check-fmt test vet check release clean
build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/xswap .
install: build
	./bin/xswap install
fmt:
	gofmt -w *.go
check-fmt:
	@test -z "$$(gofmt -l *.go)" || (gofmt -l *.go; exit 1)
test:
	go test -race ./...
vet:
	go vet ./...
check: check-fmt vet test build
release:
	VERSION='$(VERSION)' COMMIT='$(COMMIT)' BUILD_DATE='$(DATE)' REPOSITORY='$(REPOSITORY)' sh scripts/build-release.sh
clean:
	rm -rf bin dist coverage.out
