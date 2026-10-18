GO ?= go

.PHONY: build run test cover vet fmt tidy panel test-panel docker-up docker-down

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

# Собирает фронтенд и встраивает его в Go-бинарник (dist/app).
# Node не нужен на хосте: npm крутится в контейнере; в Docker-образе
# сборка панели идёт своей стадией Dockerfile.
panel:
	docker run --rm -v "$$PWD/frontend:/web" -w /web node:22-alpine \
		sh -c "npm install --no-fund --no-audit --loglevel=warn && npm run build"
	rm -rf backend/internal/infra/httpapi/dist/app
	cp -r frontend/dist backend/internal/infra/httpapi/dist/app

test-panel:
	docker run --rm -v "$$PWD/frontend:/web" -w /web node:22-alpine \
		sh -c "npm install --no-fund --no-audit --loglevel=warn && npm test"

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
