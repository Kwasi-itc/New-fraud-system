FROM golang:1.25.0 AS builder

WORKDIR /app/decision-engine-service

COPY decision-engine-service/go.mod decision-engine-service/go.sum ./
COPY investigator ../investigator
RUN go mod download

COPY decision-engine-service/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate ./cmd/migrate

FROM gcr.io/distroless/base-debian12

WORKDIR /app
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/worker /app/worker
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /app/decision-engine-service/internal/migrations /app/internal/migrations

EXPOSE 8082

ENTRYPOINT ["/app/server"]
