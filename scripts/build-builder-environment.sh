#!/bin/sh
set -eu
# Build a local, architecture-specific candidate. This does not release or qualify
# isolation. Buildx cross-platform qualification is a separate CS5 obligation.
cd "$(dirname "$0")/.."
exec docker build --progress=plain -f docker/templates/builder.dockerfile -t "${BUILDER_IMAGE:-dify-sandbox:builder-candidate}" .
