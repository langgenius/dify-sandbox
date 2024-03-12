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

.syntax unified

#define SYMBOL(Symbol) Symbol

# Forward
# ----------------------------

# These three are the same, but they differ (in the C side) by their return type.
# Unlike the three next functions, these ones don't forward XMM argument registers.
.global ForwardCallGG
.global ForwardCallF
.global ForwardCallDDDD

# The X variants are slightly slower, and are used when XMM arguments must be forwarded.
.global ForwardCallXGG
.global ForwardCallXF
.global ForwardCallXDDDD

# Copy function pointer to r12, in order to save it through argument forwarding.
# Also make a copy of the SP to CallData::old_sp because the callback system might need it.
# Save RSP in fp (non-volatile), and use carefully assembled stack provided by caller.
.macro prologue
    .cfi_startproc
    push {fp, lr}
    .cfi_def_cfa sp, 8
    .cfi_offset 11, 4
    .cfi_offset 14, 8
    mov fp, sp
    .cfi_def_cfa fp, 8
    str fp, [r2, 0]
    mov r12, r0
    mov sp, r1
    add sp, sp, #80
.endm

# Call native function.
# Once done, restore normal stack pointer and return.
# The return value is passed untouched through r0, r1, and or FP registers
.macro epilogue
    blx r12
    mov sp, fp
    .cfi_def_cfa sp, 8
    pop {fp, lr}
    .cfi_def_cfa sp, 0
    .cfi_restore 11
    .cfi_restore 14
    bx lr
    .cfi_endproc
.endm

ForwardCallGG:
    prologue
    add r1, r1, #64
    ldmia r1, {r0-r3}
    epilogue

ForwardCallF:
    prologue
    add r1, r1, #64
    ldmia r1, {r0-r3}
    epilogue

ForwardCallDDDD:
    prologue
    add r1, r1, #64
    ldmia r1, {r0-r3}
    epilogue

ForwardCallXGG:
    prologue
    vldmia r1!, {d0-d7}
    ldmia r1, {r0-r3}
    epilogue

ForwardCallXF:
    prologue
    vldmia r1!, {d0-d7}
    ldmia r1, {r0-r3}
    epilogue

ForwardCallXDDDD:
    prologue
    vldmia r1!, {d0-d7}
    ldmia r1, {r0-r3}
    epilogue

# Callbacks
# ----------------------------

.global RelayCallback
.global CallSwitchStack

# First, make a copy of the GPR argument registers (r0 to r7).
# Then call the C function RelayCallback with the following arguments:
# static trampoline ID, a pointer to the saved GPR array, a pointer to the stack
# arguments of this call, and a pointer to a struct that will contain the result registers.
# After the call, simply load these registers from the output struct.
.macro trampoline id
    .cfi_startproc
    push {fp, lr}
    .cfi_def_cfa sp, 8
    .cfi_offset 11, 4
    .cfi_offset 14, 8
    sub sp, sp, #120
    .cfi_def_cfa sp, 128
    add r12, sp, 64
    stmia r12!, {r0-r3}
    mov r0, \id
    mov r1, sp
    add r2, sp, #128
    mov r3, r12
    bl RelayCallback
    add sp, sp, #80
    ldmia sp!, {r0-r1}
    add sp, sp, #32
    .cfi_def_cfa sp, 8
    pop {fp, lr}
    .cfi_def_cfa sp, 0
    .cfi_restore 11
    .cfi_restore 14
    bx lr
    .cfi_endproc
.endm

# Same thing, but also forwards the floating-point argument registers and loads them at the end.
.macro trampoline_vec id
    .cfi_startproc
    push {fp, lr}
    .cfi_def_cfa sp, 8
    .cfi_offset 11, 4
    .cfi_offset 14, 8
    sub sp, sp, #120
    .cfi_def_cfa sp, 128
    mov r12, sp
    vstmia r12!, {d0-d7}
    stmia r12!, {r0-r3}
    mov r0, \id
    mov r1, sp
    add r2, sp, #128
    mov r3, r12
    bl RelayCallback
    add sp, sp, #80
    ldmia sp!, {r0-r1}
    vldmia sp!, {d0-d3}
    .cfi_def_cfa sp, 8
    pop {fp, lr}
    .cfi_def_cfa sp, 0
    .cfi_restore 11
    .cfi_restore 14
    bx lr
    .cfi_endproc
.endm

# When a callback is relayed, Koffi will call into Node.js and V8 to execute Javascript.
# The problem is that we're still running on the separate Koffi stack, and V8 will
# probably misdetect this as a "stack overflow". We have to restore the old
# stack pointer, call Node.js/V8 and go back to ours.
# The first three parameters (r0, r1, r2) are passed through untouched.
CallSwitchStack:
    .cfi_startproc
    push {fp, lr}
    .cfi_def_cfa sp, 8
    push {r4, r5}
    .cfi_def_cfa sp, 16
    .cfi_offset 11, 4
    .cfi_offset 14, 8
    mov fp, sp
    ldr r4, [sp, 16]
    ldr r5, [r4, 0]
    sub r5, sp, r5
    and r5, r5, #-16
    str r5, [r4, 4]
    ldr r4, [sp, 20]
    mov sp, r3
    blx r4
    mov sp, fp
    .cfi_def_cfa sp, 16
    pop {r4, r5}
    .cfi_def_cfa sp, 8
    pop {fp, lr}
    .cfi_def_cfa sp, 0
    .cfi_restore 11
    .cfi_restore 14
    bx lr
    .cfi_endproc

# Trampolines
# ----------------------------

#include "trampolines/gnu.inc"
