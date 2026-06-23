.PHONY: migrate run worker test

migrate:
	go run ./cmd/migrate up

run:
	go run ./cmd/server

worker:
	go run ./cmd/worker

test:
	go test ./...
