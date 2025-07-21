# Test environment Dockerfile template
ARG GOLANG_VERSION=1.23.9
ARG PYTHON_VERSION=3.10-slim-bookworm
ARG DEBIAN_MIRROR="http://deb.debian.org/debian testing main"
ARG PYTHON_PACKAGES="httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]"
ARG NODEJS_VERSION=v20.11.1
ARG NODEJS_MIRROR="https://npmmirror.com/mirrors/node"
ARG GOLANG_MIRROR="https://golang.org/dl"
ARG TARGETARCH

# Build stage
FROM golang:${GOLANG_VERSION} AS builder

COPY . /app
WORKDIR /app

# Install build dependencies and build
RUN apt-get update && apt-get install -y pkg-config gcc libseccomp-dev \
    && go mod tidy \
    && case "${TARGETARCH}" in \
       "amd64") bash ./build/build_amd64.sh ;; \
       "arm64") bash ./build/build_arm64.sh ;; \
       *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
       esac

# Test stage
FROM python:${PYTHON_VERSION} as tester

ARG DEBIAN_MIRROR
ARG PYTHON_PACKAGES
ARG NODEJS_VERSION
ARG NODEJS_MIRROR
ARG GOLANG_VERSION
ARG GOLANG_MIRROR
ARG TARGETARCH

# Install system dependencies
RUN echo "deb ${DEBIAN_MIRROR}" > /etc/apt/sources.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
       pkg-config \
       libseccomp-dev \
       wget \
       curl \
       xz-utils \
       zlib1g \
       expat \
       perl \
       libsqlite3-0 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy source code
COPY . /app

# Copy binary files from build stage
COPY --from=builder /app/internal/core/runner/python/python.so /app/internal/core/runner/python/python.so
COPY --from=builder /app/internal/core/runner/nodejs/nodejs.so /app/internal/core/runner/nodejs/nodejs.so

# Copy configuration files
COPY conf/config.yaml /conf/config.yaml
COPY dependencies/python-requirements.txt /dependencies/python-requirements.txt

# Install Python dependencies
RUN pip3 install --no-cache-dir ${PYTHON_PACKAGES}

# Install Node.js based on architecture
RUN case "${TARGETARCH}" in \
    "amd64") \
        NODEJS_ARCH="linux-x64" ;; \
    "arm64") \
        NODEJS_ARCH="linux-arm64" ;; \
    *) \
        echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac \
    && wget -O /opt/node-${NODEJS_VERSION}-${NODEJS_ARCH}.tar.xz \
       ${NODEJS_MIRROR}/${NODEJS_VERSION}/node-${NODEJS_VERSION}-${NODEJS_ARCH}.tar.xz \
    && tar -xvf /opt/node-${NODEJS_VERSION}-${NODEJS_ARCH}.tar.xz -C /opt \
    && ln -s /opt/node-${NODEJS_VERSION}-${NODEJS_ARCH}/bin/node /usr/local/bin/node \
    && rm -f /opt/node-${NODEJS_VERSION}-${NODEJS_ARCH}.tar.xz

# Install Go based on architecture
RUN case "${TARGETARCH}" in \
    "amd64") \
        GOLANG_ARCH="linux-amd64" ;; \
    "arm64") \
        GOLANG_ARCH="linux-arm64" ;; \
    *) \
        echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac \
    && wget ${GOLANG_MIRROR}/go${GOLANG_VERSION}.${GOLANG_ARCH}.tar.gz \
    && tar -C /usr/local -xzf go${GOLANG_VERSION}.${GOLANG_ARCH}.tar.gz \
    && ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && rm -f go${GOLANG_VERSION}.${GOLANG_ARCH}.tar.gz

# Run tests
RUN go test -timeout 120s -v ./tests/integration_tests/... 