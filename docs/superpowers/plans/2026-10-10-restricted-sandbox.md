# ENG-1189 Restricted CodeSandbox Implementation Plan

Status: **root-accepted DESIGN ONLY**. This reconciliation executes no implementation, tests or CI; native parent CI remains pending.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Root has selected sequential reviewed task execution; this planning assignment starts no implementation or helpers.

**Goal:** Add a separately deployed bounded restricted mode to the existing CodeSandbox, with actual evidence suitable for parent ENG-1183 Tasks 5–6.

**Architecture:** Keep the existing HTTP/service/native language runners. Isolate each request with immutable fresh roots and a leased UID; enforce finite per-language limits before interpreter exec and bounded I/O/cancellation through one lifecycle owner. Preserve ordinary mode and gate activation on trusted CI plus actual deployment evidence.

**Tech Stack:** Pinned Go/Linux dify-sandbox, existing libseccomp/c-shared bootstrap, small checked C exec launcher, Linux rlimits, existing Docker build/CI owners, parent Dify Python/Graphon adapter owner.

**Spec:** [restricted sandbox design](../specs/2026-10-10-restricted-sandbox-design.md). Read both files and [the joint execution contract](../specs/joint-execution-contract.md) before execution; startup observations are summarized in the spec and remain candidate evidence.

## Global Constraints

- Base sandbox commit: `5631afef06ec88f80c28129aec7fd22a30006b14`; implementation uses a root-authorized isolated branch/worktree, never blindly mutates detached pinned checkout.
- Default ordinary mode and existing endpoint behavior remain; restricted endpoint is separate. No frontend work; ENG-1177 search remains paused.
- Preserve actual Dev `default/dify-sandbox` running `docker.io/langgenius/dify-ee-sandbox:3.13.0-rc1` (no configured resources). New restricted OSS artifact uses separate `dify-sandbox-restricted` Deployment/Service/endpoint. Local OSS proof never applies to EE.
- Candidate AS: Python/Jinja 1,073,741,824 bytes; Node 1,610,612,736 bytes. One admitted request, zero queue, one replica; memory request=limit `3Gi`, CPU request `1000m`, limit `2000m`. These are candidates until full conformance.
- Actual Dev has five arm64 nodes, Linux `5.15.0-1121-azure`, containerd `1.7.34-2`, CPU allocatable `1900m` each; a 2-CPU request is unschedulable. Do not claim two reserved CPUs. Test under actual scheduling/contention and resource envelope.
- Body/code/stdout/stderr/response limits: 1,048,576 / 262,144 / 262,144 / 65,536 / 2,097,152 bytes. Wall 5s, cleanup 1s, CPU 2s, tasks 64, FDs 64, core 0, file size 1,048,576 bytes.
- No writable generated filesystem directory, caller preload, syscall override, proxy/inherited service env, generated subprocesses, mutable dependency route/timer or global cwd mutation on restricted path.
- Linux real isolation/resource/native integrations are CI-only. No local root tests that run package initialization, useradd/chroot or service mutations. Pure contract tests may run locally only after imports are verified side-effect-free.
- Root owns review/commit/push/build/deploy authorization. Future commands below describe authorized implementation/CI actions; none were executed to write this plan. Every task stops at its review boundary before the next task.
- Receipt labels/digests/client booleans are not authority. Missing publication rights, platform controls or receipt trust root prevents activation; no privilege expansion workaround.

## Review Focus

1. Cancel occurs between child Start and timer registration: Task 5 must kill/reap and retain admission until cleanup, with no escaped child.
2. Malicious stdout reproduces a successful result/receipt string while limit enforcement failed: Tasks 5–6 must return nonzero envelope, and Task 8 must reject it as authority.
3. Service restart reuses an old numeric PID or root symlink: Tasks 3/7 must require private PID namespace/PID1 and reject unsafe stale paths, never kill guessed host PIDs.
4. Node starts under the candidate AS bound but later native wrapper/Jinja/import/allocation changes exceed it: Tasks 4/7 must run useful controls plus exhaustion/recovery without raising limits automatically.
5. Ordinary process startup loses previous init side effects during restricted asset refactoring: Task 2 tests ordinary startup initialization explicitly; Tasks 6–7 preserve ordinary endpoint/output/network behavior.

---

## File ownership map

All paths below are relative to the sandbox repository unless explicitly labeled Dify handoff. New filenames are planned additions, not existing capabilities.

| Unit | Files |
| --- | --- |
| Profile/types | `internal/core/runner/types/restricted.go`, `internal/types/config.go`, `internal/static/config.go`, `conf/restricted.yaml` |
| Immutable assets | `internal/bootstrap/assets.go`; `internal/core/runner/assets.go`, `init.go`; Python/Node `setup.go`; Python `env.go`, `env.sh`; `cmd/dependencies/init.go`; `dependencies/python-restricted.lock`; `docker/restricted-inputs.lock.json` |
| Root/UID | `internal/core/runner/request_root.go`, `restricted_environment.go`, `temp_dir.go`, `uidpool/uid_pool.go`; Python/Node `python.go`/`nodejs.go` |
| Launcher/filter | `cmd/runner-launcher/main.c`, `internal/core/runner/limits_linux.go`; `internal/core/lib/seccomp.go`, `set_no_new_privs.go`, `{python,nodejs}/add_seccomp.go`; language prescripts; `build/build_{amd64,arm64}.sh` |
| Bounded capture | `internal/core/runner/capture_bounded.go`, existing `output_capture.go` only for isolated shared helper extraction if needed |
| HTTP wiring | `internal/controller/{base,run,router}.go`, `internal/middleware/cocrrent.go`, `internal/service/{python,nodejs,check}.go`, `internal/server/server.go` |
| Early CI bootstrap (Task 1) | `.github/workflows/restricted-conformance.yml`, `build/restricted-contract-tests.sh`, `build/restricted-contract-tests.json`, `docker/templates/restricted-contract.dockerfile`, `docker/generate.sh` |
| Full CI/deploy/export (Tasks 7–8) | `tests/restricted_integration/`, `tests/restricted_contract/`, `docker/templates/restricted.dockerfile`, `docker/restricted-compose.yaml`, `deploy/restricted-deployment.yaml`, existing Task 1 workflow, `build/restricted-conformance.sh`, `cmd/restricted-evidence/main.go`, `internal/types/restricted_evidence.go`, `docs/restricted-release.md` |

Use focused new files rather than rewriting ordinary runner APIs. Existing `Run` signatures and `RunnerOptions.Json()` stay compatible; new `RunRestricted` and typed limits carry restriction. Exact structs/signatures are in the spec. Any signature change during review must update both documents and downstream call sites before the next task.

## Shared-interface and sequencing preflight

Root and each task reviewer check this table before the consuming task begins. A row is an interface dependency, not a passing result. Changes to one side require updating both owners and the stage inventory.

| Task / shared owner pair | Required producer → consumer contract | Consistency/availability gate |
| --- | --- | --- |
| 1: types ↔ config/controller | `RestrictedLimits(Language)` returns by-value exact constants; unknown languages/config fail; resources are never user JSON | Type test exists and runs; ordinary defaults retained; later controller selects the same enum |
| 1: workflow ↔ all tasks 2–8 | Feature-push Linux build/test harness, explicit stage and expected named tests; native .so compile precedes test packages | Harness exists before Task 2; no unavailable default-branch dispatch, zero-test GREEN or publish credentials |
| 2: asset validator ↔ build/ordinary initialization | `AssetSet{PythonRoot,NodeRoot,ManifestDigest}` plus immutable manifest; explicit ordinary init preserves prior startup | Language assets only at this stage; no LauncherPath field or fictional executable; contract fixture is not release evidence |
| 2: bootstrap ↔ runner/language packages | `bootstrap.InitializeOrdinaryAssets` calls runner user setup and each language's InitializeAssets; server/build/tests import bootstrap | No runner→language import cycle; lower packages never import bootstrap |
| 2→3: AssetSet ↔ RootManager | Create takes validated assets, Language and live UIDLease; copies/links only manifest entries | No path/ownership/symlink drift or process-global cwd dependence |
| 3: RootManager ↔ RestrictedEnvironment | Define RestrictedEnvironment only after RootManager/UIDPool are available | Task 2 must compile without a forward reference to not-yet-created RootManager |
| 3: RootManager ↔ UIDPool | Close requires trusted child-reaped proof; root removed before once-only Release; failure quarantines | No duplicate release, speculative reuse or stale numeric PID kill |
| 3→4: fresh root/UID ↔ LimitedCommand | Explicit root/bootstrap/UID/GID + exact language limits; fixed executable selection | Bootstrap fixture owns its own bounded FD3/output/wait/cleanup; does not call Task 5 APIs |
| 4: build/asset manifest ↔ launcher/readiness | Real fixed-path launcher plus `ValidateRestrictedLauncher(path, AssetEntry)`; hash/mode/owner checked | Task 2 validation alone cannot make service ready; Task 6 requires both validators |
| 4: launcher ↔ prescript/seccomp | Limits before exec; checked chroot/filter/UID transitions before FD3; allowed descriptors/env fixed | All-thread/native startup fixture GREEN is limited to bootstrap contracts, not runner/HTTP/deployment |
| 4→5: LimitedCommand ↔ CaptureBounded | Unstarted exec.Cmd with explicit dir/group; Capture owns FD3, Start, one Wait, streams/cancel | No double Start/Wait or hidden precreated pipes; terminate references same cmd after successful Start |
| 3/5: cleanup ↔ RunRestricted | RunRestricted owns lease/root; Capture never claims root cleanup; root Close sets final cleanup eligibility | CleanupState is confirmed/not_started only after truthful lifecycle/removal/release checks; unknown quarantines and makes env unhealthy |
| 5→6: RunRestricted ↔ service/admission | Both concrete runners return finalized ExecutionResult; service maps exact RestrictedRunResponseV1 codes/fields/header/body digest; slot spans cleanup | No response completion or cancellation prematurely refills slot; no ordinary behavior changes |
| 1/2/4/6: profile/assets/launcher ↔ server readiness | Strict config + both asset validators + healthy roots + finite HTTP settings | Partial tasks cannot expose ready restricted execution; dependency routes/timer absent |
| 6→7: service/image ↔ conformance | Same built restricted HTTP owner; actual platform digest and runtime constraints measured | Stage-1 contract image/fixtures never substitute for exact restricted image; preserve EE deployment |
| 7→8: measurements ↔ exporter | Required case IDs, raw measurements/hashes and actual platform/run identity | Failed/skipped/missing outcomes remain explicit; no boolean or sandbox self-report as authority |
| 8 ↔ Dify parent Tasks 5–6 / deployer | Agreed canonical schema, trusted receipt issuer/validator, exact endpoint/current deployment, expiry/revocation; native persisted controls | Missing issuer, registry path, Helm integration or platform support blocks activation; no copied proof |

Task 7's full integration harness extends the Task 1 workflow/script ownership; it is not the first source of Linux CI. The stage progression and all producer contracts must be present in the reviewed branch before a task claims GREEN.

## Task 1: Strict server-owned profile and limit types

**Files:** Create `internal/core/runner/types/restricted.go`, `restricted_test.go`; modify `internal/types/config.go`, `internal/static/config.go`; create `conf/restricted.yaml`, `internal/static/restricted_config_test.go`. Also create the early CI bootstrap files in the map and add the narrow `restricted-contract` generator target in `docker/generate.sh`.

**Consumes:** Validated literal language values `python3`/`nodejs`; no client budget fields. **Produces:** Spec `Language`, `ExecutionLimits`, `ExecutionOutcome`, `StopReason`, `ProcessState`, `CleanupState`, `ExecutionResult`, `ValidateExecutionResult`, `RestrictedLimits(Language)`; config mode default ordinary and `ValidateRestrictedConfiguration(config) error` in `internal/static`. One server-owned `Mode string` accepts `ordinary`/`restricted`; absent or empty defaults ordinary. After valid configuration, derive internal `RestrictedMode bool` with `yaml:"-"`; it is not a second configurable authority. Preserve ordinary parsing/defaults, while rejecting restricted unknown/invalid/spoofed settings.

- [ ] Add pure result-contract tests from joint contract §§2–5: `TestRestrictedResultStates`, `TestRestrictedResultRejectsUnsetAndContradictoryFields`, `TestRestrictedReasonSetBounds`, `TestRestrictedOutcomePrecedence`. Assert nullable exit/signal, all finalized outcome/reason/process/cleanup rows, sorted unique maximum-eight observed reasons, truthful cleanup and rejection of empty/unknown enum values; register these in stage 1. Numeric HTTP envelope-code mapping remains Task 6 service coverage, since the frozen Go ExecutionResult has no HTTP code field. Define no unused code mapper or universal local-error coverage field/gate.

- [ ] Before any later Linux GREEN gate, add the feature-branch workflow bootstrap, initially stage 1. Ground it in existing Actions checkout/setup/build machinery, but give it no publication or deployment steps:
```yaml
name: Restricted Sandbox Conformance
on:
  push:
    branches: ['codex/eng-1189-*']
permissions:
  contents: read
jobs:
  contracts:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          persist-credentials: false
      - name: Build and run Linux contract stage
        env:
          CONTRACT_STAGE: '1'
          CONTRACT_ARCH: amd64
        run: bash build/restricted-contract-tests.sh
```
Use reviewed pinned action revisions when implementing. Root may choose another authorized branch prefix but must change the trigger and actual branch together. This is a push-triggered feature-branch workflow, not workflow_dispatch. Root verifies Actions/branch policy permits it; if unavailable the exact CI-owner gate remains blocked. No secrets, registry login, image push or writes beyond disposable CI build/test outputs.
- [ ] The early contract-test image explicitly uses the existing test-template Docker Hub base `docker.io/langgenius/python:3-debian13-sfw-ent-dev@sha256:e358e4a9c08c1c691bf7c1698c36ae1daee759a55f7f9dd5139136e38e39ae0a` (observed amd64/arm64 index) rather than the generator's private DHI default. Root's anonymous-only manifest read returned Docker Hub 200 and DHI 401; the historical observation is summarized in the design’s Early CI base-access decision. Keep this override narrow to the contract image/target; do not alter ordinary or production base selection. The public mirror's equivalence to the DHI production artifact is unproven, so early GREEN is contract-fixture evidence only. Full exact-image conformance must later bind its own reviewed, accessible, immutable build inputs.
- [ ] The script validates stage 1..8, uses the existing native build scripts to compile .so assets, builds `restricted-contract.dockerfile` through the generator, and runs only that stage's explicit packages/tests in a disposable Linux container. Derive the test image from the existing test-template build owner, replacing its eager full-integration RUN with the stage command. It is an ordinary test fixture with no release identity. Run user/root/chroot fixtures only inside this container. Record toolchain/base/architecture; unavailable read access to the test base is a gate, never a reason to request publication credentials.
- [ ] Define `build/restricted-contract-tests.json` as a stage-indexed package/test inventory. Stage 1 requires named type/config tests; each task appends its exact expected test names and bumps CONTRACT_STAGE in the same reviewed change. Run `go test -json` and require a pass outcome for every expected top-level test. Missing packages/tests, zero tests, build failures and skipped required cases fail the job. Include a self-test removing one inventory test name from captured results and assert verifier nonzero; this prevents a nominal successful command from masking unavailable coverage. Stages 2–6 progressively add only the task tests described below; Task 7 adds full exact-image cases separately, and Task 8 adds exporter contracts.

- [ ] Add this pure RED limit test and strict invalid-language/config tests (unknown mode, invalid numeric value, enabled network/preload, any syscall override, proxy values, writable dependency config):
```go
func TestRestrictedLimitsPerLanguage(t *testing.T) {
    py, err := RestrictedLimits(Python3); if err != nil { t.Fatal(err) }
    js, err := RestrictedLimits(NodeJS); if err != nil { t.Fatal(err) }
    if py.AddressSpaceBytes != 1073741824 || js.AddressSpaceBytes != 1610612736 {
        t.Fatalf("wrong finite AS limits: %d %d", py.AddressSpaceBytes, js.AddressSpaceBytes)
    }
    if py.StdoutBytes != 262144 || js.StderrBytes != 65536 { t.Fatal("wrong output limits") }
    if _, err := RestrictedLimits(Language("unknown")); err == nil { t.Fatal("accepted unknown language") }
}
```
- [ ] RED command: `go test ./internal/core/runner/types -run TestRestrictedLimitsPerLanguage -count=1`; expected missing types/function, not unrelated environment failure.
- [ ] Implement by-value literals and exact config validation; keep resources internal:
```go
switch language {
case Python3: limits.AddressSpaceBytes = 1073741824
case NodeJS: limits.AddressSpaceBytes = 1610612736
default: return ExecutionLimits{}, errors.New("unsupported language")
}
return limits, nil
```
Set all shared constants from the spec before this switch. Reject invalid restricted configuration with fixed nonsecret error codes. Do not silently blank unsafe config values or accept unknown settings.
- [ ] GREEN: rerun type tests; Linux CI runs config tests with ordinary env/default preservation and restricted override rejection. Review must establish ordinary default is unchanged and no profile value authorizes a receipt. Root reviews this unit; capability remains unavailable.

## Task 2: Build-only immutable assets and explicit initialization

**Files:** Asset ownership files in map; create `internal/core/runner/assets_test.go`, `tests/restricted_contract/initialization_test.go`; modify ordinary `internal/server/server.go` startup and test initialization entry point `tests/integration_tests/init.go`; add restricted Dockerfile/input lock assembly support to `docker/generate.sh`.

**Consumes:** Task 1 mode/config and already operational Linux contract CI. **Produces:** Spec `AssetManifest`, `AssetEntry`, `AssetSet`, `ValidateRestrictedAssets`; `bootstrap.InitializeOrdinaryAssets() error`, `runner.InitializeSandboxUser() error`, `python.InitializeAssets() error`, `nodejs.InitializeAssets() error`. Orchestration lives in `internal/bootstrap/assets.go`; only server/build/test entry points import bootstrap, avoiding runner↔language cycles. RestrictedEnvironment is deferred to Task 3 when RootManager exists. Build-time output contains normalized language roots plus their manifest. AssetSet has no LauncherPath. The real launcher and separate manifest entry/validator first arrive in Task 4; no placeholder executable or readiness claim is allowed now.

- [ ] RED tests mutate a manifest file byte/mode, introduce escaping relative/absolute symlinks and a socket/device entry, then assert validation fails. Ordinary-start spy must observe its initializer exactly once; restricted-start spy must observe no write/install/refresh/useradd. A filesystem snapshot before/after restricted validation must be identical.
```go
if _, err := ValidateRestrictedAssets(assetDir, manifest); err != nil { t.Fatal(err) }
if err := os.WriteFile(filepath.Join(assetDir, "lib.py"), []byte("changed"), 0444); err != nil { t.Fatal(err) }
if _, err := ValidateRestrictedAssets(assetDir, manifest); err == nil { t.Fatal("accepted asset drift") }
```
The test fixture creates a temporary root-owned image-like tree inside the CI container; manifest and expected digest come from its actual bytes, never a hard-coded fictional image digest.
- [ ] RED in Linux build/test CI: `go test ./internal/core/runner ./tests/restricted_contract -run 'Test(RestrictedAssets|Initialization)' -count=1`; expected missing validator/forbidden mutations. Build native libraries through existing build scripts first; do not misreport missing embedded .so as the intended RED.
- [ ] Implement explicit initialization call placement; package import performs no runtime tree extraction/user changes. Ordinary start/build/test paths explicitly perform their former setup. Restricted start only validates roots and fixed passwd/group entries. Assembly uses manifest-bounded entries, rejects links escaping the image tree, normalizes root ownership/modes and hash-checks files. No runtime shared tree repair:
```go
if config.RestrictedMode {
    assets, err = ValidateRestrictedAssets(assetDir, manifest)
    if err != nil { return err }
} else if err := bootstrap.InitializeOrdinaryAssets(); err != nil { return err }
```
`RestrictedMode bool` is derived internally from Task 1's validated `Mode string` and excluded from YAML; assetDir/manifest are trusted image paths. Lock build inputs only from resolved authorized sources/checksums; unresolved base/wheel/archive inputs block image production, not pure code work.
- [ ] Bump contract stage to 2 and register each new named test, then obtain GREEN exact targeted tests and existing ordinary initialization checks in the Task 1 CI harness. Review initialization order, asset syscall/library compatibility and image path closure. Contract fixtures do not authorize a restricted image; parent capability stays disabled.

## Task 3: Fresh roots, leased UIDs and cleanup health

**Files:** `request_root.go`, `request_root_test.go`, `uidpool/uid_pool.go`, `uid_pool_test.go`, `temp_dir.go`; create `restricted_environment.go` now that RootManager exists.

**Consumes:** `AssetSet`, `Language`, `UIDPool`. **Produces:** Spec `RootManager`, `RequestRoot`, `UIDLease` methods, `NewRootManager` and `RestrictedEnvironment`. Existing ordinary UID acquire/release remain compatible. RootManager implements its own private unhealthy latch.

- [ ] RED tests create sequential Python/Node roots and assert distinct paths/inodes for private bootstrap, approved immutable assets only, no writable directory, unchanged process cwd; canceled copy must remove partial root and release once. Inject remove failure: root/lease quarantine and unhealthy manager. Duplicate Release must error immediately. Reject a symlinked stale-root entry and stale-root cleanup without restart-lifecycle assurance.
```go
lease, err := pool.AcquireLease(ctx); if err != nil { t.Fatal(err) }
r, err := manager.Create(ctx, runner_types.Python3, assets, lease); if err != nil { t.Fatal(err) }
if err := r.Close(ctx, false); err == nil { t.Fatal("released live child root") }
if manager.Healthy() { t.Fatal("unproven cleanup remained healthy") }
```
Use separate fixtures for normal close because the failed proof intentionally quarantines this lease.
- [ ] RED/GREEN command in Linux CI: `go test -race ./internal/core/runner ./internal/core/runner/uidpool -run 'Test(RequestRoot|UIDLease)' -count=1`.
- [ ] Implement root parent validation, random server-generated path, context-aware fixed-buffer copy/link of immutable manifest assets, root-only bootstrap and once-only cleanup. No `os.Chdir`. Delete only under the validated parent, reject unexpected symlink/ownership rather than follow it. Cleanup sequence is binding:
```text
prove child/group gone -> remove owned root -> lease.Release -> admission release
any proof/removal error -> lease.Quarantine + unhealthy -> no positive result
```
Refactor the old TempDir helper's caller cwd reliance with explicit command directories, including build dependency commands; retain ordinary functionality under existing tests. Do not implement restart by killing persisted numeric PIDs.
- [ ] Bump contract stage to 3 and register named tests; obtain GREEN tests plus ordinary Node/dependency cwd regression in the existing CI harness. Review can independently reject cleanup/reuse behavior even if asset task passed. No generated code has run in these unit contracts.

## Task 4: Checked pre-exec resource launcher and filter boundary

**Files:** Launcher/filter/build files in map; create `tests/restricted_integration/launcher_test.go`, `bootstrap_test.go`, `bootstrap_fixture_test.go`; add `ValidateRestrictedLauncher` to the asset validation owner; update Python/Node prescripts with trusted restricted branch/options separate from user JSON. Extend existing stage inventory/workflow to 4.

**Consumes:** Task 1 limits/CI, Task 2 language assets, Task 3 root/UID, fixed image interpreter/library paths. **Produces:** Spec `LimitedCommand`, `ValidateRestrictedLauncher(path string, entry AssetEntry) error` and actual build artifact `/usr/local/libexec/sandbox-limit-launcher`; bootstrap preserves FD3 unread until all controls installed. Launcher validator checks its own real manifest entry, executable owner/mode/hash; Task 6 startup composes it with language validation.

**Early harness boundary:** `bootstrap_fixture_test.go` defines test-only `runBootstrapFixture(t *testing.T, language runner_types.Language, code []byte)` using RootManager/UIDLease and LimitedCommand directly. Code is a bounded known fixture; it installs FD3, reads at most fixture output ceilings, uses a deadline/group kill, calls one Wait and closes the root after reap. It does not call CaptureBounded or RunRestricted, which do not exist until Task 5. This harness tests the real launcher/prescript/filter under direct controls; it is not a competing production executor or full native runner/HTTP/Dify conformance. A test-only command-builder injection may select the trusted probe executable; production LimitedCommand remains fixed-interpreter only.

- [ ] RED CI fixture invokes the planned launcher with a trusted probe executable that reports `/proc/self/limits`, descriptors and environment to the harness. Assert each exact per-language soft/hard limit, own process group and absence of service canary env/FD. Force one setrlimit error and exec failure; code-entry marker must be absent. Native tests inject filter/no-new-privs/credential failures and assert user FD3 marker never runs.
```c
static int set_pair(int resource, rlim_t value) {
    struct rlimit lim = { .rlim_cur = value, .rlim_max = value };
    return setrlimit(resource, &lim);
}
/* Each failed call writes a fixed message and _exit(125); no exec fallback. */
```
- [ ] RED command in Linux CI after native library build: `go test ./tests/restricted_integration -run 'Test(Launcher|Bootstrap)' -count=1`; expected absent launcher/enforcement. The harness passes a compile-time test probe executable only in test-image launcher tests; production command construction accepts fixed Python/Node interpreters only.
- [ ] Implement launcher argument grammar: fixed language, AS/CPU/NPROC/NOFILE/FSIZE decimal limits from trusted Go caller, `--`, absolute interpreter and its fixed bootstrap args. Reject overflow/negative/missing/extra options; revalidate language/limits before spawning in Go. The C helper sets limits, closes all non-stdio/non-FD3 handles, then execve with fixed sanitized env. No shell. Go sets `Setpgid=true`, explicit `cmd.Dir` and absolute paths. Use checked libseccomp/no-new-privs/credential transitions and no caller preload; retain clone-deny initially.
- [ ] Stage 4 CI GREEN uses the direct bootstrap fixture for Python/Node filter controls under candidate AS and bounded known wrapper/import/Jinja payloads. Inspect all task UID/capabilities/seccomp; assert max bootstrap tasks ≤64 and no generated process creation. Test AS and CPU exhaustion; record rusage. Label results bootstrap contracts, never RunRestricted/HTTP/Dify execution or service recovery. If useful payloads fail, return actual failure to root; no auto-limit increase or cgroup implementation. Review compiled filter errors, task credentials and no-FD inheritance before next task.

## Task 5: Bounded capture and cancellation as one lifecycle

**Files:** `capture_bounded.go`, `capture_bounded_test.go`; language `python.go`/`nodejs.go` adds `RunRestricted`; optional small reusable helpers in existing `output_capture.go` only with ordinary regression coverage.

**Consumes:** Tasks 1–4 `LimitedCommand`, environment, root/lease. **Produces:** Spec `CaptureBounded`, `RunRestricted`, `ExecutionResult`. Existing `Run` remains ordinary. Termination closure sends group SIGKILL once; Root.Close follows trusted Wait/group proof.

- [ ] RED tests use trusted test subprocesses to emit exactly N/N+1 bytes, simultaneous stream floods, `(n>0, EOF)`, retained pipe descendants, input-writer blockage, failing Start and cancel barriers immediately before/after Start. Capture must never exceed raw caps or leave goroutines/child/pipe alive. Inject wait/kill/root-remove failure and require unavailable/unhealthy rather than success. A child printing fake JSON `{"verified":true}` must remain ordinary untrusted bytes.
```go
result, err := CaptureBounded(ctx, cmd, limits, input, terminate)
if int64(len(result.Stdout)) > limits.StdoutBytes { t.Fatal("retained excess stdout") }
if result.ReasonCode != runner_types.StopReason("stdout_limit") { t.Fatalf("reason: %s", result.ReasonCode) }
if result.CleanupState != runner_types.CleanupState("unknown") { t.Fatal("capture alone cannot finalize cleanup") }
_ = err // outcome-specific test separately asserts error category
```
Test helpers create pipes/commands under the runner test harness; never rely on a production untrusted child successfully forking through seccomp to test orphan cleanup.
- [ ] RED/GREEN command in Linux CI: `go test -race ./internal/core/runner -run TestCaptureBounded -count=1`. Repeat only race/failure cases that exercise new changes.
- [ ] Implement two fixed read buffers with bounded writes, n-before-err processing and atomic monotonic bounded enforcement-reason set. Capture owns `cmd.Start`/single `cmd.Wait`, supplies code on FD3, closes parent's unnecessary pipe ends and selects on cancellation for input feeding. Precheck context; after Start recheck before allowing steady state. Kill before diagnostics. On termination close readers/writer and join within cleanup deadline. Wait result populates CPU/signal/exit/RSS fields; no generated text determines reason.
```text
RunRestricted: lease -> root -> command -> CaptureBounded
defer cleanup ownership from first acquired resource
Capture complete -> group gone proof -> root.Close(childReaped=true)
only then result.CleanupState="confirmed"; proven never-started + reclaimed preparation => "not_started"
any lifecycle/group/root/lease uncertainty => "unknown", quarantine UID/root, hold admission
```
- [ ] Add `TestRestrictedWaitClassification`, `TestRestrictedEnforcementMonotonic`, `TestRestrictedCleanupQuarantine`: actual normal nonzero exit => child_failed, observed SIGSYS => policy_blocked, supported SIGXCPU/SIGXFSZ => resource_exceeded, unexplained SIGKILL/lost Wait => unknown; simultaneous wall/output/cancel retains every observed reason. Exercise actual group/stream/root/once-only UID cleanup, no second child before proof, and handled permitted local-error success controls under the unchanged profile. Generated output cannot set metadata.
- [ ] Bump contract stage to 5 and add new named CaptureBounded/RunRestricted tests. Obtain GREEN composed language runner positives with fresh roots, cancel/setup failure and subsequent request success in existing CI. These add the real lifecycle missing from Task 4 fixtures. Review exact one Wait, one Release, bounded allocation and admission-lifetime expectations before HTTP wiring.

## Task 6: Dedicated strict HTTP/service integration and ordinary compatibility

**Files:** HTTP/service/config owners in map; create `internal/controller/restricted_run_test.go`, `internal/middleware/restricted_admission_test.go`, `internal/service/restricted_test.go`.

**Consumes:** `RestrictedEnvironment`, language RunRestricted and result types. **Produces:** `service.RunRestrictedCode(ctx, language, code, env) *types.DifySandboxResponse`; one injected endpoint admission semaphore/readiness owner. Parent endpoint remains `/v1/sandbox/run` and outer `{code,message,data}`; restricted responses require joint-contract `RestrictedRunResponseV1` with bounded `data.execution` metadata. Ordinary envelopes/options retain their semantics.

- [ ] RED httptest cases: exact/+1 chunked body/code, duplicate unknown JSON fields, malformed UTF-8, compression, missing/null/true network, preload, canceled body, auth rejection, slow client and response escaping. Twenty synchronized callers with a blocking injected runner must observe exactly one start, bounded immediate denials and no queue. Cancellation releases no slot before fake cleanup completion. Any child/limit/cleanup failure carrying fake success stdout must yield nonzero envelope.
```go
if got.Code == 0 { t.Fatal("resource failure returned success") }
if starts.Load() != 1 { t.Fatalf("admitted %d executions", starts.Load()) }
```
The test fixture invokes the actual route/middleware with an injected internal runner function and checked barriers; it is an HTTP contract test, not sandbox conformance.
- [ ] RED/GREEN in Linux CI: `go test -race ./internal/controller ./internal/middleware ./internal/service -run Restricted -count=1`.
- [ ] Implement strict token-level key validation before struct decoding, MaxBytesReader, body deadline and UTF-8 checks; never decode budget fields. Auth first, atomic try-acquire before decode, body deadline then 5s execution context, release only after cleanup. Fixed errors contain no input/code/secrets. Serialize ordinary JSON into a bounded response buffer before write; do not require canonical full-response bytes or a custom Go ASCII encoder. Enforce joint-contract fixed code/message/data mappings; hash exact bounded request bytes and echo its unique invocation header only for associated responses. Unassociated early -429/body/auth errors cannot invent cleanup proof. Canonicalize only validated execution metadata for persisted/exported vectors. Stop admission on environment unhealthy. Restricted startup requires Task 2 ValidateRestrictedAssets **and** Task 4 ValidateRestrictedLauncher plus healthy roots before readiness; finite HTTP settings, dependency mutation denial and no ticker. Missing launcher, partial assets or earlier task GREEN cannot admit execution.
```go
select { case slot <- struct{}{}: default: reject429(); return }
// Deferred release is conditioned on completed cleanup or server termination;
// an unhealthy cleanup quarantines capacity, it does not silently refill slot.
```
- [ ] Add `TestRestrictedResponseSemanticVectors`, `TestRestrictedResponseCorrelation`, `TestRestrictedMetadataCanonicalVectors`, `TestRestrictedCleanupAdmissionBarrier`: share semantic envelope vectors with parent (UTF-8/escaped controls and valid formatting changes), reject duplicate/extra/missing/type/contradictory fields, header duplicates and digest mismatch, compare canonical metadata bytes only, and preserve held/quarantined capacity even after handler/connection return.
- [ ] Bump contract stage to 6, register named route/service/admission tests and obtain CI GREEN including ordinary legacy large-output fixture, network-enabled path, preload handling according to ordinary config, default endpoint and dependency initialization. Do not apply new restricted output caps to ordinary calls. Review this is the first composed restricted endpoint, still unavailable to Dify without Task 8 evidence.

## Task 7: Exact-image Linux conformance and deployment manifest

**Files:** `tests/restricted_integration/{http,budgets,isolation,recovery,identity}_test.go`, `tests/restricted_integration/canary/`; `docker/templates/restricted.dockerfile`, `docker/restricted-compose.yaml`, `deploy/restricted-deployment.yaml`; extend existing Task 1 `.github/workflows/restricted-conformance.yml` and stage inventory; create `build/restricted-conformance.sh`.

**Consumes:** Composed endpoint/image from Tasks 1–6. **Produces:** Actual per-platform/per-language case results and independent measurements; no capability flag. Docker manifest enforces read-only image assets, protected writable request parent, finite resources, one replica and exec-form server PID1 in a private PID namespace. No ordinary service volume/key/env reuse.

The Kubernetes manifest describes only the new `dify-sandbox-restricted` Deployment/Service: one replica; memory request=limit `3Gi`; CPU request `1000m`, limit `2000m`; private pod/container PID namespace with `shareProcessNamespace=false`, no host PID/network or hostPath; server exec entrypoint PID1; read-only image/config/assets and dedicated request-root ephemeral volume; no service-account token mount; only required existing-style chroot/setuid/setgid capabilities, validated against actual runtime. Dev nodes expose only 1900m allocatable CPU, so do not request 2000m or claim two reserved CPUs. Actual capacity/contention must pass useful/budget/recovery conformance on arm64/Linux5.15.0-1121-azure/containerd1.7.34-2. Image is rendered from the actual reviewed immutable digest at release, never a fictional digest. Root's separate Helm repository uses branch `dev`, workflow `.github/workflows/dev-deploy.yaml`, values `examples/value-aks-arm64.yaml`; root-owned future integration must use isolation from that branch and preserve the stale primary `deploy-tagbump-667225ec` checkout/untracked files. This plan does not authorize editing Helm or the EE deployment. A generic YAML declaration is not runtime evidence.

- [ ] Add RED conformance assertions against the baseline in isolated CI: stdout flood cap, shared-root/cwd controls, absent frozen-dependency mutation and measured admission/cleanup/resource gates. Record expected baseline failures; do not weaken tests until green. Use local canary servers only, seeded service/neighbor/previous-root canaries, and trusted harness process/container inspection.
- [ ] Extend the already operational feature-push workflow with stage 7 and a separate full exact-image job. Implement `build/restricted-conformance.sh` stages explicitly: build native binaries+launcher; assemble locked restricted image; resolve immutable image/platform identity; create separate ordinary positive-control sandbox and local canaries; run contract tests plus actual restricted HTTP controls; collect measurements; remove test-owned resources in always cleanup. Do not relabel the earlier contract-test image as the production artifact. Existing main/release publishing is not invoked. Integration command inside the Linux CI job is:
```bash
go test -count=1 -timeout 300s ./tests/restricted_integration/...
```
The job exports its test endpoint/canary addresses through test-only harness config; normal runs with missing config fail rather than skip security cases. Architecture matrix amd64/arm64 may publish separate unavailable results, never borrow arm64 startup evidence for amd64.
- [ ] GREEN requires useful Python/JS JSON and Jinja; exact/+1 input/output; CPU/wall/AS/resident/task/FD exhaustion with independent limits/rusage/peaks; TCP/UDP/DNS/HTTP/HTTPS/IPv4/IPv6/proxy/redirect/Unix-socket canaries positive then zero; `/proc`/descriptor/env/host/neighbor read/write/signal/asset mutation denial; 100 mixed serial/parallel-attempt cycles with max-one and zero leaks. Include root removal/kill/start injected failures and actual server restart: old namespace tasks die, no guessed PID kills, next instance safely removes only owned stale roots. Assert no supervisor/container OOM or restart during budget recovery tests. Every failure is followed by a real positive request on the same surviving service.
- [ ] Bind joint contract §7 outcome cases to the same immutable artifact/profile/language inventory: fake success before fatal external/process denial and attempted SIGSYS handler; independent canaries and filesystem sentinels; actual typed Wait/cleanup outcomes; handled permitted ENOMEM/EMFILE/ENOENT may succeed; unknown SIGKILL/lost Wait cannot. Include parent native retry/default blocker controls in the handoff. No universal-denial-observation gate.
- [ ] Verify actual manifest/runtime PID1, UID range, memory ancestor limits and root read-only mapping. Root preflight owns missing runtime support; no privileged/cgroup/Docker-socket alternative is added. Check current 3 GiB reserve against observed peaks. Review raw case logs/independent counters, not just aggregate job green. A failed or skipped case leaves affected language unavailable and blocks useful parent acceptance.

## Task 8: Trusted evidence export, release gates and Dify Task 5 handoff

**Files:** `cmd/restricted-evidence/main.go`, `tests/restricted_contract/evidence_test.go`, `docs/restricted-release.md`; extend `.github/workflows/restricted-conformance.yml` artifact export. Publication workflow changes in existing `build.yml`/`build-universal.yml` occur only after registry/repo authority and chosen route are established by root.

**Consumes:** Task 7 real test measurements; existing CI identity; parent frozen `SandboxCapabilityReceipt` schema/issuer decision. **Produces:** Versioned nonsecret measurement bundle with actual image/platform/config/dependency/interpreter/network/resource/test digests, per-case observations and CI evidence references. Does not mint an authorizing boolean or treat self-reported endpoint identity as proof.

- [ ] RED export tests reject omitted case, skipped/failing language case, conflicting platform/image, malformed digest, unbounded raw output and untrusted self-reported `verified=true`. Use synthetic test identities labeled fixtures; never present them as deployed evidence.
```go
if err := ValidateEvidenceBundle(bundle); err == nil { t.Fatal("accepted missing required case") }
```
Implement `ValidateEvidenceBundle(EvidenceBundle) error` and `EvidenceBundle` in `internal/types/restricted_evidence.go`: schema version, source/image/platform/config/dependency/interpreter/network/resource/test identities, CI run reference, case list and measurement artifact references. Case entries carry case ID, language, outcome (`passed|failed|skipped`), measured integer counters/durations/bytes and bounded diagnostics. Export validation checks completeness/shape; trust and activation are parent/deployer responsibilities, not this function.
- [ ] Bump contract stage to 8/register named exporter tests; RED/GREEN pure contract command: `go test ./tests/restricted_contract -run Evidence -count=1`. Use joint-contract canonical execution metadata vectors and semantic response vectors; agree the broader required case inventory and canonical raw-measurement/receipt digest schema with parent Task 5 before freezing exporter version; missing agreement blocks this task, not earlier sandbox fixes. Task 1's contents-read-only feature-push workflow still performs no publication; any later authorized publication is a distinct root-reviewed route.
- [ ] Implement exporter as a bounded parser of trusted CI harness artifacts, checking referenced file hashes and exact job outputs; include failed/skipped evidence without granting eligibility. Pin build artifacts and publish only through an authorized registry route after tests. Root has observed official repo pull/push=true, admin/maintain=false, default main; branch push still follows actual protections and root authorization. Remaining gates before publish: actual build-branch publication route, permitted registry, CI runner availability, locked build input resolution, root review. The observed official workflow triggers main/release only; repository push permission is not registry or deployment authority.
- [ ] Document actual deployment-owner checklist: preserve existing Dev EE Deployment; install distinct digest-pinned restricted OSS Deployment/Service/endpoint through root's verified Helm/dev route; observe actual route/transport identity/image/config/mounts/resources/PID/UID/kernel/runtime; run deployed canaries; provide authenticated deployer evidence linked to CI. `kubectl auth can-i` returned yes for create deployments.apps/services, but capacity, image-pull success, exact Helm change and deployed conformance remain unproved. Registry secret/variable names are present without disclosed values; this is not credential validity or feature-branch publication authority. If runtime support or evidence issuer/key verification is missing, report that exact missing owner and stop activation. Never dump credentials, guess host capabilities, add broad mounts, transfer OSS proof to EE or claim Dify API deploy changed CodeSandbox.
- [ ] Hand raw bundle/deployment observations to parent Task 5. Parent receipt must bind exact endpoint/current deployment/language/expiry/revocation and be validated before prepare/worker/RPC. Parent actual invocation recorder and poison/start-finish semantics remain its owner. Task 6 native persisted Python/JS add-one, Jinja greeting, invalid output schema and ordinary default controls must pass on the same deployed identity before enabling. Verify missing/expired/revoked/drifted/language-mismatched/forged receipt denies RPC. No endpoint-generated receipt string supplies trust.
- [ ] Root final review covers ordinary behavior, all spec sections, full evidence chain and outstanding deployment gates. ENG-1189 deliverable is reviewed source + genuine image/runtime evidence; ENG-1183 stays open until useful native Code/template acceptance. Record blocked capabilities honestly; frontend and paused search stay untouched.

## Self-review and execution handoff

Coverage: profile/input→Tasks 1/6; asset immutability/init→2; roots/UID/restart→3/7; resources/filter→4/7; capture/cancel/recovery→5/7; ordinary compatibility→2/6/7; publication/deployment/trust/native parent handoff→8. Five Review Focus conditions have explicit task tests. No cgroup/privileged fallback is scheduled without a measured failure and a new root decision.

Task interfaces match the spec and accepted joint execution contract; add exporter types in Task 8 only after the parent receipt/raw-measurement schema agreement. Root supplied the clean isolated Sandbox worktree and selected sequential reviewed execution; before dispatch root must assess private PID1/UID/resource assumptions, and confirm which CI/publish/deploy authorities exist. Those unknowns cannot be manufactured by an implementer. Every task has a targeted RED/GREEN and an independent review checkpoint; commits or release actions occur only under root's existing authorization and verified results. This plan itself made no such actions.

## Parent-owned receipt trust integration decision

The accepted parent-owned receipt trust boundary is summarized here: dedicated deployer Ed25519 receipt; fresh in-memory restricted-server TLS key on every boot; immutable runtime/config/dependencies; actual authenticated Pod/certificate/conformance observations; existing-SQL current-generation and short transactional attempt admission plus revoke/drain. Sandbox server startup needs restricted-only TLS/Service SAN support with no key persistence/shared key/terminator, and exporter must agree parent canonical measurement schema. Ordinary startup stays unchanged. Dify uses a structural guarded HTTPcore network stream through HTTPX, never observer authorization. CI-only candidate/native test binding precedes signed approval; final unmodified production path remains mandatory. Generation migration is schema-only and operator CLI initializes disabled scope with CAS. The eight-hour receipt/twenty-four-hour boot-certificate candidate remains unfrozen pending measured renewal/E2E/drain availability; actual issuer/bootstrap/publication credentials, exact artifact and deployment conformance remain gates. This handoff is design acceptance, not activation or source/runtime implementation.

## Accepted local/Dev trust handoff amendment (design only)

The parent receipt design names strict Docker/Kubernetes deployment variants and exactly two authenticated operator adapters, separately scoped actual local and Dev receipts/conformance/native E2E. Root accepts this additional design scope: sandbox cmd/public-identity/main.go fixed-loopback public certificate helper, measured build/assets manifest and scoped tests (no authority assertions/key reads), and parent operator-only docker_deployment.py/kubernetes_deployment.py plus protected local operator_key.py. No fabricated Kubernetes IDs for Docker; actual OCI platform manifest vs image-config digest distinction mandatory. Use authenticated local Unix-socket daemon/full-container-ID exec plus exact boot pin, protected inputs and actual process/runtime/resource/network facts; Dify receives no Docker/Kubernetes credentials. Helper must exit before execution budget cases. Real local persisted production E2E remains independent of Dev and test-only preactivation harness.

Dedicated operator key route is design-accepted only:0700 owned no-symlink directory,0600 regular owned no-symlink key, no permissive ACL/path traversal/check-open race, no repo/runtime/synced path/private material in argv/env/logs. No key provision yet. Initial8hour receipt/24hour boot cert candidate requires measured conformance/renewal/native-E2E/drain budget and reviewed availability before activation/freeze; it is not an indefinite availability promise. No automatic scheduler/renewal daemon created. Full report binding and actual release gates remain required.
