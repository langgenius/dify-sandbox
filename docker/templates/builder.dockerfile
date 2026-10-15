# Frozen Builder artifact. No request-time package install, config or credentials
# are copied into /environment-root. Requalify every changed artifact digest.
FROM golang:1.24.11-bookworm@sha256:656be510c8b4d33acf4eac8575b7f04a3e30b705c152dc6bde112dbe1a87b602 AS build
ENV GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2
RUN printf 'deb [check-valid-until=no] https://snapshot.debian.org/archive/debian/20260301T000000Z bookworm main\n' > /etc/apt/sources.list && rm -f /etc/apt/sources.list.d/* && apt-get update && apt-get install -y --no-install-recommends libseccomp-dev pkg-config
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -buildmode=c-shared -o internal/core/runner/python/python.so ./cmd/lib/python && go build -buildmode=c-shared -o internal/core/runner/nodejs/nodejs.so ./cmd/lib/nodejs
RUN go build -o /out/request-worker ./cmd/request-worker && go build -o /out/environment-manifest ./cmd/environment-manifest && go build -o /out/conformance-candidate ./tests/conformance/candidate && go build -o /out/main ./cmd/server && go build -o /out/dependency-launch ./tests/conformance/dependency-launch
RUN CGO_ENABLED=0 go build -o /out/network-gateway ./cmd/network-gateway
RUN CGO_ENABLED=0 go build -o /out/conformance ./tests/conformance && CGO_ENABLED=0 go build -o /out/conformance-canary ./tests/conformance/canary

FROM node:20.11.1-bookworm-slim@sha256:357deca6eb61149534d32faaf5e4b2e4fa3549c2be610ee1019bf340ea8c51ec AS node
WORKDIR /build/node
COPY dependencies/builder/node-package.json ./package.json
COPY dependencies/builder/node-package-lock.json ./package-lock.json
RUN npm ci --ignore-scripts --no-audit --no-fund
# Reviewed locked koffi install selects/checks the bundled native prebuild.
# It cannot download an unpinned fallback; unsupported builds fail here.
RUN --network=none node node_modules/koffi/src/cnoke/cnoke.js -p node_modules/koffi -d node_modules/koffi/src/koffi --prebuild && node -e "if(require('koffi').version !== '2.10.1') process.exit(1)"

FROM python:3.12.12-slim-bookworm@sha256:593bd06efe90efa80dc4eee3948be7c0fde4134606dd40d8dd8dbcade98e669c AS runtime
RUN printf 'deb [check-valid-until=no] https://snapshot.debian.org/archive/debian/20260301T000000Z bookworm main\n' > /etc/apt/sources.list && rm -f /etc/apt/sources.list.d/* && apt-get update && apt-get install -y --no-install-recommends libseccomp2 libstdc++6 libtk8.6 libtcl8.6 && rm -rf /var/lib/apt/lists/*
COPY dependencies/builder/ /build/
RUN pip3 install --require-hashes --only-binary=:all: --no-cache-dir --no-compile -r /build/python.lock
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/LICENSE /build/node-LICENSE
COPY --from=node /build/node /build/node
COPY --from=build /src/internal/core/runner/python/python.so /build/python.so
COPY --from=build /src/internal/core/runner/nodejs/nodejs.so /build/nodejs.so
COPY --from=build /out/ /
COPY scripts/assemble-builder-environment.py /build/assemble.py
RUN python3 /build/assemble.py
RUN mkdir -p /environment-root/usr/local/libexec && cp /network-gateway /environment-root/usr/local/libexec/network-gateway && chmod 0555 /environment-root/usr/local/libexec/network-gateway
# Confirm dependency usability in the final chroot, with the sandbox UID.
COPY tests/conformance/dependencies/python.py tests/conformance/dependencies/node.js /environment-root/work/
RUN PYTHONHOME=/usr/local LD_LIBRARY_PATH=/usr/local/lib NODE_PATH=/opt/node/node_modules chroot --userspec=65537:65537 /environment-root /usr/bin/python3 /work/python.py && PYTHONHOME=/usr/local LD_LIBRARY_PATH=/usr/local/lib NODE_PATH=/opt/node/node_modules chroot --userspec=65537:65537 /environment-root /usr/bin/node /work/node.js
RUN rm /environment-root/work/python.py /environment-root/work/node.js && /environment-manifest -root /environment-root
ENV SANDBOX_PROFILE=builder
ENTRYPOINT ["/conformance-candidate"]
