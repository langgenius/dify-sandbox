# ENG-1189: bounded restricted CodeSandbox specification

Date: 2026-10-10. Parent: ENG-1183 Release B. Status: root-accepted DESIGN ONLY; no sandbox source changes, native CI GREEN or conformance claim. The linked joint execution contract includes the root ruling permitting ordinary strict JSON wire encoding. This specification supersedes the earlier design's uniform AS budget/concurrency=2. The source base is sandbox repository commit `5631afef06ec88f80c28129aec7fd22a30006b14`.

## Objective and scope

Deliver a separate pinned restricted deployment of the existing `/v1/sandbox/run` HTTP service and native Python/Node runners. Preserve ordinary endpoint, config defaults, response behavior and network-enabled callers. Dify still invokes its existing CodeExecutor and native Graphon Code/TemplateTransform nodes; this project does not introduce a workflow executor or change Graphon. ENG-1189 produces bounded sandbox execution and verifiable release/deployment evidence. Dify parent Tasks 5–6 own eligibility, native persisted workflow acceptance and activation.

Root's subsequent read-only deployment inspection establishes that actual Dev namespace `default` Deployment `dify-sandbox` runs `docker.io/langgenius/dify-ee-sandbox:3.13.0-rc1` with no configured resources. It is distinct from both the audited OSS source and local OSS diagnostic. Preserve that EE deployment. Create a separate `dify-sandbox-restricted` Deployment/Service and explicit Dify restricted endpoint using the new conformed OSS artifact. Never transfer OSS evidence to the EE image or infer its isolation from this work. The separate Helm repository is owned by root; root's deployment owner selects its integration point after read-only preflight, outside the sandbox product patch.

Root additionally observed five arm64 Dev nodes on Linux `5.15.0-1121-azure`, containerd `1.7.34-2`, each allocatable CPU `1900m` and memory approximately 5.6 GiB (eco) or 7 GiB (agent). The dedicated candidate therefore requests `1000m` CPU, limits CPU to `2000m`, and requests/limits memory at `3Gi`; it does not reserve two CPUs. A `2000m` request would be unschedulable on these nodes. Available capacity/contention and actual scheduling remain to be observed. All useful/budget/recovery controls must pass on this real envelope; nominal limits or another kernel's CI result are insufficient. Read-only `auth can-i` returned yes for creating deployments.apps/services, not proof of successful deployment, capacity, image pull or conformance. Actual Helm owner is branch `dev`, `.github/workflows/dev-deploy.yaml`, values `examples/value-aks-arm64.yaml`. Primary checkout `deploy-tagbump-667225ec` is stale with untracked files and must remain untouched; later root-owned deployment changes require isolation from the correct branch.

No implementation, helpers/reviewers, service calls, builds, source/config edits, pushes or deployment are authorized by this planning artifact. Root preflight precedes execution. ENG-1177 search remains paused. All frontend implementation remains deferred until the parent backend roadmap gates pass. Historical native Runs/publications remain untouched.

Execution contract: [joint execution contract](joint-execution-contract.md), required with this specification.

This standalone design incorporates the accepted bounded-sandbox decisions and startup observations below. The parent Dify safe-execution roadmap owns receipt admission, native persisted acceptance and release; its detailed signing/operator implementation is an external dependency for Tasks 6/8, summarized in the trust handoff sections here. No unavailable parent scratch file is required for Tasks 1–5. [Implementation plan](../plans/2026-10-10-restricted-sandbox.md).

## Established facts versus candidates

The pinned source has unbounded capture/service accumulation, a timeout that kills only the interpreter PID, shared Python roots, Node's process-global cwd mutation, runtime dependency mutation and racy request admission. No complete request resource owner exists. The previous request-network-False probe remains accepted; repeat only focused conformance controls, not broad feasibility work.

The supplied local arm64 diagnostic's direct full filter calls returned `42`/clean stderr for Python at 1 GiB AS and Node at 1.5 and 2 GiB. Node at 1 GiB exited 2 with a Go page-summary reservation failure. A library-only case that printed 42 with fatal stderr is not a pass. These children used different environment/root/UID setup from native leased runners. Thus 1/1.5 GiB are measured startup candidates, not full native resource or isolation proof. Architecture, image and deployment evidence must be obtained afresh.

## Supported profile

The sandbox deployment mode is `restricted-v1` (default off). Dify's externally named capability remains `network-disabled-v1`. These labels are selectors, never proof. Support only `python3` and `nodejs`; Jinja uses Python. All restricted generated code executes with network false, no caller preload, no mutable dependencies, no inherited service credentials/handles, no writable filesystem directory, and no generated subprocesses. Runtime libraries/imports needed for deterministic Python/JS/Jinja remain usable.

The server configuration has one mode authority: `Mode string` accepts `ordinary` or `restricted`, with absent/empty meaning ordinary. Validation derives internal `RestrictedMode bool` (`yaml:"-"`) for later consumers; users cannot set that internal flag independently. Ordinary parsing/default behavior is preserved, and restricted unknown/invalid/spoofed configuration refuses startup. Selecting restricted config does not establish readiness or a trusted capability.

Deploy one service replica, one admitted request, no queue. Parallel HTTP requests still exercise admission/cancellation races; this profile makes no claim of two simultaneously executing children. A second replica or increased concurrency is a different deployment configuration requiring new resource/UID/neighbor conformance.

### Exact candidate limits

| Quantity | Value and enforcement |
| --- | --- |
| Body | 1,048,576 wire bytes before JSON decode; chunked included; compressed requests rejected |
| Code | 262,144 UTF-8 bytes including native wrapper/encoded inputs |
| Preload | Empty only |
| Stdout/stderr | 262,144 / 65,536 raw bytes; first excess byte terminates; no partial successful result |
| Serialized response | At most 2,097,152 bytes including escaping and fixed metadata |
| Wall/cleanup | 5 seconds from admitted execution preparation, then at most 1 second for cleanup; body-read stage separately bounded to 5 seconds |
| CPU | RLIMIT_CPU soft=hard=2 CPU seconds, before interpreter exec; proposed measured termination ceiling 2.25 CPU seconds, subject to supported kernel accounting resolution |
| Python/Jinja AS | RLIMIT_AS soft=hard=1,073,741,824 bytes, before interpreter exec |
| Node AS | RLIMIT_AS soft=hard=1,610,612,736 bytes, before interpreter exec |
| Tasks | RLIMIT_NPROC soft=hard=64 per leased nonroot real UID; separately verify trusted bootstrap count ≤64 before generated code |
| FDs/core/files | RLIMIT_NOFILE=64; RLIMIT_CORE=0; RLIMIT_FSIZE=1,048,576 bytes |
| Admission | One across languages, zero waiting queue; keep slot until cleanup proves complete |
| HTTP | 16 KiB headers; 2s header timeout; 5s body deadline; 15s response deadline; 32 accepted connections maximum |
| Deployment | Candidate memory request=limit `3Gi`; CPU request `1000m`, limit `2000m`; one replica; verify actual ancestor limits, scheduling/contention and stress peaks |

AS is a virtual mapping bound, not a 256 MiB RSS cap or a V8 heap setting. One maximum 1.5 GiB child leaves nominal capacity within 3 GiB for the supervisor, bounded buffers, root construction, page cache and kernel charges; this is planning arithmetic, not measured reserve. Include trusted setup operations in measured service peaks. Container OOM/restart is a recovery failure. No zero/unlimited override or automatic upward adjustment is supported. All constants, interpreter identity, syscall tables and architecture participate in configuration/evidence binding.

## Contracts and interfaces

All new Go types below are internal and immutable by convention: construct validated values, pass by value, do not publish writable global maps. Names are binding across implementation tasks. `runner/types` is imported as `runner_types` by services. Resource settings are never decoded from user JSON or appended to Node's current `RunnerOptions.Json()`.

```go
// internal/core/runner/types/restricted.go
type Language string
const Python3 Language = "python3"
const NodeJS Language = "nodejs"
type ExecutionLimits struct {
    BodyBytes, CodeBytes, StdoutBytes, StderrBytes, ResponseBytes int64
    Wall, Cleanup time.Duration
    AddressSpaceBytes, CPUSeconds, Tasks, OpenFiles, FileBytes uint64
}
func RestrictedLimits(language Language) (ExecutionLimits, error)
type ExecutionOutcome string
type StopReason string
type ProcessState string
type CleanupState string
type ExecutionResult struct {
    Stdout, Stderr []byte
    Outcome ExecutionOutcome
    ReasonCode StopReason
    ProcessState ProcessState
    ExitCode *int
    Signal *int
    EnforcementReasons []StopReason
    CleanupState CleanupState
    UserCPU, SystemCPU, Elapsed time.Duration
    MaxRSSBytes uint64
    StdoutReadBytes, StderrReadBytes uint64
}
func ValidateExecutionResult(result ExecutionResult) error
// internal/core/runner/capture_bounded.go
func CaptureBounded(ctx context.Context, cmd *exec.Cmd,
    limits runner_types.ExecutionLimits, input io.ReadCloser,
    terminate func() error) (runner_types.ExecutionResult, error)
// internal/core/runner/limits_linux.go
func LimitedCommand(language runner_types.Language, rootPath, bootstrapPath string,
    uid, gid int, limits runner_types.ExecutionLimits) (*exec.Cmd, error)
// internal/core/runner/python/python.go and nodejs/nodejs.go, respectively
func (p *PythonRunner) RunRestricted(ctx context.Context, code string,
    env runner.RestrictedEnvironment) (runner_types.ExecutionResult, error)
func (p *NodeJsRunner) RunRestricted(ctx context.Context, code string,
    env runner.RestrictedEnvironment) (runner_types.ExecutionResult, error)
```

Finalized enum values, process/null invariants, the bounded eight-reason monotonic set, outcome/code precedence and strict response schema are binding in joint contract §§2–5. Empty enums and contradictory fields refuse finalization. Capture returns intermediate cleanup unknown; only RunRestricted finalizes it through the existing root/UID owners.

Task 1 validates the pure finalized outcome/reason/process/cleanup/limit invariants. Numeric HTTP envelope codes are mapped and tested by the Task 6 service owner; ExecutionResult itself has no HTTP code field or mapper. All joint outcome rows and the fixed later HTTP code matrix remain mandatory coverage.

`LimitedCommand` selects the fixed interpreter/bootstrap argument form from language, validates paths belong to the root/immutable asset tree and verifies the language's exact limits. It does not accept client executable paths. `CaptureBounded` owns creation of code pipe FD3, stdio pipes, Start, exactly one Wait, cancellation, input writer, counters and closure. `terminate` references `cmd.Process` only after Start succeeds and kills its separately created process group; it must never emit output before killing. A start/cancel race checks cancellation both before Start and immediately after it. Return is allowed only when readers/writer have stopped and the child has been reaped or the environment is marked unhealthy. `MaxRSSBytes` normalizes Linux wait4 KiB to bytes. AS/NPROC enforcement observations come from trusted harness/launcher checks, not this result's prose.

### Assets and initialization

```go
// internal/core/runner/assets.go
type AssetManifest struct { Version int; Entries []AssetEntry }
type AssetEntry struct { Path, SHA256, Kind string; Mode uint32; Size int64 }
type AssetSet struct { PythonRoot, NodeRoot, ManifestDigest string }
func ValidateRestrictedAssets(assetDir string, manifest AssetManifest) (AssetSet, error)
// Task 4 adds a separately checked executable; Task 2 cannot validate one yet.
func ValidateRestrictedLauncher(path string, entry AssetEntry) error
// internal/core/runner/restricted_environment.go (Task 3, after RootManager exists)
type RestrictedEnvironment struct { Assets AssetSet; Roots *RootManager; UIDs *uidpool.UIDPool }
```

Build-time assembly owns user/passwd records, Python/Node trees and native libraries. Move package-init writes into explicitly invoked ordinary initialization; ordinary server/test entry points must still invoke it where previously required. Restricted start validates only: no repair/extraction, pip, dependency timer, useradd or passwd append. Dependency routes and service mutation functions deny restricted mode. Config/dependencies/manifests come from immutable image content, never ordinary writable volume mounts. Lock wheels/transitives/hashes, Node archive checksum, native dependencies and base-image digest. Actual resolved hashes are generated from authorized build inputs, not invented in this plan.

Initialization orchestration belongs in new `internal/bootstrap/assets.go::InitializeOrdinaryAssets() error`, which calls `runner.InitializeSandboxUser() error`, `python.InitializeAssets() error`, and `nodejs.InitializeAssets() error`. Server/build/test entry points import this upper-level bootstrap owner. Core runner and language packages must not import bootstrap; putting orchestration in runner would create an import cycle because the language runners already import runner. Task 2 does not define RestrictedEnvironment until Task 3's RootManager exists.

Manifest permits regular files/directories and narrowly declared in-tree relative symlinks whose resolved targets are verified; reject absolute/escaping links, device nodes and sockets. Root-own immutable directories 0555/files 0444 or 0555. Validation checks content, ownership/mode, path closure and interpreter/library compatibility. Compiled `.so` package initialization must not rewrite these files before restricted mode is selected.

`AssetSet` deliberately has no LauncherPath: Task 2 validates language asset trees only. Task 4 supplies the real launcher and validates its executable ownership/mode/hash against its separate manifest entry using `ValidateRestrictedLauncher`. Production `LimitedCommand` selects the fixed `/usr/local/libexec/sandbox-limit-launcher`; its path is not caller configuration. Task 6 startup must require both language asset validation and launcher validation before readiness. A language-only fixture, absent launcher, or partial earlier task cannot produce a ready restricted service.

### Root and UID lifetime

```go
// internal/core/runner/request_root.go
type RootManager struct { /* private validated parent/health state */ }
type RequestRoot struct { /* private path, lease and once-only lifecycle */ }
func NewRootManager(parent string) (*RootManager, error)
func (m *RootManager) Create(ctx context.Context, language runner_types.Language,
    assets AssetSet, lease *uidpool.UIDLease) (*RequestRoot, error)
func (r *RequestRoot) Path() string
func (r *RequestRoot) Close(ctx context.Context, childReaped bool) error
func (m *RootManager) Healthy() bool
// internal/core/runner/uidpool/uid_pool.go (ordinary Acquire/Release kept compatible)
type UIDLease struct { /* private uid, pool, once-only release/quarantine state */ }
func (p *UIDPool) AcquireLease(ctx context.Context) (*UIDLease, error)
func (l *UIDLease) UID() int
func (l *UIDLease) Release() error
func (l *UIDLease) Quarantine()
```

`RootManager` owns a root-only request parent and creates unguessable unique subdirectories; no user path components. Copy only validated immutable assets with fixed-buffer/context-aware copying. Hard links are allowed only for root-owned immutable regular files on the validated image tree; no link to mutable sources. No process-global chdir. Python gets a fresh root; Node passes explicit `cmd.Dir`; restricted code never uses shared Python LIB_PATH as its chroot. The request chroot and bootstrap are root-owned/read-only; no generated code/input/output is persisted to disk. Code arrives through bounded FD3. Root contains neither `/proc`, `/sys`, service config/key/logs, sockets, host mounts nor neighbor roots. No writable tmp/cache; Python bytecode writes disabled and Jinja disk caching absent.

One lease per live child; root removal must complete after group termination/reap before Release. Root creation failures clean partial paths before returning a lease. Failed removal/wait/group proof quarantines root+UID, makes readiness false and retains admission until service recovery; no optimistic lease return. Duplicate releases error, never block or replenish capacity twice. `Close` requires caller's trusted child lifecycle state, not a user flag.

Service restart cannot safely kill an arbitrary remembered numeric PID/group: PID reuse is possible. Initial deployment must have a private PID namespace with the restricted server as PID1 (exec-form entrypoint), so server death terminates remaining namespace tasks. If the deployment cannot establish that invariant, restart cleanup is unsupported and capability stays unavailable until another reviewed lifecycle owner exists. On start, remove only stale internal root names under the protected parent after establishing no prior child can be live; reject symlinks and unexpected ownership. No host PID mode, shared-PID sidecars, broad recursive cleanup or guessed process kills.

### Launcher and filter

Add a small C `sandbox-limit-launcher`: apply checked hard/soft limits before exec; keep UID0 only for trusted interpreter/bootstrap chroot and credential drop; fixed absolute interpreter arguments, no shell/search PATH. Set process group in Go `SysProcAttr.Setpgid=true`. Input FD3 is the only extra descriptor. Env is empty except fixed nonsecret deterministic runtime settings recorded in the profile; proxies, Python/Node search injection and syscall overrides absent. Server listener/key/log descriptors are close-on-exec. Limit setup/exec failure returns fixed diagnostic and a nonzero exit before generated code is read.

Both bootstrap paths install chroot, no-new-privs, checked seccomp TSYNC and group/GID/UID drop before reading/compiling/evaluating FD3. No caller preload under any alias. Check every libseccomp rule/export/load and credential transition result. Default restricted filter denies network, fork/exec, setsid/setpgid, ptrace and unneeded privilege operations; current clone-deny policy remains. Do not widen clone/clone3 merely to make a test pass. If deterministic useful work requires additional native threads, stop for a scoped review of argument-filtered same-thread-group clone; clone3 pointer arguments cannot be treated as ordinary inspectable flags. All-thread UID/capability/filter state must be observed in CI. RLIMIT_NPROC does not enforce UID0 trusted startup; bootstrap thread count and no-new-thread invariant after drop are separate gates. Host-UID collisions/range ownership remain a deployment gate.

### Output, termination and HTTP

Keep legacy OutputCaptureRunner/ordinary service behavior behind ordinary branches. Restricted capture uses two fixed-size read buffers and bounded destinations; process `n>0` before EOF, and never send reused mutable slices to another goroutine. Retain at most each stream ceiling, count consumed bytes, kill on first excess, and bound stream shutdown. No unbounded channels, io.ReadAll, strings.Builder, diagnostic logging or final serialization on this path. Cancellation covers body read, root preparation, code writer, child, readers and cleanup. Latch every observed enforcing reason atomically into the bounded monotonic set; apply joint-contract deterministic presentation precedence. Unknown cleanup prevents success while preserving every observed enforcement fact. Do not infer trustworthy stop reasons from generated stderr text.

Success requires actual normal exit 0, no observed enforcing reasons or I/O error, valid bounded untruncated streams and cleanup confirmed. Map finalized ExecutionResult into RestrictedRunResponseV1 using joint contract §§3–5, including explicit not_started/confirmed/unknown cleanup, nullable exit/signal and fixed negative codes; never return partial stdout on failure. Honest normal nonzero exit without observed enforcement is child_failed; unexplained SIGKILL or lost Wait is unknown. Preserve ordinary semantics for normal stderr/output separately; stderr text is untrusted. A sandbox result establishes an execution outcome, not trusted deployment identity or business acceptance.

Restricted controller: authenticate first, atomically try-acquire before body decode, require JSON/no compression, unique known keys, strict UTF-8, explicit bool false `enable_network`, supported language and bounded nonempty code. Preload absent/empty permitted; anything else denied. Use MaxBytesReader and body deadline for chunked/slow input. Require the unique X-Dify-Sandbox-Invocation-ID header and hash the entire bounded exact body before JSON decode. Fully associated responses echo both values. Full slot returns immediate bounded -429 without manufacturing a body digest; the parent treats unassociated early errors as unknown. No waiter queue. Body decode and execution get separate deadlines, but the slot spans both and cleanup. Stop accepting when environment unhealthy. Use ordinary bounded JSON wire serialization; no canonical full-response encoder/re-encoding equality. Strict parent decoding rejects duplicate/extra/missing/contradictory fields; only validated execution metadata is canonicalized for persistence/export. Health readiness must reflect immutable asset/lifecycle health, not merely process uptime. Bind request cancellation to `c.Request.Context()`, not only gin.Context.

## Evidence and parent Task 5 handoff

Three distinct layers are required; none can be supplied by untrusted generated code:

1. **Sandbox CI observations:** exact source revision, immutable platform-image digest, architecture, dependency/asset/interpreter/filter/resource config digests, test revision and CI run identity, per-language case outcomes with measurements and independent canary counters. A build label/digest echoed by the sandbox is not identity proof.
2. **Deployment observations:** trusted deployer resolves the actual running image/platform/config/mounts/env allowlist/resource limits/UID and PID namespace/runtime/kernel/endpoint route identity; executes deployed canaries; binds these to the CI artifact. The observed endpoint must be the one Dify will call. No real secret values are included. Source SHA tags and health GET cannot establish this identity.
3. **Dify capability/attempt evidence:** parent Task 5's `SandboxCapabilityReceipt` is minted/validated by a trusted release/deployment evidence owner only after layers 1–2 and parent Task 6 native controls pass. It binds endpoint identity, language cases, image/platform/config/dependency/network/resource digests, issuer/run evidence, issued/expiry timestamps and revocation generation. The accepted deployer Ed25519 verifier/issuer design must be implemented and proven by that owner before activation. Missing trust root or freshness/revocation validation is a blocking integration gap, not `verified=true` configuration.

Use the parent frozen receipt type as the integration owner; do not create an independent sandbox “approved” DTO or a second assertion registry. Use the accepted joint execution metadata schema/version and vectors; parent receipt/raw-measurement exporter schema agreement remains separately required before exporter implementation. Export raw structured measurements plus content digests from trusted CI, not a boolean authorization. Parent prepare, worker and actual invocation each reject absent/stale/revoked/mismatched/language-ineligible receipts before RPC. Endpoint selection is per adapter, never global mutation; authenticated endpoint route/transport identity must be bound by deployment evidence, not DNS name text alone. If an authenticated deployment identity mechanism is absent, leave capability unavailable.

Parent per-invocation recorder writes `sandbox_started` before RPC and completed/failed after actual outcome, bound to authoritative context/native run/task/node/implementation/invocation/profile plus nonsecret input/code digests. Each start/admission/binding carries the exact request-body digest and fresh UUIDv4; terminals carry the bounded typed execution snapshot, independent enforcement set and complete/blocked/unknown evidence state from joint contract §6. SameRecorder and Task 4 retain seal/native-attempt ownership. Recorder failure prevents RPC; poison/incomplete start-finish evidence prevents a positive seal. Those observations do not replace deployment conformance. Native Start→Code(41+1)→End, JS equivalent, Jinja greeting, invalid output schema and persisted Run/node ownership are parent Tasks 5–6 gates. All skipped/failed/unknown language cases stay unavailable.

## Acceptance and unsupported assumptions

### Early CI availability and fixture boundary

Task 1 creates `.github/workflows/restricted-conformance.yml` plus `build/restricted-contract-tests.sh`, an explicit stage/test inventory and a narrow Linux contract-test image target derived from the existing test build owner. Its initial scope is compile/unit/contract execution, not publication or exact production-image conformance. It runs on an authorized push to `codex/eng-1189-*`, with `permissions: contents: read` and checkout `persist-credentials: false`. This is a push trigger evaluated from the feature branch, not a dispatch requiring an unavailable default-branch workflow. Actions availability, branch workflow protections and permitted test-base pulls remain explicit root/CI-owner gates. No secrets, registry login, push, deployment or default-branch workflow mutation is required by this bootstrap. Task 7 extends the same workflow with full exact-image/runtime conformance.

Each subsequent task advances a checked stage number and adds the named expected tests. Missing source/package/test, zero executed matching tests, failed tests or unavailable required Linux architecture is not GREEN; the harness checks actual `go test -json` test outcomes against that stage's inventory. Existing .so libraries are compiled before contract packages are built. Root-requiring fixture tests run only in the disposable CI test container, never on the developer host. Contract-image dependency resolution is a test prerequisite; unresolved access stops the gate rather than borrowing production credentials. A contract test image is not the restricted release artifact and does not authorize deployment.

Task 4 direct native bootstrap fixtures use Tasks 2–3 asset/root/UID contracts and Task 4 `LimitedCommand`, install FD3 with a bounded known payload, and own bounded output, deadline, wait/group cleanup inside the test harness. They exercise the actual launcher/prescript/filter and finite startup limits without depending on Task 5's not-yet-existing `CaptureBounded` or `RunRestricted`. They are explicitly bootstrap contract fixtures, not native leased-runner/HTTP, Dify/Graphon, or deployment conformance. Task 5 adds composed RunRestricted lifecycle tests; Task 6 adds real HTTP/service wiring; Task 7 supplies full exact-image measurement and recovery. No earlier GREEN can be promoted across those boundaries.

Required measured cases: exact/+1 input/output limits and slow/chunked requests; CPU/time/allocation/AS/task/FD limits; exit/start/cancel/pipe-retention failures; independent positive then zero restricted TCP/UDP/DNS/HTTP/HTTPS/proxy/redirect/IPv4/IPv6/Unix-socket canaries; host/service/neighbor/previous-request read/write/env/descriptor/signal probes; manifest/config unchanged; 100 mixed serial/parallel-attempt cycles with max-one child, no leaked leases/roots/tasks/FDs and healthy positives after each failure. Verify parent/container never restarts or OOMs. Include fake printed success before fatal prohibited external/process operations, attempted SIGSYS handler, SIGXCPU/SIGXFSZ, unexplained SIGKILL/lost Wait, monotonic multi-cause/cleanup failures, semantic wire variants and parent retry/default retaining blockers. Permitted handled local ENOMEM/EMFILE/ENOENT controls may succeed with normal exit0, valid output, no observed enforcement and confirmed cleanup; no universal local-error detector or gate exists. Compiled seccomp behavior, namespace/resource controls and real native integrations are CI-only tests; a macOS unit run cannot stand in for them.

Root's later repository permission inspection establishes pull/push=true and admin/maintain=false for `langgenius/dify-sandbox`, default branch main. This permits a branch push subject to actual repository protections; it does not prove merge rights, build-branch publication, registry access or deployment/conformance authority. Feature pushes do not currently publish official artifacts through the observed main/release workflow. Existing main/release workflows publish Docker Hub/ECR architecture/manifests using configured credentials. The release owner must verify an authorized branch publication route, registry/CI runners and pinned build inputs before release. Actual Dev resource/PID/UID support for the new dedicated deployment remains a gate; the existing EE deployment's unconfigured resources prove nothing about this candidate. Dify's AgentRuntime workflow is unrelated.

Root's metadata-only inspection confirms sandbox secret names `DOCKERHUB_USER`, `DOCKERHUB_TOKEN` and AWS key/secret/region entries, plus variable names `AWS_ECR_REGISTRY`/`AWS_ECR_REPOSITORY`. No values were read/disclosed. Name presence is not credential validity, permission to consume them from a feature branch, a usable branch-publication route or artifact proof. The early contents-read-only contract workflow continues to use no secrets or registry publication.

Cgroups are **not** initial implementation scope. If full useful controls or finite-limit recovery fail and cannot fit the measured deployment envelope, return a concrete failed test/measurement to root. Only then consider a separately reviewed per-request delegated cgroup owner. Never add a Docker socket, privileged pod, broad host cgroup mount or cluster-admin permission implicitly. Until all acceptance and parent handoff gates pass, ENG-1189/ENG-1183 remain open and Code/template unavailable.

## Early CI base-access decision

The dedicated contract fixture uses the existing official test-template public Docker Hub base, pinned to the anonymous-read observed multi-platform manifest `docker.io/langgenius/python:3-debian13-sfw-ent-dev@sha256:e358e4a9c08c1c691bf7c1698c36ae1daee759a55f7f9dd5139136e38e39ae0a`. Root observed anonymous Docker Hub manifest 200 for amd64/arm64 and DHI 401. Only the narrow contract generator target overrides the private generator default; ordinary/production images stay unchanged. This establishes read access, not a successful build or production-image equivalence; the later actual restricted image still requires locked accessible inputs and its own complete conformance. Root supplied this historical anonymous-read observation; it is not build or production evidence.

## Parent-owned receipt trust integration decision

The accepted parent-owned receipt trust boundary is summarized here: dedicated deployer Ed25519 receipt; fresh in-memory restricted-server TLS key on every boot; immutable runtime/config/dependencies; actual authenticated Pod/certificate/conformance observations; existing-SQL current-generation and short transactional attempt admission plus revoke/drain. Sandbox server startup needs restricted-only TLS/Service SAN support with no key persistence/shared key/terminator, and exporter must agree parent canonical measurement schema. Ordinary startup stays unchanged. Dify uses a structural guarded HTTPcore network stream through HTTPX, never observer authorization. CI-only candidate/native test binding precedes signed approval; final unmodified production path remains mandatory. Generation migration is schema-only and operator CLI initializes disabled scope with CAS. The eight-hour receipt/twenty-four-hour boot-certificate candidate remains unfrozen pending measured renewal/E2E/drain availability; actual issuer/bootstrap/publication credentials, exact artifact and deployment conformance remain gates. This handoff is design acceptance, not activation or source/runtime implementation.

## Accepted local/Dev trust handoff amendment (design only)

The parent receipt design names strict Docker/Kubernetes deployment variants and exactly two authenticated operator adapters, separately scoped actual local and Dev receipts/conformance/native E2E. Root accepts this additional design scope: sandbox cmd/public-identity/main.go fixed-loopback public certificate helper, measured build/assets manifest and scoped tests (no authority assertions/key reads), and parent operator-only docker_deployment.py/kubernetes_deployment.py plus protected local operator_key.py. No fabricated Kubernetes IDs for Docker; actual OCI platform manifest vs image-config digest distinction mandatory. Use authenticated local Unix-socket daemon/full-container-ID exec plus exact boot pin, protected inputs and actual process/runtime/resource/network facts; Dify receives no Docker/Kubernetes credentials. Helper must exit before execution budget cases. Real local persisted production E2E remains independent of Dev and test-only preactivation harness.

Dedicated operator key route is design-accepted only:0700 owned no-symlink directory,0600 regular owned no-symlink key, no permissive ACL/path traversal/check-open race, no repo/runtime/synced path/private material in argv/env/logs. No key provision yet. Initial8hour receipt/24hour boot cert candidate requires measured conformance/renewal/native-E2E/drain budget and reviewed availability before activation/freeze; it is not an indefinite availability promise. No automatic scheduler/renewal daemon created. Full report binding and actual release gates remain required.
