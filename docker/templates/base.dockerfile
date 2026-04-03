# Base Dockerfile template - shared system dependencies installation logic
ARG PYTHON_VERSION=python:3.14-slim-bookworm
FROM ${PYTHON_VERSION}

# Install system dependencies
RUN apt-get update \
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
       passwd \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
ARG PYTHON_PACKAGES="httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]"

RUN pip3 install --no-cache-dir ${PYTHON_PACKAGES}
