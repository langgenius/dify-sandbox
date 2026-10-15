#!/bin/sh
set -eu
# /delegated is ONLY this service's unit subtree; no host hierarchy discovery.
[ "$(stat -fc %T /delegated)" = cgroup2fs ]
[ -z "$(cat /delegated/cgroup.procs)" ]
case "$(cat /proc/self/cgroup)" in
 0::/system.slice/dify-builder-candidate.service/manager) ;;
 *) echo 'DelegateSubgroup=manager placement required' >&2; exit 1 ;;
esac
printf '+cpu +memory +pids\n' > /delegated/cgroup.subtree_control
mkdir -p /delegated/scopes
printf '100000 100000\n' > /delegated/scopes/cpu.max
printf '3221225472\n' > /delegated/scopes/memory.max
printf '0\n' > /delegated/scopes/memory.swap.max
printf '448\n' > /delegated/scopes/pids.max
printf '+cpu +memory +pids\n' > /delegated/scopes/cgroup.subtree_control
exec /conformance-candidate --lifecycle-reconcile
