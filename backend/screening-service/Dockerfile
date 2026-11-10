FROM golang:1.25.8 AS builder

WORKDIR /app/screening-service
COPY screening-service/go.mod screening-service/go.sum ./
COPY investigator ../investigator
COPY mysqlstore ../mysqlstore
COPY auth-service ../auth-service
COPY case-manager-service ../case-manager-service
COPY data-model-service ../data-model-service
COPY decision-engine-service ../decision-engine-service
RUN go mod download

COPY screening-service/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/case-delivery-worker ./cmd/case-delivery-worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate ./cmd/migrate

FROM gcr.io/distroless/base-debian12

WORKDIR /app
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/worker /app/worker
COPY --from=builder /bin/case-delivery-worker /app/case-delivery-worker
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /app/screening-service/internal/migrations /app/internal/migrations

EXPOSE 8085
ENTRYPOINT ["/app/server"]
