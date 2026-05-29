# Build stage
FROM golang:1.23-alpine AS build
RUN apk add --no-cache gcc musl-dev   # cgo for go-sqlite3
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=1 go build -o /out/server ./cmd/server

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates sqlite-libs
WORKDIR /app
COPY --from=build /out/server /app/server
EXPOSE 21465
ENTRYPOINT ["/app/server"]
