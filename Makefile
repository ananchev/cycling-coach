.PHONY: build run dev test lint stop logs

build:
	docker compose build

run:
	docker compose up -d

stop:
	docker compose down

logs:
	docker compose logs -f

dev:
	go run ./cmd/server || true

test:
	go test ./...

lint:
	golangci-lint run ./...
