#!/usr/bin/env bash
# Normal CI provisioning only. Never invoke this on a workstation/shared runner.
set -euo pipefail
umask 077
[[ ${GITHUB_ACTIONS:-} == true && ${RUNNER_ENVIRONMENT:-} == github-hosted && -n ${GITHUB_RUN_ID:-} ]] || { echo 'Fresh GitHub-hosted Linux VM required' >&2; exit 1; }
root=/var/lib/dify-builder-ci
marker=/run/dify-builder-ci-owner
owner="${GITHUB_RUN_ID}:${GITHUB_RUN_ATTEMPT:-1}"
repo=$(cd "$(dirname "$0")/.." && pwd)
owned() { [[ $(sudo cat "$marker" 2>/dev/null) == "$owner" ]]; }
case ${1:-} in
 provision)
  image=${2:?immutable published image index required}
  architecture=${3:?native architecture required}
  [[ $image =~ @sha256:[0-9a-f]{64}$ ]]
  # Root-owned current-job handoff is installed only by the pinned harness's
  # successful authenticate step. A candidate-supplied JSON/verified flag is
  # never consumed. No repository/registry/OIDC token enters fixture setup.
  [[ -z ${GH_TOKEN:-} && -z ${GITHUB_TOKEN:-} && -z ${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-} ]]
  : "${CS5_SOURCE_REVISION:?protected approved source required}"
  : "${CS5_HARNESS_REVISION:?protected harness revision required}"
  [[ $(git -C "$repo" rev-parse HEAD) == "$CS5_HARNESS_REVISION" ]]
  [[ $(sudo stat -c '%u:%a' /run/dify-builder-verified-build.json) == 0:600 ]]
  sudo python3 - "$repo/scripts/verify-builder-build-binding.py" "$image" "$architecture" "$CS5_SOURCE_REVISION" "$CS5_HARNESS_REVISION" "$GITHUB_RUN_ID" "${GITHUB_RUN_ATTEMPT:-1}" <<'PY'
import importlib.util,json,pathlib,sys
script,image,arch,source,harness,run,attempt=sys.argv[1:]
spec=importlib.util.spec_from_file_location('trusted_build_binding',script)
verifier=importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)
r=json.loads(pathlib.Path('/run/dify-builder-verified-build.json').read_text())
verifier.verify_handoff(r,image,source,harness,run,attempt,arch)
PY
  [[ $(uname -s) == Linux && $(cat /proc/1/comm) == systemd ]]
  case $(uname -m):$architecture in x86_64:amd64|aarch64:arm64) ;; *) echo 'Native host/image architecture mismatch' >&2; exit 1;; esac
  # shellcheck source=/dev/null
  . /etc/os-release
  [[ $ID == ubuntu && $VERSION_ID == 24.04 ]]
  version=$(systemctl show --property=Version --value); [[ $version =~ ^([0-9]+) ]]; (( BASH_REMATCH[1] >= 254 ))
  [[ $(stat -fc %T /sys/fs/cgroup) == cgroup2fs && $(cat /sys/module/apparmor/parameters/enabled) == Y ]]
  [[ ! -e $root && ! -e $marker && ! -e /run/netns/dify-builder-ci ]]
  if id dify-builder-ci >/dev/null 2>&1; then echo 'Fixture account already exists' >&2;exit 1;fi
  if getent passwd | awk -F: '$3 >= 20000 && $3 < 20512 {found=1} END {exit !found}'; then echo 'Fixture UID lease range is occupied' >&2;exit 1;fi
  # The authenticated image was pulled before the registry token was removed.
  actual_arch=$(docker image inspect --format '{{.Architecture}}' "$image")
  [[ $actual_arch == "$architecture" ]]
  config_id=$(docker image inspect --format '{{.Id}}' "$image")
  sudo python3 - "$architecture" "$config_id" <<'PY'
import json,pathlib,sys
r=json.loads(pathlib.Path('/run/dify-builder-verified-build.json').read_text())
assert r['platforms'][sys.argv[1]]['config_digest']==sys.argv[2],'executed config mismatch before export'
PY
  printf '%s\n' "$owner" | sudo tee "$marker" >/dev/null
  sudo useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin dify-builder-ci
  sudo install -d -m 0755 "$root" "$root/image" "$root/qualification" "$root/observations" "$root/ordinary-conf" "$root/ordinary-dependencies" "$root/ordinary-state"
  sudo install -d -m 0700 "$root/state" "$root/state/work"
  sudo install -d -m 0700 -o 65534 -g 65534 "$root/artifacts"
  container=$(docker create --platform "linux/$architecture" --network none --cap-drop ALL --security-opt no-new-privileges "$image")
  trap 'docker rm "$container" >/dev/null 2>&1 || true' EXIT
  docker export "$container" | sudo tar -xpf - -C "$root/image"
  docker rm "$container" >/dev/null
  trap - EXIT
  for binary in conformance-candidate request-worker conformance conformance-canary main; do sudo test -x "$root/image/$binary"; done
  sudo apt-get update
  sudo apt-get install -y --no-install-recommends apparmor apparmor-utils iproute2
  sudo python3 "$repo/tests/conformance/fixture/prepare.py" "$architecture"
  sudo chown -R dify-builder-ci:dify-builder-ci "$root/state" "$root/observations"
  sudo chown dify-builder-ci:dify-builder-ci "$root/qualification/server.yaml"
  sudo python3 - "$architecture" <<'PY'
import json,pathlib,sys
root=pathlib.Path('/var/lib/dify-builder-ci')
r=json.loads(pathlib.Path('/run/dify-builder-verified-build.json').read_text())
(root/'index.json').write_text(r['index_raw'])
(root/'platform.json').write_text(r['platforms'][sys.argv[1]]['manifest_raw'])
PY
  sudo python3 "$repo/tests/conformance/fixture/identity.py" "$image" "$architecture" "$CS5_SOURCE_REVISION" "${GITHUB_RUN_ID}:${GITHUB_RUN_ATTEMPT:-1}" "systemd-$version" "$config_id" "$CS5_HARNESS_REVISION"
  sudo install -m 0444 "$repo/tests/conformance/fixture/start-candidate.sh" "$root/qualification/start-candidate.sh"
  # Setup-only authority in a fresh owned namespace. There is no default route,
  # veth or host routing change. Candidate never receives outer NET_ADMIN.
  sudo ip netns add dify-builder-ci
  sudo ip -n dify-builder-ci link set lo up
  for address in 192.0.2.10/32 192.0.2.20/32 2001:db8::10/128 2001:db8::20/128; do sudo ip -n dify-builder-ci address add "$address" dev lo; done
  [[ -z $(sudo ip -n dify-builder-ci route show default) && -z $(sudo ip -n dify-builder-ci -6 route show default) ]]
  sudo install -m 0644 "$repo/tests/conformance/fixture/apparmor.profile" /etc/apparmor.d/dify-builder-ci
  sudo apparmor_parser --replace /etc/apparmor.d/dify-builder-ci
  for role in candidate canary integration ordinary; do
   sudo install -m 0644 "$repo/tests/conformance/fixture/$role.service" "/etc/systemd/system/dify-builder-$role.service"
  done
  for role in candidate canary integration ordinary; do
   sudo systemd-analyze verify "/etc/systemd/system/dify-builder-$role.service"
  done
  sudo systemctl daemon-reload
  # Actual-image negative control in a separate disposable container. Only the
  # synthetic evidence directory is writable; no credentials/socket/network.
  sudo install -d -m 0700 "$root/drift"
  docker run -i --rm --network none --cap-drop ALL --security-opt no-new-privileges --memory 1g --pids-limit 64 \
   --mount "type=bind,source=$root/drift,target=/evidence" -e CS5_DRIFT_OUTPUT=/evidence/dependency-drift.json \
   --entrypoint /usr/local/bin/python3 "$image" - < "$repo/tests/conformance/dependencies/tamper.py"
  sudo install -m 0444 "$root/drift/dependency-drift.json" "$root/qualification/dependency-drift.json"
  sudo systemctl start dify-builder-canary.service dify-builder-candidate.service dify-builder-ordinary.service
  sudo python3 "$repo/tests/conformance/fixture/readiness.py"
  ;;
 measure)
  owned
  sudo install -d /etc/systemd/system/dify-builder-integration.service.d
  printf '[Service]\nExecStart=\nExecStart=/conformance --measure --endpoint http://127.0.0.1:8194 --output /artifacts/budget-candidate.json\n' | sudo tee /etc/systemd/system/dify-builder-integration.service.d/mode.conf >/dev/null
  sudo systemctl daemon-reload
  sudo systemctl start dify-builder-integration.service
  ;;
 freeze)
  owned
  # New service generation; preserve measurement artifacts, discard only owned
  # successful execution tombstones after the unit is fully dead/clean.
  sudo systemctl stop dify-builder-candidate.service
  sudo python3 "$repo/tests/conformance/fixture/freeze.py"
  sudo systemctl start dify-builder-candidate.service
  sudo python3 "$repo/tests/conformance/fixture/readiness.py"
  ;;
 verify)
  owned
  sudo rm -f /etc/systemd/system/dify-builder-integration.service.d/mode.conf
  sudo systemctl daemon-reload
  sudo systemctl start dify-builder-integration.service
  ;;
 logs)
  owned || exit 0
  uname -a
  systemctl --version
  for role in candidate canary ordinary integration; do
   sudo systemctl show "dify-builder-$role.service" --property=User,Group,Delegate,DelegateSubgroup,CPUQuotaPerSecUSec,MemoryMax,TasksMax,NoNewPrivileges,AppArmorProfile,ControlGroup,CapabilityBoundingSet,SystemCallFilter,SystemCallErrorNumber,NetworkNamespacePath,RootDirectory
   sudo journalctl --no-pager -u "dify-builder-$role.service" -n 200
  done
  sudo aa-status --json
  sudo journalctl --dmesg --no-pager --grep=dify-builder -n 100 || true
  ;;
 cleanup)
  owned || exit 0
  for role in integration candidate ordinary canary; do sudo systemctl kill --kill-whom=all --signal=KILL "dify-builder-$role.service" || true; sudo systemctl stop "dify-builder-$role.service" || true; done
  sudo ip netns delete dify-builder-ci
  sudo rm -f "$root/client.env" "$root/ordinary.env" "$root/qualification/server.yaml" "$root/ordinary-conf/config.yaml" "$marker" /run/dify-builder-verified-build.json
  ;;
 *) echo 'Usage: builder-conformance-ci.sh provision IMAGE ARCH | measure | freeze | verify | logs | cleanup' >&2;exit 2;;
esac
