GO ?= go

.PHONY: build run test cover vet fmt tidy docker-up docker-down

build:
	cd backend && $(GO) build -o bin/aibot ./cmd/aibot

run:
	cd backend && $(GO) run ./cmd/aibot

test:
	cd backend && $(GO) test ./...

cover:
	cd backend && $(GO) test ./... -covermode=atomic -coverprofile=coverage.out
	cd backend && $(GO) tool cover -func=coverage.out | tail -1

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
