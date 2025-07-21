# Production environment Dockerfile template
ARG PYTHON_VERSION=3.10-slim-bookworm
ARG DEBIAN_MIRROR="http://deb.debian.org/debian testing main"
ARG PYTHON_PACKAGES="httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]"
ARG NODEJS_VERSION=v20.11.1
ARG NODEJS_MIRROR="https://npmmirror.com/mirrors/node"
ARG TARGETARCH

FROM python:${PYTHON_VERSION}

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

# Copy binary files
COPY main /main
COPY env /env

# Copy configuration files
COPY conf/config.yaml /conf/config.yaml
COPY dependencies/python-requirements.txt /dependencies/python-requirements.txt
COPY docker/entrypoint.sh /entrypoint.sh

# Set permissions and install dependencies
RUN chmod +x /main /env /entrypoint.sh \
    && pip3 install --no-cache-dir ${PYTHON_PACKAGES}

# Download Node.js based on architecture and run environment initialization
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
    && export NODE_TAR_XZ="/opt/node-${NODEJS_VERSION}-${NODEJS_ARCH}.tar.xz" \
    && export NODE_DIR="/opt/node-${NODEJS_VERSION}-${NODEJS_ARCH}" \
    && /env \
    && rm -f /env

# Set environment variables (dynamically set, replaced by generate.sh at runtime)
ENV NODE_TAR_XZ=/opt/node-${NODEJS_VERSION}-linux-__ARCH__.tar.xz
ENV NODE_DIR=/opt/node-${NODEJS_VERSION}-linux-__ARCH__

ENTRYPOINT ["/entrypoint.sh"] 