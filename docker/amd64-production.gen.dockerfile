# Production environment Dockerfile template
ARG PYTHON_VERSION=3.10-slim-bookworm
ARG DEBIAN_MIRROR="http://deb.debian.org/debian testing main"
ARG PYTHON_PACKAGES="httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]"
ARG NODEJS_VERSION=v20.11.1
ARG NODEJS_MIRROR="https://npmmirror.com/mirrors/node"
ARG TARGETARCH

FROM python:3.10-slim-bookworm

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

# Copy binary files
COPY main /main
COPY env /env

# Copy configuration files
COPY conf/config.yaml /conf/config.yaml
COPY dependencies/python-requirements.txt /dependencies/python-requirements.txt
COPY docker/entrypoint.sh /entrypoint.sh

# Set permissions and install dependencies
RUN chmod +x /main /env /entrypoint.sh \
    && pip3 install --no-cache-dir httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]

# Download Node.js based on architecture and run environment initialization
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
    && export NODE_TAR_XZ="/opt/node-v20.11.1-${NODEJS_ARCH}.tar.xz" \
    && export NODE_DIR="/opt/node-v20.11.1-${NODEJS_ARCH}" \
    && /env \
    && rm -f /env

# Set environment variables (dynamically set, replaced by generate.sh at runtime)
ENV NODE_TAR_XZ=/opt/node-v20.11.1-linux-x64.tar.xz
ENV NODE_DIR=/opt/node-v20.11.1-linux-x64

ENTRYPOINT ["/entrypoint.sh"] 