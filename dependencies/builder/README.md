# Frozen Builder environment

Build with `scripts/build-builder-environment.sh`. This uses pinned multiarch
Python, Node and Go image indexes, a dated Debian snapshot, hash-checked Python
wheels (`--only-binary=:all:`), and the npm integrity lock. The koffi 2.10.1
install script is run only during the build with network disabled; missing native
prebuilds fail the build. Runtime assembly copies an allowlisted interpreter,
package and ELF-library closure, plus certificate and license provenance.

`/environment-root/environment-manifest.json` records exact architecture,
interpreter and Jinja versions, category and complete-tree SHA-256 digests,
entry count and byte count. Canonical entries bind relative path, mode, owner,
regular-file content, directory presence and symlink target. Absolute links use
chroot semantics; dangling links, loops, escapes and special files are refused.
The manifest digest binds all metadata. The expected digest belongs in the
trusted `isolation.Profile.EnvironmentDigest`; a request only echoes it and
cannot select a root. The supervisor verifies at construction and before launch;
the worker verifies again after recursively mounting the environment read-only,
before private work/temp mounts, bootstrap or submitted preload/code.

`/work` is the sole mutable request mount. HOME, temp and caches are private to
that request. Package managers, service configuration, service keys, qualification
files and sockets are absent from the environment root. The image's outer
filesystem contains build/probe tools for the service, and is never the user
root. Set trusted `SANDBOX_PROFILE=builder` for Builder service processes; this
explicitly skips ordinary runner extraction and refuses dependency mutation.
The ordinary `/main` entrypoint refuses Builder mode because it has no trusted
reservation resolver; use a server wired through `SetupBuilder`. The included
`/conformance-candidate` uses synthetic reservations and is a test fixture only.

Qualification operators must provide a private `/qualification/profile.json`
with `EnvironmentRoot=/environment-root`, the baked digest, trusted paths and
measured finite budgets, plus a private `/qualification/server.yaml` key. No
sample profile is promoted to a release budget. CS5 owns the qualified deployment
capabilities, cgroup delegation, architectures, network and release evidence.

Component probes under `tests/conformance/dependencies` distinguish dependency
loads in a plain chroot, actual native bootstrap/seccomp, unchanged ordinary HTTP
execution, and service-side artifact tampering. They are disposable-container
checks; none is a substitute for `--case dependencies` against the actual
qualified Builder service. The latter requires a profile and independent
namespace/cleanup observations, exercises imports, Jinja, read-only dependencies,
serial and parallel private cache reuse and rejects dependency mutation routes.
A missing profile, namespace setup or delegated cgroup is a failed prerequisite.
