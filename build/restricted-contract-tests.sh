#!/usr/bin/env bash
# Disposable Linux CI contract fixture only. This is not a release artifact.
set -euo pipefail

stage="${CONTRACT_STAGE:-}"
arch="${CONTRACT_ARCH:-}"
[[ "$stage" =~ ^[1-8]$ ]] || { echo 'CONTRACT_STAGE must be 1..8' >&2; exit 1; }
[[ "$arch" =~ ^(amd64|arm64)$ ]] || { echo 'CONTRACT_ARCH must be amd64 or arm64' >&2; exit 1; }
[[ "$(uname -s)" == Linux ]] || { echo 'Linux CI only' >&2; exit 1; }
case "$(uname -m):$arch" in
  x86_64:amd64|aarch64:arm64) ;;
  *) echo 'Native CI architecture does not match CONTRACT_ARCH' >&2; exit 1 ;;
esac
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Reject unimplemented stages and malformed/empty inventories before building.
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
python3 - "$stage" > "$workdir/inventory.tsv" <<'PY'
import json, re, sys
inventory = json.load(open('build/restricted-contract-tests.json'))
selected = inventory.get(sys.argv[1])
if not isinstance(selected, dict) or not selected:
    raise SystemExit('stage inventory is unavailable')
for package, tests in selected.items():
    if not re.fullmatch(r'\./[A-Za-z0-9_/-]+', package) or not isinstance(tests, list) or not tests or len(set(tests)) != len(tests):
        raise SystemExit('invalid stage inventory')
    if any(not isinstance(test, str) or not re.fullmatch(r'Test[A-Za-z0-9_]+', test) for test in tests):
        raise SystemExit('invalid required test name')
    print(package + '\t^(' + '|'.join(tests) + ')$')
PY

verify_results() {
  python3 - "$stage" "$1" <<'PY'
import json, sys
inventory = json.load(open('build/restricted-contract-tests.json'))[sys.argv[1]]
module = 'github.com/langgenius/dify-sandbox'
expected = {(module + package[1:], test) for package, tests in inventory.items() for test in tests}
required_packages = {package for package, _ in expected}
passed, packages, started = set(), set(), set()
for line in open(sys.argv[2]):
    event = json.loads(line)
    action, package, test = event.get('Action'), event.get('Package'), event.get('Test')
    if action == 'fail' or (action == 'skip' and (package, test) in expected):
        raise SystemExit('failed or skipped required coverage')
    if (package, test) in expected:
        if action == 'run': started.add((package, test))
        if action == 'pass': passed.add((package, test))
    if test is None and action == 'pass': packages.add(package)
if not expected or expected - passed or expected - started or required_packages - packages:
    raise SystemExit('missing required test/package pass: ' + repr(sorted(expected - passed)))
print('verified ' + str(len(expected)) + ' required top-level test passes')
PY
}

if [[ "${1:-}" == --container ]]; then
  printf 'Contract stage=%s architecture=%s\n' "$stage" "$arch"
  go version
  results="$workdir/results.jsonl"
  : > "$results"
  while IFS=$'\t' read -r package pattern; do
    go test -json -count=1 -timeout 120s -run "$pattern" "$package" | tee -a "$results"
  done < "$workdir/inventory.tsv"
  verify_results "$results"
  # Delete every event for one required test, including run/pass. The verifier
  # must fail even though the original go test command exited successfully.
  python3 - "$stage" "$results" "$workdir/missing.jsonl" <<'PY'
import json, sys
inventory = json.load(open('build/restricted-contract-tests.json'))[sys.argv[1]]
package = next(iter(inventory))
missing = ('github.com/langgenius/dify-sandbox' + package[1:], inventory[package][0])
with open(sys.argv[3], 'w') as output:
    for line in open(sys.argv[2]):
        event = json.loads(line)
        if (event.get('Package'), event.get('Test')) != missing: output.write(line)
PY
  if verify_results "$workdir/missing.jsonl"; then
    echo 'coverage verifier accepted missing required test' >&2; exit 1
  fi
  echo 'coverage verifier missing-test self-test passed'
else
  [[ $# == 0 ]] || { echo 'unsupported harness argument' >&2; exit 1; }
  printf 'Contract fixture stage=%s architecture=%s\n' "$stage" "$arch"
  printf 'Base: %s\n' 'docker.io/langgenius/python:3-debian13-sfw-ent-dev@sha256:e358e4a9c08c1c691bf7c1698c36ae1daee759a55f7f9dd5139136e38e39ae0a'
  bash docker/generate.sh restricted-contract "$arch"
  image="restricted-contract:stage-$stage-$arch"
  # Existing build/build_<arch>.sh owns .so compilation inside the builder.
  docker build --platform "linux/$arch" --build-arg "TARGETARCH=$arch" -t "$image" -f "docker/$arch-restricted-contract.gen.dockerfile" .
  docker run --rm --platform "linux/$arch" -e "CONTRACT_STAGE=$stage" -e "CONTRACT_ARCH=$arch" "$image"
fi
