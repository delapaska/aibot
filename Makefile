GO ?= go

.PHONY: build run test vet fmt tidy docker-up docker-down

build:
	cd backend && $(GO) build -o bin/aibot ./cmd/aibot

run:
	cd backend && $(GO) run ./cmd/aibot

test:
	cd backend && $(GO) test ./...

vet:
	cd backend && $(GO) vet ./...

fmt:
	cd backend && $(GO) fmt ./...

tidy:
	cd backend && $(GO) mod tidy

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
