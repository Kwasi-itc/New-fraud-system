FROM golang:1.24.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server && CGO_ENABLED=0 go build -o /out/admin ./cmd/admin
FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /out/ /app/
EXPOSE 8087
ENTRYPOINT ["/app/server"]
