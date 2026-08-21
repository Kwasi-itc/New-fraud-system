FROM golang:1.25.0 AS builder
WORKDIR /app

COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /bin/server /app/server
EXPOSE 8083
ENTRYPOINT ["/app/server"]
