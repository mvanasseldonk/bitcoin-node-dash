.PHONY: all web build run test lint clean

MOCK ?= healthy

all: build

## web: install frontend dependencies and build web/dist
web:
	cd web && npm ci && npm run build

## build: build the frontend and the single node-dash binary
build: web
	go build -trimpath -o bin/node-dash ./cmd/node-dash

## run: build and run on mock data (MOCK=healthy|syncing|warning|error|unreachable|starting|stale)
run: build
	NODEDASH_MOCK=$(MOCK) ./bin/node-dash

## test: run Go tests and the frontend type check
test:
	go test ./...
	cd web && npm run typecheck

## lint: check formatting and run go vet
lint:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...

clean:
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
