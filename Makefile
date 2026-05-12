APP_NAME=marble-datamodel-service
DATA_MODEL_TEST_DATABASE_URL ?= postgres://datamodel:datamodel@localhost:5434/datamodel?sslmode=disable

.PHONY: run build test test-integration migrate-up migrate-down docker-up docker-down

run:
	go run ./cmd/server

build:
	go build ./...

test:
	go test ./...

test-integration:
	DATA_MODEL_TEST_DATABASE_URL=$(DATA_MODEL_TEST_DATABASE_URL) go test -run Integration ./...

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down 1

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v
