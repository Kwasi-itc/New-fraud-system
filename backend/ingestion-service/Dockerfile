FROM golang:1.25.0 AS builder
WORKDIR /backend/ingestion-service

COPY event-store-service /backend/event-store-service
COPY ingestion-service/go.mod ingestion-service/go.sum ./
RUN go mod download

COPY ingestion-service .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate ./cmd/migrate

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/worker /app/worker
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /backend/ingestion-service/internal/migrations /app/internal/migrations
EXPOSE 8081
ENTRYPOINT ["/app/server"]
