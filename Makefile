.PHONY: help build test lint fmt clean docker-build docker-push up down logs migrate

# Variables
BINARY_SERVER=server
BINARY_WORKER=worker
GO=go
DOCKER_IMAGE=event-fanout-service
DOCKER_REGISTRY=ghcr.io/shwetaudacious

help:
	@echo "Available targets:"
	@echo "  build             - Build binaries"
	@echo "  test              - Run unit tests"
	@echo "  test-integration  - Run integration tests (requires Postgres + Redis)"
	@echo "  test-all          - Run unit + integration tests"
	@echo "  test-coverage     - Run tests with coverage"
	@echo "  lint              - Run linter"
	@echo "  fmt               - Format code"
	@echo "  clean             - Remove built binaries"
	@echo "  docker-build      - Build Docker image"
	@echo "  docker-push       - Push Docker image"
	@echo "  up                - Start services with docker-compose"
	@echo "  down              - Stop services"
	@echo "  logs              - View container logs"
	@echo "  migrate           - Run database migrations"

build:
	$(GO) build -o bin/$(BINARY_SERVER) ./cmd/server
	$(GO) build -o bin/$(BINARY_WORKER) ./cmd/worker
	@echo "✅ Binaries built successfully"

test:
	$(GO) test -v -race $$(go list ./... | grep -v /tests/integration)

test-integration:
	$(GO) test -v -tags=integration ./tests/integration/...

test-all: test test-integration

test-coverage:
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report generated: coverage.html"

lint:
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...
	$(GO) mod tidy

clean:
	rm -rf bin/
	rm -f coverage.out coverage.html
	@echo "✅ Cleaned"

docker-build:
	docker build -t $(DOCKER_IMAGE):latest -t $(DOCKER_IMAGE):dev .

docker-push:
	docker tag $(DOCKER_IMAGE):latest $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	docker tag $(DOCKER_IMAGE):dev $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):dev
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):dev

up:
	docker-compose up -d
	@echo "✅ Services started"
	@echo "📊 Dashboard: http://localhost:8080"
	@echo "🗄️  PostgreSQL: localhost:5432"
	@echo "📦 Redis: localhost:6379"

down:
	docker-compose down
	@echo "✅ Services stopped"

logs:
	docker-compose logs -f

logs-server:
	docker-compose logs -f server

logs-worker:
	docker-compose logs -f worker

ps:
	docker-compose ps

# Quick test: ingest an event
test-ingest:
	curl -X POST http://localhost:8080/api/v1/events \
		-H "Content-Type: application/json" \
		-d '{"type":"test.event","source":"cli","payload":{"message":"Hello from CLI"}}'

# Quick test: list subscriptions
test-list-subs:
	curl http://localhost:8080/api/v1/subscriptions

# Quick test: create a subscription
test-create-sub:
	curl -X POST http://localhost:8080/api/v1/subscriptions \
		-H "Content-Type: application/json" \
		-d '{"webhook_url":"http://webhook.example.com","rules":{"type":"test.event"}}'
