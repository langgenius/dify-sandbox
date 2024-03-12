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

add_library(raylib SHARED
    ../../../vendor/raylib/src/rcore.c
    ../../../vendor/raylib/src/rshapes.c
    ../../../vendor/raylib/src/rtextures.c
    ../../../vendor/raylib/src/rtext.c
    ../../../vendor/raylib/src/rmodels.c
    ../../../vendor/raylib/src/utils.c
    ../../../vendor/raylib/src/rglfw.c
    ../../../vendor/raylib/src/raudio.c
)
set_target_properties(raylib PROPERTIES PREFIX "")
target_include_directories(raylib PRIVATE ../../../vendor/raylib/src/external/glfw/include)
target_compile_definitions(raylib PRIVATE PLATFORM_DESKTOP GRAPHICS_API_OPENGL_21 BUILD_LIBTYPE_SHARED NDEBUG)

if(MSVC)
    target_compile_options(raylib PRIVATE /wd4244 /wd4305)
else()
    target_compile_options(raylib PRIVATE -Wno-sign-compare -Wno-old-style-declaration
                                          -Wno-unused-function -Wno-missing-field-initializers
                                          -Wno-unused-value -Wno-implicit-fallthrough -Wno-stringop-overflow
                                          -Wno-unused-result)
    if(CMAKE_CXX_COMPILER_ID MATCHES "Clang")
        target_compile_options(raylib PRIVATE -Wno-unknown-warning-option)
    endif()
endif()

if(WIN32)
    target_compile_definitions(raylib PRIVATE _CRT_SECURE_NO_WARNINGS _CRT_NONSTDC_NO_DEPRECATE)
    target_link_libraries(raylib PRIVATE winmm)
endif()

if(CMAKE_SYSTEM_NAME MATCHES "BSD")
    target_include_directories(raylib PRIVATE /usr/local/include)
endif()

if(APPLE)
    target_compile_options(raylib PRIVATE -Wno-unknown-warning-option -Wno-macro-redefined)
    target_compile_definitions(raylib PRIVATE GL_SILENCE_DEPRECATION)
    set_source_files_properties(../../../vendor/raylib/src/rglfw.c PROPERTIES COMPILE_FLAGS "-x objective-c")
    target_link_libraries(raylib PRIVATE "-framework Cocoa" "-framework IOKit" "-framework CoreFoundation" "-framework OpenGL")
endif()

if(UNIX AND NOT APPLE)
    set(missing_xlib "")

    find_path(XLIB_INCLUDE_DIRS X11/Xlib.h)
    if(NOT XLIB_INCLUDE_DIRS)
        list(APPEND missing_xlib Xlib)
    endif()
    find_path(XCURSOR_INCLUDE_DIRS X11/Xcursor/Xcursor.h)
    if(NOT XCURSOR_INCLUDE_DIRS)
        list(APPEND missing_xlib Xcursor)
    endif()
    find_path(XRANDR_INCLUDE_DIRS X11/extensions/Xrandr.h)
    if(NOT XRANDR_INCLUDE_DIRS)
        list(APPEND missing_xlib Xrandr)
    endif()
    find_path(XKB_INCLUDE_DIRS X11/XKBlib.h)
    if(NOT XKB_INCLUDE_DIRS)
        list(APPEND missing_xlib Xkbcommon)
    endif()
    find_path(XINERAMA_INCLUDE_DIRS X11/extensions/Xinerama.h)
    if(NOT XINERAMA_INCLUDE_DIRS)
        list(APPEND missing_xlib Xinerama)
    endif()
    find_path(XINPUT_INCLUDE_DIRS X11/extensions/XInput2.h)
    if(NOT XINPUT_INCLUDE_DIRS)
        list(APPEND missing_xlib XInput2)
    endif()

    if(missing_xlib)
        list(JOIN missing_xlib ", " missing_xlib_str)
        message(FATAL_ERROR "Missing X11 development files: ${missing_xlib_str}")
    endif()

    target_include_directories(raylib PRIVATE ${XLIB_INCLUDE_DIRS})
endif()
