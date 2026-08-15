# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /out/gcs-connector ./cmd/gcs-connector

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /config
COPY --from=builder /out/gcs-connector /gcs-connector
ENTRYPOINT ["/gcs-connector"]
