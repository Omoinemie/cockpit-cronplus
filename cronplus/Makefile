VERSION ?= $(shell cat ../VERSION 2>/dev/null | tr -d '[:space:]' || echo "0.0.0")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
ROOT := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))/..

.PHONY: all clean build daemon cli

all: build

build: daemon cli

daemon:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/cronplusd ./cmd/cronplusd

cli:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/cronplus ./cmd/cronplus

clean:
	rm -rf bin/
