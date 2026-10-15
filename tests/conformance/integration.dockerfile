FROM golang:1.24.11-bookworm@sha256:656be510c8b4d33acf4eac8575b7f04a3e30b705c152dc6bde112dbe1a87b602 AS build
ENV GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2 CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/core/runner/isolation ./internal/core/runner/isolation
COPY internal/core/runner/environment ./internal/core/runner/environment
COPY internal/core/runner/network ./internal/core/runner/network
COPY tests/conformance ./tests/conformance
RUN go build -o /conformance ./tests/conformance
FROM scratch
COPY --from=build /conformance /conformance
USER 65534
ENTRYPOINT ["/conformance"]
