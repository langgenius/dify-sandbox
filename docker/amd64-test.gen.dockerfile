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
FROM golang:1.23.9 AS builder

COPY . /app
WORKDIR /app

# Install build dependencies and build
RUN apt-get update && apt-get install -y pkg-config gcc libseccomp-dev \
    && go mod tidy \
    && case "amd64" in \
       "amd64") bash ./build/build_amd64.sh ;; \
       "arm64") bash ./build/build_arm64.sh ;; \
       *) echo "Unsupported architecture: amd64" && exit 1 ;; \
       esac

# Test stage
FROM python:3.10-slim-bookworm as tester

ARG DEBIAN_MIRROR
ARG PYTHON_PACKAGES
ARG NODEJS_VERSION
ARG NODEJS_MIRROR
ARG GOLANG_VERSION
ARG GOLANG_MIRROR
ARG TARGETARCH

# Install system dependencies
RUN echo "deb http://deb.debian.org/debian testing main" > /etc/apt/sources.list \
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
RUN pip3 install --no-cache-dir httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]

# Install Node.js based on architecture
RUN case "amd64" in \
    "amd64") \
        NODEJS_ARCH="linux-x64" ;; \
    "arm64") \
        NODEJS_ARCH="linux-arm64" ;; \
    *) \
        echo "Unsupported architecture: amd64" && exit 1 ;; \
    esac \
    && wget -O /opt/node-v20.11.1-${NODEJS_ARCH}.tar.xz \
       https://npmmirror.com/mirrors/node/v20.11.1/node-v20.11.1-${NODEJS_ARCH}.tar.xz \
    && tar -xvf /opt/node-v20.11.1-${NODEJS_ARCH}.tar.xz -C /opt \
    && ln -s /opt/node-v20.11.1-${NODEJS_ARCH}/bin/node /usr/local/bin/node \
    && rm -f /opt/node-v20.11.1-${NODEJS_ARCH}.tar.xz

# Install Go based on architecture
RUN case "amd64" in \
    "amd64") \
        GOLANG_ARCH="linux-amd64" ;; \
    "arm64") \
        GOLANG_ARCH="linux-arm64" ;; \
    *) \
        echo "Unsupported architecture: amd64" && exit 1 ;; \
    esac \
    && wget https://golang.org/dl/go1.23.9.${GOLANG_ARCH}.tar.gz \
    && tar -C /usr/local -xzf go1.23.9.${GOLANG_ARCH}.tar.gz \
    && ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && rm -f go1.23.9.${GOLANG_ARCH}.tar.gz

# Run tests
RUN go test -timeout 120s -v ./tests/integration_tests/... 