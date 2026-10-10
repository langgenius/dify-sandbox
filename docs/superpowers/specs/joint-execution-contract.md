# Sandbox → parent joint execution contract

Status: **root-accepted DESIGN ONLY** (2026-10-10); no implementation evidence, CI GREEN or activation. Root accepted the types, bounds, mapping and lifecycle below and ruled that HTTP response wire bytes need not be canonical. Scope: sandbox Tasks 1/5/6 and parent P5, with existing Task 4 seal/sameRecorder ownership. Source references: Dify reviewed `4090974607bb8c67f933680fc8d9813ab9d8e543`; sandbox supplied pin `5631afef06ec88f80c28129aec7fd22a30006b14`. This amendment changes documentation only. Native parent CI remains pending.

## 1. Decisions and scope

The binding parent §11 scope ruling is incorporated in the reconciled §10 and this contract. Universal local-error coverage fields and gates are absent. PRD §9.1 requires default Mock/Dry Run and separately authorized real effects; it does not require detecting every caught ENOMEM, EMFILE or ENOENT. Exact admitted immutable profile/receipt and per-language conformance remain admission authority. No tracing, new permissions, state owner, lease engine or cryptographic attestation is introduced.

Keep the existing ordinary API/options/envelope semantics. Only the dedicated restricted branch requires this protocol. Authenticated runtime metadata reports supervisor facts for one invocation; it neither signs a new receipt nor proves business success. Retain the existing TLS binding, receipt/currentness checks, recorder and seal. No client field can choose an outcome, reason, resource budget or cleanup state.

## 2. Go result and lifecycle owner

Freeze these types in `internal/core/runner/types/restricted.go`; all named string types reject empty/unrecognized values at finalization. Use nullable exit/signal and the explicit process/cleanup states below; no scalar-zero or boolean-cleanup parallel authority fields.

```go
type ExecutionOutcome string
type StopReason string
type ProcessState string
type CleanupState string
type ExecutionResult struct {
    Stdout, Stderr []byte // bounded raw capture; never metadata authority
    Outcome ExecutionOutcome
    ReasonCode StopReason
    ProcessState ProcessState
    ExitCode *int // nil is unknown/inapplicable, never implicit zero
    Signal *int
    EnforcementReasons []StopReason // immutable finalized copy; bounded set
    CleanupState CleanupState
    UserCPU, SystemCPU, Elapsed time.Duration
    MaxRSSBytes, StdoutReadBytes, StderrReadBytes uint64
}
func ValidateExecutionResult(result ExecutionResult) error
```

Keep existing `CaptureBounded`, `LimitedCommand` and concrete `RunRestricted` signatures. Capture owns actual Start, exactly one Wait after successful Start, actual WaitStatus, FD3 writer, bounded stream shutdown and group termination. An `exec.ExitError` containing a real terminal WaitStatus is valid observation, not “lost Wait.” A missing/unusable WaitStatus is unknown. Capture's intermediate result has cleanup unknown; only RunRestricted finalizes cleanup after existing root/UID owners complete their checks. Returned Go `error` is internal and never overrides a known latched enforcement fact or supplies a wire diagnostic; it cannot coexist with finalized success.

Enforcement is a monotonic synchronized set, not only “first reason wins.” Freeze maximum eight sorted unique values: `canceled`, `cpu_limit`, `file_size_limit`, `input_limit`, `seccomp_or_sigsys`, `stderr_limit`, `stdout_limit`, `wall_limit`. Only supervisor observations insert values. No AS/NPROC/FD/ENOENT inference, stderr matching or generated response parsing may populate it. Retain internal measurements without expanding wire authority.

Process states: `not_started` = Start never succeeded and no ambiguous launch; `exited` = real normal WaitStatus, exit 0..255 and null signal; `signaled` = real signaled WaitStatus, signal 1..64 and null exit; `unknown` = null exit and signal. `not_started` also has both null. A shell-style exit 128+signal is an ordinary exit, not signal proof.

Cleanup states: `confirmed` requires a started child, terminal Wait, writer/readers stopped, group/no-descendant proof under the conformed profile/private namespace, checked root removal, then once-only UID release. `not_started` requires proven no child plus reclamation of every partially acquired root/lease/FD. `unknown` covers failed/ambiguous Start/Wait/stream/group/root/lease checks. Never infer cleanup from a timeout, EOF, response write or connection close.

Admission remains held through cleanup. Only confirmed/not_started permits slot return. Unknown quarantines existing root/UID ownership, keeps capacity unavailable and readiness false; no second child or speculative release. Restart recovery uses the existing private-PID-namespace lifecycle invariant, never guessed numeric PID reuse. This is the planned lifecycle owner, not a new lease mechanism.

## 3. Exact restricted wire v1

`RestrictedRunResponseV1` preserves outer code/message/data and has exactly the following fields; no optional fields, extras, aliases, duplicate keys, float integers, booleans-as-integers or coercion. `RestrictedExecutionV1` is the execution object. Body is JSON with identity encoding, at most 2,097,152 wire bytes; validated execution object has at most 4,096 canonical metadata bytes within that ceiling.

```text
code: integer
message: string
data:
  stdout: string
  error: string
  execution:
    schema_version: integer 1
    invocation_id: lowercase canonical UUIDv4 (36 ASCII bytes)
    request_body_sha256: [0-9a-f]{64}
    outcome: succeeded | child_failed | policy_blocked | resource_exceeded |
             canceled | rejected | infrastructure_failed | unknown
    reason_code: StopReason
    process_state: not_started | exited | signaled | unknown
    exit_code: integer 0..255 | null
    signal: integer 1..64 | null
    enforcement_reasons: sorted unique array of the eight values in §2
    cleanup_state: confirmed | not_started | unknown
```

Full StopReason inventory (19): `ok`, `invalid_input`, `input_limit`, `capacity_unavailable`, `service_unavailable`, `start_failed`, `child_exit`, `seccomp_or_sigsys`, `wall_limit`, `stdout_limit`, `stderr_limit`, `cpu_limit`, `file_size_limit`, `canceled`, `io_failed`, `invalid_output`, `cleanup_failed`, `unclassified_signal`, `execution_unknown`.

Root ruling: use ordinary bounded JSON for HTTP, with strict UTF-8, duplicate/extra/type/bounds and contradiction validation. Object ordering, insignificant whitespace, literal UTF-8 versus valid JSON escapes and ordinary encoder escaping may differ. Do not compare re-encoded bytes to response bytes or build a custom Go ASCII serializer. Authenticated TLS plus the exact invocation/request-body digest owns correlation. Reject BOM, lone surrogates and invalid UTF-8 rather than replacing them.

Canonicalize **only validated RestrictedExecutionV1 metadata** for persisted fingerprints/exported vectors: recursively sorted keys, compact separators, integer decimal values, literal null and sorted enforcement set; UTF-8 JSON with no BOM/trailing newline. All permitted metadata strings are ASCII identifiers/enums/digests, so this needs no arbitrary-output escape protocol. Name this function `canonical_execution_metadata_bytes`; its SHA-256 is `execution_metadata_sha256` where a fingerprint is needed. It does not hash/canonicalize the entire HTTP response, authenticate a receipt or replace existing canonical observation ownership.

`message="success"`, `data.error=""` only for succeeded; otherwise both strings equal the fixed `reason_code`. Only succeeded carries actual bounded stdout; all other outcomes have stdout `""`. Never expose generated stderr, raw errors, paths, PIDs, secrets or samples. Raw stdout/stderr ceilings remain 262,144/65,536 bytes, with valid UTF-8 and no truncation required for success. Native result-tag/JSON/schema validation remains the existing parent parser's responsibility, not Go's `invalid_output` classifier.

Canonical persisted/exported execution metadata vector (all characters below are literal single-line ASCII):
```json
{"cleanup_state":"confirmed","enforcement_reasons":[],"exit_code":0,"invocation_id":"00000000-0000-4000-8000-000000000001","outcome":"succeeded","process_state":"exited","reason_code":"ok","request_body_sha256":"0000000000000000000000000000000000000000000000000000000000000000","schema_version":1,"signal":null}
```
The zero digest is fixture-only, not a valid correlation claim for an arbitrary body. Share semantic envelope vectors with different valid JSON formatting, literal UTF-8/escaped controls, HTML characters, nulls and every outcome; compare decoded facts. Share byte-exact canonical metadata vectors separately. No second native/request serializer is introduced. Cost if the wire choice proves unsuitable is bounded metadata/vector rework, not permission or policy widening.

## 4. Correlation and early rejection

Parent actual-attempt owner generates a fresh UUIDv4 once and binds it to `SandboxAttemptStart`, admission, transport and terminal. Require exactly one `X-Dify-Sandbox-Invocation-ID` header; validate raw header occurrences case-insensitively, reject comma-combined/duplicate/whitespace/noncanonical values. Add only this header to the existing guarded bridge allowlist. No redirect, fallback, automatic retry or pooled restricted transport.

Parent constructs the ordinary HTTPX request body once, bounds it, stores SHA-256 of its exact bytes in start/admission/binding, and verifies those same bytes at sole dispatch. No JSON reformatting before hashing. Sandbox authenticates, try-acquires the existing slot before body decode, consumes the bounded entire identity body, hashes exact bytes before decoding, then echoes the header/hash. Request JSON cannot supply either value. Strict unknown/duplicate request keys remain rejected.

Every fully associated restricted envelope uses HTTP 200 and the typed negative codes below, matching envelope-style route handling; parent validates status and body together. Auth rejection, missing header, immediate full-slot rejection, over-limit/incomplete body and transport failure may lack a complete-body digest: return bounded ordinary error/status as appropriate, never fake a digest or assert associated cleanup. Parent classifies these as unassociated unknown. In particular immediate -429 must not read an unbounded body merely to manufacture metadata. Typed capacity_unavailable is possible after complete bounded-body acquisition if an internal lease cannot be acquired.

An associated response must match the admitted UUID, exact request digest, authenticated endpoint/boot binding and deadline. Unknown version/enum, missing/excess field, duplicate key, bounds/encoding violation or contradiction is protocol unknown. No ordinary-decoder fallback. Response correlation is not replay protection or new attestation; invocation uniqueness and existing receipt/deadline admission still apply.

## 5. Deterministic outcome and numeric mapping

Apply precedence below; preserve the complete enforcement set even when unknown wins. In a complete lifecycle, reason precedence within enforcement is `seccomp_or_sigsys`, `input_limit`, `wall_limit`, `stdout_limit`, `stderr_limit`, `cpu_limit`, `file_size_limit`, `canceled`. This ordering is deterministic presentation, not a claim about temporal/root cause order.

| Condition | outcome / reason_code | code |
| --- | --- | --- |
| Process/Wait/start ambiguity (also requires cleanup unknown) | unknown / execution_unknown | -500 |
| Known process state but cleanup unknown | unknown / cleanup_failed | -500 |
| Actual unexplained signal, including SIGKILL, with no established enforcing cause | unknown / unclassified_signal | -500 |
| Observed SIGSYS (31 on Linux amd64/arm64), with complete lifecycle | policy_blocked / seccomp_or_sigsys | -500 |
| Observed input/wall/stdout/stderr limit or actual SIGXCPU=24 / SIGXFSZ=25 | resource_exceeded / first resource reason in precedence | -500 |
| Observed cancellation only, with complete lifecycle | canceled / canceled | -500 |
| Fully read invalid request, no child and reclaimed preparation | rejected / invalid_input | -400 |
| Fully correlated lease capacity refusal, no child and reclaimed preparation | rejected / capacity_unavailable | -429 |
| Fully correlated unhealthy service refusal, no child and reclaimed preparation | rejected / service_unavailable | -503 |
| Proven Start failure with reclaimed preparation | infrastructure_failed / start_failed | -500 |
| Known lifecycle, stream/input I/O failure or invalid captured UTF-8 | infrastructure_failed / io_failed or invalid_output | -500 |
| Normal nonzero exit, no enforcement or I/O failure, confirmed cleanup | child_failed / child_exit | -500 |
| Normal exit 0, no enforcement/error, bounded valid untruncated streams, confirmed cleanup | succeeded / ok | 0 |

Prelaunch input limit or cancellation may have process/cleanup not_started; after Start, known outcomes require exited/signaled plus confirmed cleanup. Enforcing reason wins over child exit or malformed output; unknown lifecycle/cleanup wins over outcome while preserving enforcement. A signal resulting from the supervisor's latched wall/output/cancel termination keeps that cause. SIGKILL alone never means CPU/AS/OOM. Equal soft/hard CPU limits may yield SIGKILL: this honestly remains unknown unless another trusted cause exists. SIGSYS is conservatively named `seccomp_or_sigsys`, not proof of one exact forbidden syscall. Contradictory enums/code/message/process/cleanup/exit/signal combinations are rejected, never repaired into success.

## 6. Parent typed terminal and existing seal mapping

Extend the existing `SandboxAttemptStart`, `SandboxAttemptAdmission` and `RestrictedSandboxBinding` with required `request_body_sha256: Digest`. Extend `SandboxAttemptFinish` in place with `execution: RestrictedExecutionV1 | None`, `observed_enforcement_reasons: tuple[StopReason, ...]`, and `evidence_state: Literal["complete","blocked","unknown"]`; retain its existing invocation/outcome/dispatch/cleanup fields. This is a bounded snapshot in the same observation store, not another receipt/table/recorder. Existing receipt/profile/context/native-run/task/node/implementation/code/input bindings remain mandatory and matched.

`sandbox_started` is durable before RPC. Exactly one `sandbox_completed` or `sandbox_failed` terminal uses that invocation and same recorder. Finish uses outcome completed only for valid succeeded metadata plus successful existing post-response currentness checks, otherwise failed. Its reason_code is the validated wire reason, or fixed local `sandbox_transport_unknown`, `sandbox_protocol_unknown`, `sandbox_currentness_failed` or `sandbox_not_dispatched`. Local no-RPC proof can use not_started; may-have-sent/no valid associated response uses cleanup unknown. An append failure poisons the same recorder; no fallback instance or synthetic terminal success.

Evidence state is unknown for invalid/missing response, unknown outcome/cleanup, unassociated dispatch, missing/currentness/pairing/native coverage proof; blocked for complete authenticated policy/resource/canceled/rejected/infrastructure outcomes; complete for succeeded or honest child_failed. This contract uses blocked for known operational refusal to prevent native default recovery manufacturing eligibility, without calling it a business-code fault. Persist any valid correlated enforcement set independently when lifecycle validation fails: only authenticated, exact UUID/body-matched, structurally valid enum-set evidence qualifies; child text, mismatched/unparseable response or malformed set contributes nothing. Unknown precedence remains, and previously valid blocked IDs survive.

For complete metadata, observed enforcement set equals execution.enforcement_reasons. No enforcement is required for rejected/infrastructure blockers. Parent decoder creates a fixed reason-code CodeExecutionError subtype from trusted fields; code/message/stderr alone never classifies. `succeeded` permits existing native result parser and Graphon schema checks. Parser/schema failure is real native failure even when sandbox_completed was already recorded. `child_failed` may enter existing native failure/diagnostic policy, without inferring ENOMEM, EMFILE or a definite business bug. No positive business conclusion follows from sandbox success alone.

Reuse Task 4 seal precedence: incomplete proof → execution_evidence_unknown; complete failed/stopped/partial native status → native_failed; complete native success plus retained blocker → execution_blocked; only otherwise consider existing simulation/restricted completion. Keep blocked_node_ids even beside unknown/native_failed, and consume that blocker at existing diagnosis/repair/publication gates. Defaults/retries never erase enforcement/blockers; fresh invocation IDs still match actual native attempt/retry ownership. SameRecorder health and canonical persisted rows remain required. Never upgrade an already sealed unknown or rewrite native status to fit a response.

## 7. Same conformance inventory and outstanding measured gates

Task 1 pure contract cases cover every row, empty enums/null versus zero, contradiction, sorted-set bounds and canonical metadata vectors plus semantic wire variants. Task 5 exercises actual Start/one Wait, real exit/signal mapping, N/N+1 streams, simultaneous causes, cancellation races, lost Wait, retained pipes and root/UID/group failures; hold cleanup barrier while second admission is refused, then prove release only on success and quarantine on uncertainty. Task 6 runs these typed results through real restricted HTTP/correlation/serialization/admission and preserves ordinary regressions. Parent P5 runs the same vectors plus TLS/body mismatch, malformed/legacy response, recorder poison, native retry/default and retained blocker/seal precedence. Fixtures prove contracts only.

Tasks 7–8 and parent native/local/Dev gates bind these same case IDs/results to the actual immutable artifact/profile/platform/language inventory. Mandatory cases still include useful Python/JS/Jinja, fake success before fatal external/process attempts, attempted SIGSYS handler, supported non-main-thread denial, independent positive then zero TCP/UDP/DNS/HTTP/HTTPS/proxy/redirect/IPv4/IPv6/Unix canaries, descriptors/env/memory/host/service/neighbor/previous-root isolation, immutable assets and healthy repeated recovery. A failed forbidden-read/write sentinel is conformance failure even if the child catches an error.

Comparison controls deliberately handle permitted local allocation/FD/lookup failure and return deterministic valid native output: normal exit0 plus no observed enforcement and confirmed cleanup may succeed under the conformed profile. Known wall/output/input/policy blocks cannot be cleared by handled exceptions or default output. Unexplained SIGKILL/lost Wait remains unknown. No universal telemetry obligation is hidden in the inventory.

Remaining implementation/platform gates are measured fatal syscall inventory and all-thread/native compatibility, actual group/stream/root/UID lifecycle proof, exact resource/recovery measurements, authenticated endpoint/current receipt, Task 4 actual native attempt matcher and sameRecorder integration, canonical fixtures, and existing independent local/Dev production E2E/release gates. No new blocking product ambiguity was found; these are implementation/platform evidence gates, not passing facts. All six verified Dev backend APIs still gate frontend tickets; ENG-1177 remains paused.

## 8. Repository document ownership

Read this contract with the [restricted sandbox design](2026-10-10-restricted-sandbox-design.md) and [implementation plan](../plans/2026-10-10-restricted-sandbox.md). Parent Dify P5 consumes the same contract and owns receipt verification, generation/currentness, native adapters, sameRecorder and Task 4 seal integration; these are cross-repository implementation dependencies, not missing local document links. Root has reconciled the corresponding parent trust documents. Root review/commit of these documentation files precedes Task 1 dispatch. Design acceptance closes no implementation/platform gate in §7.
