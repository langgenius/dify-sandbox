# CS1 request isolation conformance

The unit target uses Linux Go 1.24.11 pinned by the multi-platform manifest
`sha256:656be510c8b4d33acf4eac8575b7f04a3e30b705c152dc6bde112dbe1a87b602`.
It builds the existing native seccomp libraries in the disposable image, has no
network at runtime, no host source mounts, no credentials and no Docker socket.
Go compilation parallelism is 2 to respect its finite PID budget.

```
docker compose -f tests/conformance/compose.yaml build unit integration
docker compose -f tests/conformance/compose.yaml run --rm unit go test ./internal/core/runner/isolation -run 'TestRejectsInvalidBudget|TestIndependentRoots' -count=1
docker compose -f tests/conformance/compose.yaml run --rm unit go test -race ./internal/core/runner/... ./internal/controller ./tests/conformance/... -count=1
docker compose -f tests/conformance/compose.yaml run --rm integration --case roots-and-budgets
```

The integration command is deliberately a failing prerequisite check until CS3
and CS5 supply the actual qualified candidate image and fixture. It does not
skip, infer capabilities from a Docker label, or fall back to ordinary execution.
The harness requires controller results AND independent supervisor observations;
user stdout cannot stand in for namespace/cgroup evidence.

## Qualified Linux runner prerequisites

* Build `cmd/request-worker` and `tests/conformance/candidate` into the actual
  candidate image. The production server does not invoke the synthetic resolver.
* Install the CS3 immutable language environment at the profile's EnvironmentRoot.
  It contains Python and Node executables, their normal libraries/dependencies,
  the rebuilt native seccomp libraries (including `DifySeccompIsolated`), the Node `koffi` dependency, an empty
  `/work` mountpoint, and no credentials, sockets or host proc/dev mounts.
  The environment bind is non-recursive and recursively read-only; `/work` is
  the sole writable tmpfs and TMPDIR is `/work/tmp`.
* Provision an empty delegated cgroup-v2 subtree outside the environment and
  work roots. The runner must enable `cpu`, `memory`, and `pids` controllers on
  its parent, allow child `cgroup.procs` writes, and support `cgroup.kill`,
  `memory.swap.max`, `memory.oom.group`, and `memory.peak`. Mount only that narrow
  delegation at the profile's CgroupRoot using the CI-owned runtime setup. Do not
  mount `/sys/fs/cgroup` broadly and do not use privileged containers.
* Supply scoped Linux namespace/mount/chroot/setuid/setgid capabilities and a
  seccomp profile admitting the wrapper's `clone`, `mount`, `mount_setattr`,
  `chroot`, `close_range`, `prctl` and rlimit setup. The native language seccomp
  layer still applies. The provided capability list does not itself establish
  delegation or qualify Docker's seccomp profile.
* Populate the `qualification` volume with a server-owned `profile.json` (Go
  `isolation.Profile` JSON) and use finite synthetic limits with concurrency at
  least 2 and enough wall time for two language starts. CPUTimeMillis must be
  low enough for a busy process to exceed it before WallMillis at CPUQuotaMicros
  / CPUPeriodMicros. These fixtures are not service release budget measurements.
* Mount the separate `observations` volume writable in the candidate and read-only
  in the harness. Only synthetic observation JSON is written there. There is no
  public introspection route. Start the candidate/canary using the
  `qualified-linux` compose profile after provisioning; keep their network
  internal. CS4 extends the otherwise unused canary with network-scope assertions.

The enumerated case checks mixed Python/Node cwd and independent roots,
mount/PID/IPC/UTS/network namespace IDs, cgroup controls, serial reuse, resource
exhaustion, peer survival and clean removal. A resource stress failure must have
matching kernel/errno evidence. In particular, native seccomp refusing fork
before `pids.max` is reached does not falsely qualify PID exhaustion; CS5 must
resolve the useful supported workload and retain the failure evidence.

## Composition and wire contract

Production uses `controller.SetupBuilder` with a constructed supervisor and a
server-owned reservation resolver. `Setup` remains ordinary mode. Missing
options on Builder deployments are refused, never routed to legacy execution.
The HTTP owner must configure finite read/header/write timeouts. The synthetic
candidate does this explicitly and accepts only test reservation references.

`options` contains numeric `schema_version: 1`, execution/language/digest fields,
`budget_reservation_ref`, the budget and `network_scope`. Code/preload and the
matching language are at the top level. Unknown/duplicate fields are rejected.
The initial network representation is exactly `{"mode":"none"}`.
`budget_reservation_ref` is a lookup reference, not authority; there is no
production permissive/default resolver.

For the current Go protocol, InvocationDigest is SHA-256 of `json.Marshal(spec)`
with InvocationDigest set to the empty string, a NUL byte, then compact
`json.Marshal(Payload{Code, Preload})`. The spec fields follow contracts.go's
order; encoding uses Go's standard JSON string escaping. CS6 must use this exact
versioned encoder or coordinate a canonical-format change with this owner.

Completed executions remain in a bounded replay registry. At capacity it refuses
new execution IDs; it never evicts tombstones and redispatches code. Production
restart/replay persistence is supplied by the common owner in CS6. Failed cleanup
quarantines the admission reservation. CS2 extends bounded cancellation/output
termination and cleanup behavior; CS5 owns the actual-image release evidence.

The isolated entry point changes root/cwd before filtering, while the ordinary
entry point retains its existing behavior. Isolated-only filesystem additions
are `getcwd`, `newfstatat`, `readlinkat`, `getdents64`, `fstat`, `lseek`, `statfs`
and `fstatfs`; their names resolve for the target architecture through libseccomp.
No network syscalls are added and the isolated C ABI refuses a true network flag.
Current native filter unit evidence is Linux arm64 only; amd64 and actual useful
import workloads remain part of the CS3/CS5 image qualification.

Review follow-up: every replay passes full bounded payload/profile/reservation
validation before registry reuse. New identities atomically claim one pending
owner and one finite admission ticket; duplicates do not reserve worker memory
or queue slots again. Pending cancellation/deadline results retain their owner.

Concurrent conformance now rejects MaxConcurrent/Concurrent below 2. Fixed
workloads emit bounded READY/ALIVE markers; private observations record monotonic
supervisor lifecycle intervals. READY/ALIVE markers are accepted only while
their own cgroup is populated, so buffered output after exit cannot prove
liveness. The synthetic recorder samples the already
trusted delegated peer cgroup's `cgroup.events` for `populated 1`, without reading
`/proc/<pid>/root`, adding capabilities, or exposing an observer route to code.
Mixed-language cases require overlapping ready-to-exit intervals and a live-peer
kernel sample during that overlap. For every stress case the harness waits for
peer readiness before dispatching the offender. The peer has a finite keepalive
of half the profile wall budget; the offender gets one quarter of that budget.
The final evidence must show peer readiness before offender start, an independent
populated-peer sample after offender exit, a later peer heartbeat, and clean peer
success after that sample. A serialized or early-finished peer cannot qualify.
Configure the synthetic CPUTimeMillis low enough to exceed the total CPU budget
within the offender's reduced wall deadline at the actual quota/period ratio.
Actual kernel/image qualification remains pending CS3/CS5; unit fixtures exercise
the evidence rejection/acceptance logic and do not substitute for it.

## CS2 lifecycle qualification

`--case lifecycle` requires the same qualified actual image/delegation as CS1.
Start the candidate with `--lifecycle-reconcile` before invoking this case. The
candidate privately starts a real native workload in a child supervisor, kills
that supervisor, reconciles its durable journal, verifies the original process
identities are absent, and verifies reuse of the native UID by a healthy peer.
It also replaces an owned root with a symlink as a service-side cleanup fault,
checks quarantine plus a successful peer, repairs the test fault, restarts, and
checks reconciliation and parent FD counts. The proof is written only through
the private observations volume; there is no public test-control endpoint.
Use a fresh disposable qualification instance for this probe: fixed crash-probe
IDs intentionally remain durable replay tombstones and cannot be rerun as code.
The probe's journal and roots must persist across supervisor processes.

The candidate now uses the ordinary server authentication middleware, configured
by `/qualification/server.yaml` with a nonempty `app.key`. Supply the same secret
as `SANDBOX_API_KEY` in the integration client; no key enters child execution.
Production Builder composition refuses an empty server key. The ordinary
endpoint's deployment/configuration is unchanged.

The HTTP lifecycle case checks stdout/stderr flooding, UTF-8 byte boundaries,
PID-namespace descendant pipe cleanup, TERM-ignoring cancellation, wrong digest,
duplicate cancel, disconnect after native readiness, terminal same-ID retrieval,
and successful subsequent execution. Missing native fork/signal support is a
failed prerequisite, not a skip or a simulated pass. Unit tests separately cover
pre-dispatch cancellation, queue races, untrusted journal paths, unavailable
quarantined capacity, journal write failure, and fake-cgroup refusal.

`GET /v1/sandbox/executions/:id` and `POST .../:id/cancel` require `X-Api-Key`
and `X-Invocation-Digest`. GET returns output-free bounded status. Cancel requests
termination idempotently and returns current status; poll GET for completion.
A fully validated duplicate run retrieves a live owned terminal result. After
restart, the nonsecret journal cannot restore result bytes: the terminal identity
returns `unknown/result_unavailable`, never success or a second dispatch.

`Profile.JournalRoot` is trusted constructor configuration only; if omitted it
is `WorkRoot + ".journal"`. It must be outside all child environment/work/cgroup
roots, service-owned, mode 0700, and nonsymlink. Records/lock are 0600, filenames
hash execution IDs, and writes use file fsync + atomic rename + directory fsync.
An exclusive lifetime lock prevents two supervisors from owning the same records.
Startup reconciles before intake; unknown unjournaled roots fail startup. Failed
cleanup retains its slot/root and never returns capacity until successful restart
reconciliation. All-quarantined capacity returns explicit unavailable.
