FROM golang:1.24 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/ingestion-server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/ingestion-worker ./cmd/worker

FROM gcr.io/distroless/base-debian12
COPY --from=builder /bin/ingestion-server /ingestion-server
COPY --from=builder /bin/ingestion-worker /ingestion-worker
EXPOSE 8081
ENTRYPOINT ["/ingestion-server"]
