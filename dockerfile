FROM python:3.10-slim

# copy main binary to /main
COPY main /main
COPY conf/config.yaml /conf/config.yaml

# change source to TSINGHUA if environment['TSINGHUA'] is set
ARG TSINGHUA

RUN apt-get clean && \
    apt-get update && apt-get install -y pkg-config libseccomp-dev \
    && rm -rf /var/lib/apt/lists/* \
    && chmod +x /main \
    && pip3 install jinja2

ENTRYPOINT ["/main"]