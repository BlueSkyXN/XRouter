ARG GO_VERSION=1.23
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
ARG TARGETOS
ARG TARGETARCH
COPY go.mod ./
COPY *.go ./
COPY config.example.json ./
RUN target_os="${TARGETOS:-$(go env GOOS)}"; \
    target_arch="${TARGETARCH:-$(go env GOARCH)}"; \
    go test ./... && \
    CGO_ENABLED=0 GOOS="${target_os}" GOARCH="${target_arch}" go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/xrouter .

FROM alpine:3.20
WORKDIR /app
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
LABEL org.opencontainers.image.title="XRouter" \
      org.opencontainers.image.description="OpenAI-compatible self-hosted LLM routing gateway" \
      org.opencontainers.image.source="https://github.com/BlueSkyXN/XRouter" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${DATE}"
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -s /sbin/nologin xrouter
COPY --from=build /out/xrouter /app/xrouter
COPY config.example.json /app/config.example.json
USER xrouter
EXPOSE 8080
ENTRYPOINT ["/app/xrouter", "-config", "/app/config.example.json"]
