# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -o /out/ ./cmd/server ./cmd/token

# Runtime stage
FROM alpine:3.23
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/token /app/token
ENV PORT=21465 DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 21465
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 CMD ["/app/server", "-healthcheck"]
STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/server"]
