# Base Dockerfile template - shared system dependencies installation logic
ARG PYTHON_VERSION=3.10-slim-bookworm
FROM python:${PYTHON_VERSION}

# Build arguments
ARG DEBIAN_MIRROR="http://deb.debian.org/debian testing main"

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

# Install Python dependencies
ARG PYTHON_PACKAGES="httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]"

RUN pip3 install --no-cache-dir ${PYTHON_PACKAGES} 