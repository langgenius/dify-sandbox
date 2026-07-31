# Dify-Sandbox
## Introduction
Dify-Sandbox offers a simple way to run untrusted code in a secure environment. It is designed to be used in a multi-tenant environment, where multiple users can submit code to be executed. The code is executed in a sandboxed environment, which restricts the resources and system calls that the code can access.

## Use
### Requirements
DifySandbox currently only supports Linux, as it's designed for docker containers. It requires the following dependencies:
- libseccomp
- pkg-config
- gcc
- golang 1.20.6

### Steps
1. Clone the repository using `git clone https://github.com/langgenius/dify-sandbox` and navigate to the project directory.
2. Run ./install.sh to install the necessary dependencies.
3. Run ./build/build_[amd64|arm64].sh to build the sandbox binary.
4. Run ./main to start the server.

If you want to debug the server, firstly use build script to build the sandbox library binaries, then debug as you want with your IDE.

### Node.js runtime compatibility

Every sandboxed Node.js process starts with V8's `--jitless` option as a fixed
security property. JavaScript therefore runs without JIT compilation,
WebAssembly is unavailable, and CPU-intensive workloads may run more slowly.
Basic JavaScript features such as JSON, Buffer, exception handling, and network
APIs remain available subject to the sandbox's existing policies. There is no
configuration switch to enable JIT or WebAssembly.

After the trusted runtime bootstrap installs seccomp, `mmap` and `mprotect`
requests containing `PROT_EXEC` terminate the sandbox process. Existing native
code loaded before that boundary can still execute, but new `RX`/`RWX` mappings
and staged `RW` to `RX` transitions are unavailable. Native extensions loaded
on demand, FFI callbacks, and packages that generate machine code at runtime
may therefore fail. Native modules needed by the sandbox's documented Python
features are loaded by the trusted bootstrap before seccomp is installed;
Requests and HTTPX are loaded only for executions with network access enabled.

## FAQ

Refer to the [FAQ document](FAQ.md)


## Workflow
![workflow](workflow.png)
