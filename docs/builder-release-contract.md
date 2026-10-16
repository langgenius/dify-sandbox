# Builder actual-image qualification contract

Status: implementation in progress; no capability is issued by these files.
Base: `7dd9f31dd6d3d8a39bd1057049506fce843f0d3c`.

## Isolated normal build and independently pinned harness

`builder-candidate-build.yml` is a manually dispatched, source-only prepared
lane. It is not connected to ordinary main/release production builds. A reviewed
owner must authorize the isolated same-repository GHCR package before dispatch;
this source change creates no package, permission, variable, environment or run.
No value defaults to a production image, main or latest tag.

The protected owner must supply all of these repository variables through an
administrative process independently of the candidate revision/document:

| Input | Required owner value |
| --- | --- |
| `BUILDER_APPROVED_SOURCE_REVISION` | Exact reviewed candidate source SHA (40 lowercase hex). |
| `BUILDER_TRUSTED_BUILD_WORKFLOW` | `langgenius/dify-sandbox/.github/workflows/builder-candidate-build.yml` for this repository. |
| `BUILDER_TRUSTED_BUILD_REVISION` | Exact reviewed build workflow commit. Manual build requires its OIDC source to equal the approved source. |
| `BUILDER_TRUSTED_HARNESS_REVISION` | Separately reviewed immutable harness checkout SHA, containing the verifier/provision scripts and fixture policy. Never filled from candidate input. |
| `BUILDER_TRUSTED_QUALIFICATION_REVISION` | Exact reviewed conformance entrypoint SHA. Dispatch/call that owner-pinned workflow revision. |
| `BUILDER_CANDIDATE_IMAGE` | Explicit registry-owner authorization for `ghcr.io/langgenius/dify-sandbox-builder-candidate`. This proposed package has not been created or tested for write authorization. |

The owner must protect the actual workflow entrypoint/ref and these expected
identities from candidate authors, and dispatch/call only that reviewed
entrypoint. A workflow at an arbitrary attacker-controlled Git ref is outside
this trust boundary: its own gate could be replaced. A trusted caller can pin
the reusable conformance workflow to the reviewed SHA. Missing/malformed
protected inputs fail before checkout; a selected candidate SHA must match the
independently approved source. Candidate source never supplies verifier code,
privileged setup, signer identity or expected harness identity.

Native amd64 and arm64 build jobs have only contents-read permission and no
registry login or OIDC grant. They execute the existing pinned Builder
Dockerfile in Docker's embedded BuildKit, without build secrets/host network or
emulation. Existing runner tools are pinned and checked before the Dockerfile:
Docker server/client 28.0.4 at upstream commits `6430e49`/`b8034c0`, Buildx 0.37.2
at `2d379c0`, and the default Docker driver. No privileged BuildKit container or
mutable tool download is introduced. Different hosted tool versions fail for
owner review rather than silently changing the builder. Both hosted runner
architectures still need an actual tool/version compatibility run.

A separate publish job consumes exactly two immutable current-run native
archive artifacts. It checks both source/platform/run/attempt/content receipts
before loading either image, confirms loaded config/architecture, pushes only
`candidate-<source>-<run>-<attempt>[-<arch>]` tags in the isolated package, and
assembles an OCI image index from the actual returned platform digests. The
maintained ORAS 1.3.0 publisher uses an explicitly SHA256-pinned official archive
and a commit-pinned setup action. Its returned OCI descriptor must match the
exact index bytes, media type and size. It rechecks raw
index/platform bytes and config bindings. Only this job requests packages-write,
attestations-write and OIDC. `actions/attest` at reviewed commit
`7a37fbbab9669658a566371acd1cf2f8c7a40d32` generates its maintained default SLSA
provenance for the exact returned index and both platform subjects; no custom
predicate stands in for a build. The bundle is stored in GitHub and retained
with raw build/publication logs, receipts and exact image refs. Registry storage
records and attestation registry pushes are disabled, so artifact-metadata-write
is not requested. A partial rerun with old-attempt native artifacts is rejected;
rebuild both native jobs for a fresh attempt.

Conformance checks out only the protected harness SHA. That harness executes
`gh attestation verify` with protected repository/signer workflow/signer SHA,
approved source SHA, SLSA predicate and deny-self-hosted policy, separately for
the immutable index and both native manifests. It checks authenticated subjects,
exact two-platform inventory, raw OCI digests and native pulled config before
provisioning. Read-only registry/API credentials exist only in that authentication
step and are removed before setup. The successful current-job report is handed
off as a root-owned 0600 file; candidate JSON cannot replace it. Provisioning
checks its source/harness/run/attempt/image and the loaded config again before
export. All setup scripts/services come from the pinned harness, and service
payloads receive no CI credentials. No candidate checkout is executed by this
workflow.

Qualification `source_revision` remains the exact built candidate SHA;
`ci.test_revision` records the independent harness SHA. Release verification
requires an explicit well-formed protected `expected.harness_revision` matching
that test revision, in addition to existing source/image/provenance/controller
checks. The signed provenance envelope must bind both identities. Neither
successful JSON assembly nor synthetic verifier tests issue a qualification.

This preparation has not proven GHCR organization/package policy or actual
attestation publication. Repository permissions observed were push/triage, not
admin/maintain; Actions policy reads returned HTTP 403. An authorized owner must
settle registry permission, protect the exact inputs/entrypoints, then explicitly
approve and dispatch the reviewed build. Review the returned immutable index
before separately authorizing native kernel conformance. Ordinary production
build/release files are unchanged.

## Fixture setup contract (review before activation)

Run only on a fresh disposable Linux host of the actual target architecture,
with cgroup v2 and an externally provisioned empty delegated subtree. Never use
buildx emulation as kernel qualification. Build credentials stay in a different
job. Execution receives no repository tokens, cloud credentials, Docker socket,
host home directory, or business network. The CI control process may use the
runner Docker CLI, but no socket is mounted into any execution container.

The runner owner supplies a narrow cgroup-v2 directory whose parent has cpu,
memory and pids enabled and whose child controls include cgroup.kill,
memory.swap.max, memory.oom.group and memory.peak. The suite does not discover,
mount or modify the host cgroup root. The directory is bind-mounted only at
/delegated. The candidate requires authority to move its own workers into this
subtree. A platform owner must provision that delegation in the ephemeral
runner before activation; a label or writable ordinary directory is insufficient.

A fixture setup process creates a fresh outer network namespace with only lo,
assigns 192.0.2.10 and 192.0.2.20 plus 2001:db8::10 and 2001:db8::20 to lo,
and brings lo up. It creates no veth/default route/bridge, changes no host
sysctl, and cannot reach business or public networks. NET_ADMIN is needed only
by that bounded setup process in the new namespace, then dropped. Canary,
candidate parent and integration client share this outer namespace. Candidate
payloads receive their own inner net/PID/mount/IPC/UTS/user namespaces. The
none-mode user-namespace and UID-custody production prerequisite is not yet
qualified; a static none-mode host UID fails this suite. Only the
trusted parent broker can connect from outer namespace to approved canary IPs.

The candidate is the fresh dedicated nonroot `dify-builder-ci` service account.
Its bounding, ambient, permitted and effective capability sets are exactly
SYS_ADMIN, SYS_CHROOT, SETUID, SETGID and DAC_OVERRIDE, checked from actual proc
status with its nonzero UID before requests. No-new-privileges is mandatory.
The account owns only fixture state, journals, observations and its private
server configuration; systemd delegates only the dedicated service cgroup.
The client cannot write those files. Root setup retains control of public
profile/configuration files and installed service/LSM definitions. No privileged
container, NET_ADMIN in candidate parent, host PID/network namespace, broad
host cgroup mount or AppArmor-unconfined is allowed. A reviewed runtime
seccomp/LSM profile must admit the wrapper's namespace/mount/chroot/rlimit and
FD operations while retaining language seccomp. The suite must refuse a missing
profile. The AppArmor profile permits AF_UNIX stream and seqpacket controls,
the exact supervisor bind/private/read-only/tmpfs operations and read-only
same-profile process inspection. It grants no parent SETFCAP, NET_ADMIN or
SYS_PTRACE. Existing Docker default seccomp/LSM support is not presumed. The
runner owner supplies the immutable runtime-profile digest and proof of its
applied identity. This is a genuine activation input, not an operator pass flag.

Canary runs as an unprivileged user, read-only filesystem, all capabilities
dropped, no-new-privileges, max 128 MiB memory, 32 PIDs, 128 FDs. Candidate's
outer ceiling is 4 GiB/512 PIDs/1 CPU initially for measurement, never a released
per-request budget. Integration is 256 MiB/32 PIDs; finite 20-minute total run.
Qualification, observations and output are fresh private directories. Only
candidate writes observations; integration reads them. API key is fresh random
fixture-only configuration shared between client and trusted server; it never
enters a payload environment or public evidence. TLS key stays in canary only.

## Collector and readiness

/qualification/network.json contains CollectorURL plus exactly ten Targets
(tcp/udp/dns/http/https, both address families), and four AlternateTargets
(http/https, both families). Primary addresses are .10/::10; alternates .20/::20.
Ports are 19001 TCP, 19002 UDP, 19003 DNS, 19004 HTTP, 19005 HTTPS. Collector is
127.0.0.1:19000 and is accessible only to trusted outer-namespace components.
Counter keys are protocol/address/port. GET returns boot_id, counters, active,
received_bytes. All maps include every configured target, even at zero. Counters
are actual accepted connections or datagrams; received_bytes counts actual
socket reads (TLS counts wire reads, not asserted payload length); active is the
number of currently accepted open TCP connections, including HTTP/TLS; UDP/DNS
active is always zero. Values are monotonic except active. No reset endpoint.

Readiness requires every listener bound and ten independent positive probes,
followed by collector counter increases and active return to baseline. A changed
boot_id invalidates all comparisons. /ok returns 200; /redirect-unapproved is
302 to the corresponding alternate address; /bytes?count=N returns at most
1 MiB; /hold heartbeats until disconnect or 15 seconds, with prompt close
observation. rebind.fixture.test A/AAAA first returns primary and then alternate
per family. No child can read collector endpoints, certificates or control keys.

## Trusted verification port

verify_release(document, *, trust=None, expected=None) rejects missing trust.
The trust object authenticates detached signatures over canonical qualification,
build provenance and endpoint receipt bytes against independently pinned public
keys, enforces key purpose, expiry and revocation, and returns authenticated
payloads. No signed/verified boolean from an input document grants authority.
Expected deployment identity, generation, platform, image/source/environment
and effective mount/network/resource/configuration digests are supplied by the
trusted deployment owner, never taken from the untrusted document itself.
The sandbox's own digest is corroboration only. Release and deployment keys are
separate. The normal release owner signs after every required case passes; the
trusted controller signs a receipt only after examining actual image, immutable
mounts, routing and effective configuration. This repository does not invent or
issue production endpoint receipts.

## Measurement and remaining gates

Measure serial and four-way mixed Python/Node dependency imports plus Jinja;
record cold/warm completion and cgroup CPU/memory plus sampled PID/FD/disk/output
peaks. The sampling period and observation source must accompany measurements;
no unsampled peak may be called an exact maximum. Select finite budgets with
explicit headroom and rerun all resource exhaustion and recovery cases. Do not
promote initial measurement ceilings or historical values into release limits.
Samples taken only during trusted setup cannot qualify native execution:
the fixed workload must first emit its readiness marker, and live owned-cgroup
proc observations must then show FD/tmpfs use and zero CapEff/CapPrm/CapAmb.
Missing process-inspection authority rejects the measurement; it never silently
uses the setup sample. Actual kernel validation of this observer is pending.

Both native amd64 and arm64 hosts, actual immutable candidate digest, reviewed
runtime seccomp/LSM profile, delegated cgroup, and trusted release/deployment
public keys and endpoint metadata are required inputs. Python requests and
proxy-aware Node HTTP/CONNECT must prove scoped operation. Raw protocol egress,
proxy-unaware Node clients and private business destinations remain unsupported
by the declared scoped profile; explicit none-mode denial still covers them.
The six groups are roots-and-budgets, lifecycle, dependencies, network,
qualification and ordinary-regression. Ordinary controls use their separate
unchanged endpoint and must prove Python/Node and controlled network=true.
Prior ordinary Python SIGSYS is an unresolved observed baseline gate, never a
skip or authorization to weaken its filter.
