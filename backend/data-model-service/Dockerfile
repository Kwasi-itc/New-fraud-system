FROM golang:1.25.0 AS builder

WORKDIR /app/data-model-service

COPY data-model-service/go.mod data-model-service/go.sum ./
COPY investigator ../investigator
COPY mysqlstore ../mysqlstore
RUN go mod download

COPY data-model-service/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate ./cmd/migrate
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate-mysql ./cmd/migrate-mysql
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/reconcile-physical-mysql ./cmd/reconcile-physical-mysql
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/inspect-deliveries-mysql ./cmd/inspect-deliveries-mysql
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/redrive-delivery-mysql ./cmd/redrive-delivery-mysql
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/recover-delivery ./cmd/recover-delivery

FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/worker /app/worker
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /bin/migrate-mysql /app/migrate-mysql
COPY --from=builder /bin/reconcile-physical-mysql /app/reconcile-physical-mysql
COPY --from=builder /bin/inspect-deliveries-mysql /app/inspect-deliveries-mysql
COPY --from=builder /bin/redrive-delivery-mysql /app/redrive-delivery-mysql
COPY --from=builder /bin/recover-delivery /app/recover-delivery
COPY --from=builder /app/data-model-service/internal/migrations /app/internal/migrations

EXPOSE 8080

ENTRYPOINT ["/app/server"]
