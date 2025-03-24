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

if(CMAKE_BUILD_TYPE STREQUAL "Release" OR CMAKE_BUILD_TYPE STREQUAL "RelWithDebInfo" OR
   CMAKE_BUILD_TYPE STREQUAL "MinSizeRel")
    set(USE_UNITY_BUILDS ON CACHE BOOL "Use single-TU builds (aka. Unity builds)")
else()
    set(USE_UNITY_BUILDS OFF CACHE BOOL "Use single-TU builds (aka. Unity builds)")
endif()

if(NODE_JS_LINK_DEF)
    add_custom_command(OUTPUT ${NODE_JS_LINK_LIB}
                       COMMAND ${CMAKE_AR} ${CMAKE_STATIC_LINKER_FLAGS}
                               /def:${NODE_JS_LINK_DEF} /out:${NODE_JS_LINK_LIB}
                       WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
                       MAIN_DEPENDENCY ${NODE_JS_LINK_DEF})
    add_custom_target(node.lib DEPENDS ${NODE_JS_LINK_LIB})
endif()

function(add_node_addon)
    cmake_parse_arguments(ARG "" "NAME" "SOURCES" ${ARGN})
    add_library(${ARG_NAME} SHARED ${ARG_SOURCES} ${NODE_JS_SOURCES})
    target_link_node(${ARG_NAME})
    set_target_properties(${ARG_NAME} PROPERTIES PREFIX "" SUFFIX ".node")
endfunction()

function(target_link_node TARGET)
    target_include_directories(${TARGET} PRIVATE ${NODE_JS_INCLUDE_DIRS})
    if(NODE_JS_LINK_LIB)
        if(TARGET node.lib)
            add_dependencies(${TARGET} node.lib)
        endif()
        target_link_libraries(${TARGET} PRIVATE ${NODE_JS_LINK_LIB})
    endif()
    target_compile_options(${TARGET} PRIVATE ${NODE_JS_COMPILE_FLAGS})
    if(NODE_JS_LINK_FLAGS)
        target_link_options(${TARGET} PRIVATE ${NODE_JS_LINK_FLAGS})
    endif()
endfunction()

if(USE_UNITY_BUILDS)
    function(enable_unity_build TARGET)
        get_target_property(sources ${TARGET} SOURCES)
        string(GENEX_STRIP "${sources}" sources)

        set(unity_file_c "${CMAKE_CURRENT_BINARY_DIR}/${TARGET}_unity.c")
        set(unity_file_cpp "${CMAKE_CURRENT_BINARY_DIR}/${TARGET}_unity.cpp")
        file(REMOVE ${unity_file_c} ${unity_file_cpp})

        set(c_definitions "")
        set(cpp_definitions "")

        foreach(src ${sources})
            get_source_file_property(language ${src} LANGUAGE)
            get_property(definitions SOURCE ${src} PROPERTY COMPILE_DEFINITIONS)
            if(IS_ABSOLUTE ${src})
                set(src_full ${src})
            else()
                set(src_full "${CMAKE_CURRENT_SOURCE_DIR}/${src}")
            endif()
            if(language STREQUAL "C")
                set_source_files_properties(${src} PROPERTIES HEADER_FILE_ONLY 1)
                file(APPEND ${unity_file_c} "#include \"${src_full}\"\n")
                if (definitions)
                    set(c_definitions "${c_definitions} ${definitions}")
                endif()
            elseif(language STREQUAL "CXX")
                set_source_files_properties(${src} PROPERTIES HEADER_FILE_ONLY 1)
                file(APPEND ${unity_file_cpp} "#include \"${src_full}\"\n")
                if (definitions)
                    set(cpp_definitions "${cpp_definitions} ${definitions}")
                endif()
            endif()
        endforeach()

        if(EXISTS ${unity_file_c})
            target_sources(${TARGET} PRIVATE ${unity_file_c})
            if(c_definitions)
                set_source_files_properties(${unity_file_c} PROPERTIES COMPILE_DEFINITIONS ${c_definitions})
            endif()
        endif()
        if(EXISTS ${unity_file_cpp})
            target_sources(${TARGET} PRIVATE ${unity_file_cpp})
            if(cpp_definitions)
                set_source_files_properties(${unity_file_cpp} PROPERTIES COMPILE_DEFINITIONS ${cpp_definitions})
            endif()
        endif()

        target_compile_definitions(${TARGET} PRIVATE -DUNITY_BUILD=1)
    endfunction()
else()
    function(enable_unity_build TARGET)
    endfunction()
endif()
