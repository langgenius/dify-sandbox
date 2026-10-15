#!/usr/bin/env bash
set -euo pipefail
mode=${1:?--measure or --verify required}
shift
[[ ${1:-} == --output && -n ${2:-} && $# == 2 ]] || { echo 'Usage: builder-conformance.sh --measure|--verify --output PATH' >&2; exit 2; }
output=$2
mkdir -p "$(dirname "$output")"
repo=$(cd "$(dirname "$0")/.." && pwd)
case $mode in
 --measure)
  "$repo/scripts/builder-conformance-ci.sh" measure
  sudo cat /var/lib/dify-builder-ci/artifacts/budget-candidate.json | tee "$output" >/dev/null
  ;;
 --verify)
  "$repo/scripts/builder-conformance-ci.sh" verify
  # This constructs unsigned evidence for the normal release owner to attest.
  # Activation separately requires verify-builder-release.py and the deployment
  # controller's authenticated endpoint receipt; construction grants nothing.
  sudo python3 "$repo/tests/conformance/fixture/assemble.py" | tee "$output" >/dev/null
  ;;
 *) echo 'Unknown qualification mode' >&2;exit 2;;
esac
