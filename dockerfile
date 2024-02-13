FROM python:3.10-slim

# copy main binary to /main
COPY main /main
COPY conf/config.yaml /conf/config.yaml

RUN apt-get update && apt-get install -y \
    pkg-config libseccomp-dev \
    && rm -rf /var/lib/apt/lists/* \
    && chmod +x /main

ENTRYPOINT ["/main"]