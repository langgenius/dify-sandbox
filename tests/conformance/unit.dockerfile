FROM golang:1.24.11-bookworm@sha256:656be510c8b4d33acf4eac8575b7f04a3e30b705c152dc6bde112dbe1a87b602
ENV GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2
RUN apt-get update && apt-get install -y --no-install-recommends libseccomp-dev pkg-config && rm -rf /var/lib/apt/lists/* && useradd -u 65537 sandbox
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -buildmode=c-shared -o internal/core/runner/python/python.so ./cmd/lib/python && go build -buildmode=c-shared -o internal/core/runner/nodejs/nodejs.so ./cmd/lib/nodejs
RUN mkdir -p /out && go build -o /out/request-worker ./cmd/request-worker
CMD ["go", "test", "./internal/core/runner/isolation", "-count=1"]
