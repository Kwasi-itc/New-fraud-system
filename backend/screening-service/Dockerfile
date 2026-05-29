FROM golang:1.24.2 AS build

WORKDIR /app
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /screening-server ./cmd/server

FROM gcr.io/distroless/base-debian12
COPY --from=build /screening-server /screening-server
EXPOSE 8085
ENTRYPOINT ["/screening-server"]
