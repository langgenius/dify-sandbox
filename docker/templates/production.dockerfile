# Production environment Dockerfile template
ARG PYTHON_VERSION=python:3.12-slim-bookworm
ARG PYTHON_PACKAGES="httpx==0.27.2 requests==2.32.3 jinja2==3.1.6 PySocks httpx[socks]"
ARG NODEJS_VERSION=v20.11.1

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

# Copy Node.js tarball (pre-downloaded in CI to avoid network issues during build)
COPY docker/node-dist/node-${NODEJS_VERSION}-linux-__ARCH__.tar.xz /opt/node-${NODEJS_VERSION}-linux-__ARCH__.tar.xz

# Run environment initialization
RUN /env && rm -f /env

# Set environment variables (dynamically set, replaced by generate.sh at runtime)
ENV NODE_TAR_XZ=/opt/node-${NODEJS_VERSION}-linux-__ARCH__.tar.xz
ENV NODE_DIR=/opt/node-${NODEJS_VERSION}-linux-__ARCH__

ENTRYPOINT ["/entrypoint.sh"]
