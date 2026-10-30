FROM golang:1.25.0 AS builder

ARG SOURCE_REVISION
ARG SOURCE_TREE_SHA256
WORKDIR /app/decision-engine-service

COPY decision-engine-service/go.mod decision-engine-service/go.sum ./
COPY investigator ../investigator
COPY mysqlstore ../mysqlstore
RUN go mod download

COPY decision-engine-service/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate ./cmd/migrate
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.builtRevision=${SOURCE_REVISION} -X main.builtSourceSHA256=${SOURCE_TREE_SHA256}" -o /bin/mysql-release ./cmd/mysql-release

FROM gcr.io/distroless/base-debian12

ARG SOURCE_REVISION
ARG SOURCE_TREE_SHA256
LABEL org.opencontainers.image.revision=$SOURCE_REVISION \
      com.itc.decision.source-tree-sha256=$SOURCE_TREE_SHA256

WORKDIR /app
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/worker /app/worker
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /bin/mysql-release /app/mysql-release
COPY --from=builder /app/decision-engine-service/internal/migrations /app/internal/migrations

EXPOSE 8082

ENTRYPOINT ["/app/server"]
