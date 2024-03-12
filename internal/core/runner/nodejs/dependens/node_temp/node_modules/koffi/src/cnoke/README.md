# Table of contents

- [Introduction](#introduction)
- [Get started](#get-started)
- [Command usage](#command-usage)

# Introduction

CNoke is a simpler alternative to CMake.js, without any dependency, designed to build
native Node addons based on CMake.

Install it like this:

```sh
npm install cnoke
```

It obviously requires [CMake](http://www.cmake.org/download/) and a proper C/C++ toolchain:

* Windows: Visual C++ Build Tools or a recent version of Visual C++ will do (the free Community version works well)
* POSIX (Linux, macOS, etc.): Clang or GCC, and Make (Ninja is preferred if available)

# Get started

In order to build a native Node.js addon with CNoke, you can use the following CMakeLists.txt
template to get started:

```cmake
cmake_minimum_required(VERSION 3.11)
project(hello C CXX)

find_package(CNoke)

add_node_addon(NAME hello SOURCES hello.cc)
```

You can also do it manually (without the module) if you prefer:

```cmake
cmake_minimum_required(VERSION 3.11)
project(hello C CXX)

add_library(hello SHARED hello.cc ${NODE_JS_SOURCES})
set_target_properties(hello PROPERTIES PREFIX "" SUFFIX ".node")
target_include_directories(hello PRIVATE ${NODE_JS_INCLUDE_DIRS})
target_link_libraries(hello PRIVATE ${NODE_JS_LIBRARIES})
target_compile_options(hello PRIVATE ${NODE_JS_COMPILE_FLAGS})
target_link_options(hello PRIVATE ${NODE_JS_LINK_FLAGS})
```

In order for this to run when `npm install` runs (directly or when someone else installs
your dependency), add the following script to package.json:

```json
"scripts": {
    "install": "cnoke"
}
```

# Command usage

You can find the same help text by running `cnoke --help`:

```
Usage: cnoke [command] [options...] [targets...]

Commands:
    configure                            Configure CMake build
    build                                Build project (configure if needed)
    clean                                Clean build files

Options:
    -d, --directory <DIR>                Change project directory
                                         (default: current working directory)

    -B, --config <CONFIG>                Change build type: RelWithDebInfo, Debug, Release
                                         (default: RelWithDebInfo)
    -D, --debug                          Shortcut for --config Debug

        --prebuild <URL>                 Set URL template to download prebuilt binaries
        --require <PATH>                 Require specified module, drop prebuild if it fails

    -a, --arch <ARCH>                    Change architecture and ABI
                                         (default: x64)
    -v, --runtime-version <VERSION>      Change node version
                                         (default: v16.14.0)
    -t, --toolset <TOOLSET>              Change default CMake toolset
    -C, --prefer-clang                   Use Clang instead of default CMake compiler

        --verbose                        Show build commands while building
```

The ARCH value is similar to process.arch, with the following differences:

- arm is changed to arm32hf or arm32sf depending on the floating-point ABI used (hard-float, soft-float)
- riscv32 is changed to riscv32sf, riscv32hf32, riscv32hf64 or riscv32hf128 depending on the floating-point ABI
- riscv64 is changed to riscv64sf, riscv64hf32, riscv64hf64 or riscv64hf128 depending on the floating-point ABI
