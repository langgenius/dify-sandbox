# Overview

Koffi is a fast and easy-to-use C FFI module for Node.js, featuring:

* Low-overhead and fast performance (see [benchmarks](https://koffi.dev/benchmarks))
* Support for primitive and aggregate data types (structs and fixed-size arrays), both by reference (pointer) and by value
* Javascript functions can be used as C callbacks (since 1.2.0)
* Well-tested code base for popular OS/architecture combinations

The following combinations of OS and architectures __are officially supported and tested__ at the moment:

ISA / OS           | Windows     | Linux    | macOS       | FreeBSD     | OpenBSD
------------------ | ----------- | -------- | ----------- | ----------- | --------
x86 (IA32) [^1]    | ✅ Yes      | ✅ Yes   | ⬜️ *N/A*    | ✅ Yes      | ✅ Yes
x86_64 (AMD64)     | ✅ Yes      | ✅ Yes   | ✅ Yes      | ✅ Yes      | ✅ Yes
ARM32 LE [^2]      | ⬜️ *N/A*    | ✅ Yes   | ⬜️ *N/A*    | 🟨 Probably | 🟨 Probably
ARM64 (AArch64) LE | ✅ Yes      | ✅ Yes   | ✅ Yes      | ✅ Yes      | 🟨 Probably
RISC-V 64 [^3]     | ⬜️ *N/A*    | ✅ Yes   | ⬜️ *N/A*    | 🟨 Probably | 🟨 Probably

[^1]: The following call conventions are supported: cdecl, stdcall, MS fastcall, thiscall.
[^2]: The prebuilt binary uses the hard float ABI and expects a VFP coprocessor. Build from source to use Koffi with a different ABI (softfp, soft).
[^3]: The prebuilt binary uses the LP64D (double-precision float) ABI. The LP64 ABI is supported in theory if you build Koffi from source but this is untested. The LP64F ABI is not supported.

Go to the web site for more information: https://koffi.dev/

# Project history

You can consult the [changelog](https://koffi.dev/changelog) on the official website.

Major version increments can include breaking API changes, use the [migration guide](https://koffi.dev/changelog#migration-guide) for more information.

# Build manually

Koffi is built with a custom CMake-wrapper called CNoke, which also lives in this repository. Don't try to run CMake manually because it will fail.

Follow the [documented build instructions](https://koffi.dev/contribute#build-from-source) to build Koffi from source.

# License

This program is free software: you can redistribute it and/or modify it under the terms of the **MIT License**.

Find more information here: https://choosealicense.com/licenses/mit/
