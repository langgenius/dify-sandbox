# Copyright 2023 Niels Martignène <niels.martignene@protonmail.com>
#
# Permission is hereby granted, free of charge, to any person obtaining a copy of
# this software and associated documentation files (the “Software”), to deal in 
# the Software without restriction, including without limitation the rights to use,
# copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the
# Software, and to permit persons to whom the Software is furnished to do so,
# subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND,
# EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
# OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
# NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
# HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
# WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
# FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
# OTHER DEALINGS IN THE SOFTWARE.

cmake_minimum_required(VERSION 3.6)
cmake_policy(SET CMP0091 NEW)

if(NOT NODE_JS_INCLUDE_DIRS)
    message(FATAL_ERROR "Please use CNoke to build Koffi, follow instructions here: https://koffi.dev/contribute#build-from-source")
endif()

project(koffi C CXX ASM)
find_package(CNoke)

include(CheckCXXCompilerFlag)

set(CMAKE_CXX_STANDARD 20)
if(MSVC)
    set(CMAKE_MSVC_RUNTIME_LIBRARY MultiThreaded)
    set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} /Zc:__cplusplus /Zc:preprocessor /Zc:twoPhase- /W4 /wd4200 /wd4201 /wd4127 /wd4458 /wd4706 /wd4702 /wd4324")

    # ASM_MASM does not (yet) work on Windows ARM64
    if(NOT CMAKE_GENERATOR_PLATFORM MATCHES "ARM64")
        enable_language(ASM_MASM)
    endif()
else()
    set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -Wall -Wextra -Wno-missing-field-initializers -Wno-unused-parameter -Wswitch -Werror=switch")

    if(CMAKE_CXX_COMPILER_ID MATCHES "Clang")
        set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -Wno-unknown-warning-option")
    endif()
endif()

if(UNIX AND NOT APPLE)
    set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -D_FILE_OFFSET_BITS=64")
    set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -D_FILE_OFFSET_BITS=64")
    set(CMAKE_SHARED_LINKER_FLAGS "${CMAKE_SHARED_LINKER_FLAGS} -z noexecstack")
endif()

# ---- Koffi ----

# Recompute the version string after each commit
if(EXISTS "${CMAKE_SOURCE_DIR}/../../.git/logs/HEAD")
    configure_file("${CMAKE_SOURCE_DIR}/../../.git/logs/HEAD" git_logs_HEAD COPYONLY)
endif()
if(EXISTS ${CMAKE_CURRENT_SOURCE_DIR}/package.json)
    file(READ ${CMAKE_CURRENT_SOURCE_DIR}/package.json PKG)
else()
    file(READ ${CMAKE_CURRENT_SOURCE_DIR}/../../package.json PKG)
endif()
string(REGEX MATCH "\"version\": \"([^\"]+)\"" XX "${PKG}")
set(KOFFI_VERSION ${CMAKE_MATCH_1})

set_source_files_properties(src/ffi.cc PROPERTIES COMPILE_DEFINITIONS VERSION=${KOFFI_VERSION})

set(KOFFI_SRC
    src/call.cc
    src/ffi.cc
    src/parser.cc
    src/util.cc
    src/win32.cc
    ../../src/core/libcc/libcc.cc
)
if(CMAKE_SIZEOF_VOID_P EQUAL 8)
    # CMAKE_SYSTEM_PROCESSOR is wrong on Windows ARM64

    if(CMAKE_SYSTEM_PROCESSOR MATCHES "aarch|arm" OR CMAKE_GENERATOR_PLATFORM STREQUAL "ARM64" OR CMAKE_OSX_ARCHITECTURES MATCHES "arm")
        if(WIN32)
            get_filename_component(cl_dir "${CMAKE_CXX_COMPILER}" DIRECTORY)
            file(TO_CMAKE_PATH "${cl_dir}/armasm64.exe" asm_compiler)

            # Work around missing ARM64-native ARMASM64 compiler (at least in VS 17.3 Preview 2)
            if(NOT EXISTS "${asm_compiler}")
                file(TO_CMAKE_PATH "${cl_dir}/../../Hostx64/arm64/armasm64.exe" asm_compiler)
            endif()

            message(STATUS "Using ARMASM64 compiler: ${asm_compiler}")

            file(TO_CMAKE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/src/abi_arm64_asm.asm" asm_source)
            file(TO_CMAKE_PATH "${CMAKE_CURRENT_BINARY_DIR}/abi_arm64_asm.obj" asm_object)

            add_custom_command(
                OUTPUT "${asm_object}"
                COMMAND "${asm_compiler}" ARGS /nologo /o "${asm_object}" "${asm_source}"
                DEPENDS "${asm_source}"
                COMMENT "Assembling ${asm_source}"
            )
            set_source_files_properties("${asm_object}" PROPERTIES EXTERNAL_OBJECT TRUE)

            list(APPEND KOFFI_SRC src/abi_arm64.cc "${asm_object}")
        else()
            list(APPEND KOFFI_SRC src/abi_arm64.cc src/abi_arm64_asm.S)
        endif()
    elseif(CMAKE_SYSTEM_PROCESSOR MATCHES "riscv")
        list(APPEND KOFFI_SRC src/abi_riscv64.cc src/abi_riscv64_asm.S)
    else()
        if(WIN32)
            list(APPEND KOFFI_SRC src/abi_x64_win.cc src/abi_x64_win_asm.asm)
        else()
            list(APPEND KOFFI_SRC src/abi_x64_sysv.cc src/abi_x64_sysv_asm.S)
        endif()
    endif()
elseif(CMAKE_SIZEOF_VOID_P EQUAL 4)
    if(CMAKE_SYSTEM_PROCESSOR MATCHES "aarch|arm")
        list(APPEND KOFFI_SRC src/abi_arm32.cc src/abi_arm32_asm.S)
    else()
        if(WIN32)
            list(APPEND KOFFI_SRC src/abi_x86.cc src/abi_x86_asm.asm)
        else()
            list(APPEND KOFFI_SRC src/abi_x86.cc src/abi_x86_asm.S)
        endif()
    endif()
endif()

add_node_addon(NAME koffi SOURCES ${KOFFI_SRC})
target_include_directories(koffi PRIVATE . ../.. ../../vendor/node-addon-api)

target_compile_definitions(koffi PRIVATE FELIX_TARGET=koffi NAPI_DISABLE_CPP_EXCEPTIONS NAPI_VERSION=8 LIBCC_NO_STATX)
if(WIN32)
    target_compile_definitions(koffi PRIVATE _CRT_SECURE_NO_WARNINGS _CRT_NONSTDC_NO_DEPRECATE)
    target_link_libraries(koffi PRIVATE ws2_32)
endif()
if(NOT MSVC OR CMAKE_C_COMPILER_ID MATCHES "[Cc]lang")
    # Restore C/C++ compiler sanity

    if(NOT MSVC)
        target_compile_options(koffi PRIVATE $<$<COMPILE_LANGUAGE:CXX>:-fno-exceptions -fno-strict-aliasing -fwrapv
                                                                       -fno-delete-null-pointer-checks>)
    else()
        target_compile_options(koffi PRIVATE $<$<COMPILE_LANGUAGE:CXX>:-fno-strict-aliasing /clang:-fwrapv
                                                                       -fno-delete-null-pointer-checks>)
    endif()

    check_cxx_compiler_flag(-fno-finite-loops use_no_finite_loops)
    if(use_no_finite_loops)
        target_compile_options(koffi PRIVATE $<$<COMPILE_LANGUAGE:CXX>:-fno-finite-loops>)
    endif()
endif()
enable_unity_build(koffi)
