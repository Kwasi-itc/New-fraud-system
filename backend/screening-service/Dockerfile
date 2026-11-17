FROM golang:1.27.4 AS builder

WORKDIR /app/screening-service
COPY screening-service/go.mod screening-service/go.sum ./
COPY investigator ../investigator
RUN go mod download

COPY screening-service/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/case-delivery-worker ./cmd/case-delivery-worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate ./cmd/migrate
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/source-import ./cmd/source-import
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/source-worker ./cmd/source-worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/source-admin ./cmd/source-admin
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/source-verify ./cmd/source-verify
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/source-notice ./cmd/source-notice

FROM gcr.io/distroless/base-debian12

WORKDIR /app
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/worker /app/worker
COPY --from=builder /bin/case-delivery-worker /app/case-delivery-worker
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /bin/source-import /app/source-import
COPY --from=builder /bin/source-worker /app/source-worker
COPY --from=builder /bin/source-admin /app/source-admin
COPY --from=builder /bin/source-verify /app/source-verify
COPY --from=builder /bin/source-notice /app/source-notice
COPY --from=builder /app/screening-service/internal/migrations /app/internal/migrations

EXPOSE 8085
ENTRYPOINT ["/app/server"]
