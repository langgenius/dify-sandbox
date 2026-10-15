# Builder invocation network and data scope

Builder networking defaults to `none`. It uses the existing native Python/Node
runners inside a fresh network namespace and keeps their independent no-network
seccomp behavior. The ordinary endpoint and its legacy network option are
unchanged. Builder never interprets that option as an authorization grant.

The implemented scoped capability is **explicit HTTP proxy and HTTPS CONNECT
mediation to public, exact address/port destinations**. It is disabled until a
trusted deployment profile is qualified. Raw TCP, UDP, DNS, private business
addresses, SOCKS, wildcard hosts and direct network access are not supported.
Python libraries that honor proxy environment variables can use the proxy. Node
or Python code that ignores proxies needs to speak the explicit proxy protocol;
there is no direct-route fallback or generated user preload.

## Authorization and immutable disclosure

`NetworkScope` has `mode`, `destinations`, `input_manifest_digest`,
`max_egress_bytes`, `expires_at` (Unix seconds), and `scope_digest`. Each destination
contains `protocol`, `host`, `port`, and an ordered array of literal `addresses`.
Only `none` and `scoped` modes are accepted. `none` must be empty. Scoped input and
scope digests are lowercase SHA-256 hex. The scope digest is SHA-256 of Go's JSON
encoding of the scope with its digest field empty; ordered arrays and all other
fields are bound. At most 64 destinations and 16 pins per destination are parsed,
and the parser's 1 TiB ceiling never expands the trusted profile or approval's
smaller byte limit. Expiry and the authoritative execution wall deadline both
apply; expiry can only shorten the existing admission-to-run deadline.

A digest is not authority. The unchanged trusted composition port is
`ReservationResolver(context.Context, InvocationSpec) (Budget, error)`. The Dify
CS6 owner must reconstruct the complete immutable invocation from its protected
approval/reservation and compare execution identity, canonical code/preload and
input manifest, environment/dependency digest, profile, network scope, remaining
budget and expiry. It must reject a client that changes values and recomputes
hashes. The invocation digest binds the exact canonical payload bytes and entire
specification. This repository does not mint approvals or accept model/client
approval flags. Its synthetic `cs1-*` conformance resolver is not a production
authorization implementation.

The server metadata owner resolves and qualifies DNS addresses before approval
and checks freshness/drift before launch as its contract requires. Those exact
addresses are authenticated pins. The gateway performs **no runtime DNS lookup**
and never uses proxy environment variables; it dials only an approved literal.
A later DNS answer cannot select a new endpoint. Every HTTP request, including a
request on an existing connection, and every CONNECT authority is checked against
the approved protocol, host and port. Redirects are returned to the client and
its next request is checked independently. Proxy authorization and hop-by-hop
headers are removed. IP literals must match their sole address pin. IPv6 mapped,
scoped/link-local, private, loopback, metadata and transition address forms are
refused. Broader private business endpoint support needs an explicit server-owned
network range qualification; this version does not provide it.

Dify owns actual input-manifest/file and approved SecretRef-version resolution.
The Sandbox payload currently carries code and preload, not an independently
materialized input-manifest body. Shape/hash validation here does not verify that
a declared manifest describes external data. Approval must disclose the full
opaque script, all accessible data/files, execution identity, dependency digest,
destinations, and dispatch/byte/time bounds. Hidden messages inside arbitrary
code are not separately approved or inspected. CONNECT is opaque destination-bound
traffic, including TLS contents and SNI; it is not per-message semantic approval.
Any related mutation requires a new common-owner decision before launch.

## Runtime boundary

The supervisor owns one immutable gateway policy per execution. It creates a
private Unix sequenced-packet socketpair, passes only its frontend side as worker FD 4,
and retains FD 3 as the payload gate. No management listener or credential is
available to the child. The parent closes its duplicate frontend endpoint immediately after successful worker start, so helper endpoint loss delivers EOF and revokes handed-off streams. On start failure, the caller retains endpoint cleanup ownership. The worker is attached to its cgroup before the parent
sends trusted-setup byte `2`. Scoped workers add a fresh user namespace: namespace
UID 0 maps to the trusted supervisor UID, namespace UID 65537 maps to a durable
per-execution host UID lease, and GID 0 maps to the existing supervisor GID with
setgroups enabled. Missing mapping privileges are a failed prerequisite.

Inside that owning user/network namespace, trusted setup enables only loopback,
then drops NET_ADMIN from bounding, effective, permitted and inheritable sets.
The service receives no new capability and never enters a child net namespace.
There are no veths, host routes, host listeners or direct external routes. After
its read-only chroot and request tmpfs are installed, the worker starts the measured
`/usr/local/libexec/network-gateway` binary with empty environment, empty stdin,
namespace UID 65537 and no capabilities. The helper sets dumpability to zero
before readiness. Payload has no proc mount, control FD or ptrace/pidfd syscall
permission. Helper lifetime is fenced by PID-namespace init death and cgroup
cleanup, not parent-death signaling across the worker's later UID transition.

The helper binds `127.0.0.1:3128` and passes accepted client sockets with SCM_RIGHTS
to the parent broker. Readiness is a single no-FD frame; each later frame must
contain exactly one connected IPv4 loopback stream socket. Truncation, extra or
unexpected descriptors, wrong socket types and repeated readiness revoke the
channel; received descriptors are closed on rejection. Each execution has at
most 64 tracked sockets and 16 active proxy requests, bounded headers and read
timeouts. `ApplyScope` checks an owned, distinct network namespace FD and requires
that loopback is its only interface. Scoped application also requires this exact
execution's ready private gateway. Only then does the parent send byte `1` to
release the payload. Missing/unsupported raw capability is rejected before launch
with `configuration_required/network_capability`.

Payload inherits only fixed runtime values and proxy URLs, never service keys,
proxy-control credentials or ambient environment. The existing bootstrap selects
a separate scoped native seccomp export with fixed TCP client operations and
AF_INET/AF_INET6 SOCK_STREAM creation only. Raw/UDP/AF_UNIX sockets, socketpair and
arbitrary syscall overrides are unavailable. The original isolated export retains
its socket EPERM/no-network behavior. Both isolated variants ignore
`ALLOWED_SYSCALLS`; the ordinary variant remains unchanged.

The broker counts outbound stream bytes, including HTTP headers/body or opaque
CONNECT stream bytes, across all destinations. It reserves bytes before writes,
so partial/failed writes conservatively consume their reservation. IP/TCP packet
headers and handshake packets are not application stream bytes. No DNS traffic
occurs at runtime. Quota exhaustion, expiry, cancellation or channel failure
closes all active broker sockets. Receipts contain identity, policy digest,
reserved egress bytes, the first revocation reason and the rejected write byte count, never packet contents or credentials. Routine final close preserves a prior quota, expiry or channel-loss cause.

The finite host UID pool uses private durable leases at the canonical execution
journal path plus `.uids`. Requested profile ranges are explicit, exclusive,
finite, disjoint from ordinary UIDs 10000–10999 and the static none-mode UID 65537.
Work-root and cgroup paths are durably tracked before creation. A lease is returned
only after cgroup/process/root cleanup, terminal journal commit and independent
process/resource absence checks. Unknown cleanup or failed final release retains
both UID and execution capacity in quarantine. There is no passwd mutation or
fallback to a shared ordinary UID allocator.

## Trusted profile and build contribution

`HTTPProxyQualified` defaults to false. Enabling it also requires explicit
`HostUIDMin`, exclusive `HostUIDMax`, and `MaxNetworkEgressBytes`. The bounded range
is at most 4096 UIDs, starts at or above 11000, and excludes 65537. These are
server-owned profile fields bound by the approved profile identity, not invocation
options. The measured executable must be regular, executable and nonwritable by
group/others. The complete environment manifest binds it alongside interpreters,
dependencies and native libraries.

The root-owned image builder must statically build `./cmd/network-gateway`, copy
it to `/environment-root/usr/local/libexec/network-gateway` with mode 0555 after
environment assembly and before final manifest creation, and rebuild native
libraries and request-worker. A different image/environment digest requires fresh
qualification. This source alone does not qualify a profile.

## Evidence and required qualification

Component tests exercise real HTTP parsing, inherited Unix FD transfer, pinned
synthetic socket traffic, denial/rebinding/redirect boundaries, aggregate quota,
revocation, authorization refusal and durable UID refusal. Native filter tests
install actual kernel seccomp filters inside the existing unprivileged Linux unit
fixture. They are not proof of the complete user/net/mount/cgroup execution path.

`integration --case network` requires protected `/qualification/profile.json`
and `/qualification/network.json`. The latter contains `CollectorURL`, ten
`Targets` (TCP/UDP/DNS/HTTP/HTTPS × IPv4/IPv6), and four `AlternateTargets`
(HTTP/HTTPS × IPv4/IPv6), each with `Protocol`, `Address`, `Host`, `Port`. Controlled
addresses are synthetic documentation addresses within the isolated CI fixture.
Collector GET returns `boot_id`, monotonic `counters`, actual accepted-stream `received_bytes`, and live `active` maps keyed
`protocol/address/port`; all keys must be independently present. HTTP paths are
`/ok`, `/redirect-unapproved`, `/bytes?count=N` (at most 1 MiB), and `/hold` with
bounded heartbeat traffic. The DNS canary changes `rebind.fixture.test` from the
primary address to an alternate after its initial query.

The case first requires traffic on every permissive control, then zero traffic
from actual none execution. It checks approved proxy traffic, rejected alternate
and redirect destinations, simultaneous separate scopes, immutable DNS pins,
quota, expiry, cancellation and independent live-connection settlement. Quota requires successful workload completion, attempt/completion markers, a nonroutine egress-quota receipt, an observed canary connection and independent received bytes within the approved ceiling. Expiry requires readiness and an independently live hold before scope cutoff; elapsed time through actual connection settlement must remain within expiry plus twice CleanupMillis and 250ms of harness allowance. The ordinary wall limit must be strictly later than this bound. Scope UTC expiry is converted once and subsequent duration checks use the harness monotonic clock. Synthetic
TLS certificate skipping exists only in the test client, not the production
broker. Missing fixture/profile/capability/evidence fails; it never skips or
manufactures a qualification pass. Actual image, UID/GID/setgroups, namespace,
loopback capability, native Python/Node, amd64/arm64, cold/load and Local/Dev/OCI
qualification remain the root-owned CS5 gate. Existing CS3 verification deadline
and stuck-verifier quarantine are preserved.
