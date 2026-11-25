FROM golang:1.27.4 AS build
WORKDIR /src/auth-service
COPY auth-service/go.mod auth-service/go.sum ./
COPY platform-auth ../platform-auth
RUN go mod download
COPY auth-service/ .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server && CGO_ENABLED=0 go build -o /out/admin ./cmd/admin
FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /out/ /app/
EXPOSE 8087
USER nonroot
ENTRYPOINT ["/app/server"]
