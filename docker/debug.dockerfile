# ARGUMENTS --------------------------------------------------------------------
# Base image
ARG BASE_IMAGE=python:3.10-slim-bookworm

# Build arguments
ARG GOLANG_VERSION=1.24.9

# ------------------------------------------------------------------------------

# Builder stage
FROM golang:${GOLANG_VERSION} AS builder

# Set working directory
WORKDIR /app

# Copy source code
COPY . /app

# Install build dependencies and build
WORKDIR /app
RUN apt-get update && apt-get install -y pkg-config gcc libseccomp-dev

RUN touch internal/core/runner/python/python.so \
    && touch internal/core/runner/nodejs/nodejs.so \
    && go mod tidy

RUN case "amd64" in \
       "amd64") bash ./build/build_amd64.sh ;; \
       "arm64") bash ./build/build_arm64.sh ;; \
       *) echo "Unsupported architecture: amd64" && exit 1 ;; \
       esac

# Tester stage
FROM ${BASE_IMAGE} AS tester

# Install runtime dependencies
RUN echo "deb http://deb.debian.org/debian testing main" > /etc/apt/sources.list
RUN apt-get update && apt-get install -y \
    pkg-config \
    gcc \
    libseccomp-dev \
    wget \
    curl \
    git \
    strace \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/internal/core/runner/python/python.so /app/internal/core/runner/python/python.so

# Copy configuration files and dependencies
COPY conf/config.yaml /conf/config.yaml
COPY dependencies/python-requirements.txt /dependencies/python-requirements.txt

# Install Python dependencies
RUN pip3 install --no-cache-dir httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]

# Install Go based on architecture
RUN case "amd64" in \
    "amd64") \
        GOLANG_ARCH="linux-amd64" ;; \
    "arm64") \
        GOLANG_ARCH="linux-arm64" ;; \
    *) \
        echo "Unsupported architecture: amd64" && exit 1 ;; \
    esac \
    && wget https://golang.org/dl/go1.24.9.${GOLANG_ARCH}.tar.gz \
    && tar -C /usr/local -xzf go1.24.9.${GOLANG_ARCH}.tar.gz \
    && ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && rm -f go1.24.9.${GOLANG_ARCH}.tar.gz

# Copy source code for testing
COPY . /app

# Run ONLY TestFileUpload
CMD ["go", "test", "-timeout", "120s", "-v", "-run", "TestFileUpload", "./tests/integration_tests/..."]
