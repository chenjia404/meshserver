GOMODCACHE ?= $(CURDIR)/.gomodcache
GOTOOLCHAIN ?= auto

.PHONY: proto build run test check compose-up compose-down

proto:
	GOMODCACHE=$(GOMODCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) ./scripts/gen-proto.sh

build:
	GOMODCACHE=$(GOMODCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) go build ./cmd/meshserver

run:
	GOMODCACHE=$(GOMODCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) go run ./cmd/meshserver

test:
	GOMODCACHE=$(GOMODCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) go test ./...

check:
	GOMODCACHE=$(GOMODCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) ./scripts/check.sh

compose-up:
	cd docker-compose && docker compose up -d --build

compose-down:
	cd docker-compose && docker compose down

