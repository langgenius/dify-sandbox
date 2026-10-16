#!/usr/bin/env bash
# Trusted harness script. Build has no registry login, push token or OIDC grant.
set -euo pipefail
source_dir=${1:?reviewed source checkout required}
architecture=${2:?native architecture required}
output=${3:?archive output required}
[[ ${GITHUB_ACTIONS:-} == true && ${RUNNER_ENVIRONMENT:-} == github-hosted ]]
[[ ${BUILDER_APPROVED_SOURCE_REVISION:-} =~ ^[0-9a-f]{40}$ ]]
[[ $(git -C "$source_dir" rev-parse HEAD) == "$BUILDER_APPROVED_SOURCE_REVISION" ]]
[[ -z $(git -C "$source_dir" status --porcelain) ]]
case $(uname -m):$architecture in x86_64:amd64|aarch64:arm64) ;; *) echo 'Native runner required' >&2; exit 1;; esac
mkdir -p "$output"
docker version > "$output/docker-version.log"
docker buildx version > "$output/buildx-version.log"
# Pinned existing runner tools; no mutable setup download or privileged
# docker-container builder. Engine v28.0.4 embeds the Docker BuildKit backend.
# Official source commits: moby/moby 6430e49a55babd9b8f4d08e70ecb2b68900770fe,
# docker/cli b8034c0ed70494a90c133461d145cd072d920d7c,
# docker/buildx 2d379c0c3f22da0d2759d132a0ec81ca949098f0.
[[ $(docker version --format '{{.Server.Version}}:{{.Server.GitCommit}}:{{.Server.Arch}}') == "28.0.4:6430e49:$architecture" ]] || { echo 'Unreviewed Docker server/native platform' >&2; exit 1; }
[[ $(docker version --format '{{.Client.Version}}:{{.Client.GitCommit}}') == 28.0.4:b8034c0 ]] || { echo 'Unreviewed Docker client' >&2; exit 1; }
[[ $(docker buildx version) =~ ^github.com/docker/buildx\ v0\.37\.2\ 2d379c0([0-9a-f]*)$ ]] || { echo 'Unreviewed Buildx' >&2; exit 1; }
docker buildx inspect default > "$output/builder-inspect.log"
[[ $(awk '$1 == "Driver:" {print $2}' "$output/builder-inspect.log") == docker ]] || { echo 'Embedded Docker builder required' >&2; exit 1; }
# Uses the existing reviewed immutable dependency recipe without secret mounts.
docker buildx build --builder default --load --provenance=false --platform "linux/$architecture" --progress=plain -f "$source_dir/docker/templates/builder.dockerfile" \
  -t "builder-native:$architecture" "$source_dir" 2>&1 | tee "$output/build.log"
docker save -o "$output/image.tar" "builder-native:$architecture"
python3 "$(dirname "$0")/prepare-builder-build.py" record "$output" "$BUILDER_APPROVED_SOURCE_REVISION" "$architecture" "$GITHUB_RUN_ID" "$GITHUB_RUN_ATTEMPT"
