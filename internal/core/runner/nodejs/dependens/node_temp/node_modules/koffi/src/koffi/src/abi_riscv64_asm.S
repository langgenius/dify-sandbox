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

#define SYMBOL(Symbol) Symbol

# Forward
# ----------------------------

# These three are the same, but they differ (in the C side) by their return type.
# Unlike the three next functions, these ones don't forward FA argument registers.
.global ForwardCallGG
.global ForwardCallF
.global ForwardCallDG
.global ForwardCallGD
.global ForwardCallDD

# The X variants are slightly slower, and are used when FA arguments must be forwarded.
.global ForwardCallXGG
.global ForwardCallXF
.global ForwardCallXDG
.global ForwardCallXGD
.global ForwardCallXDD

# Copy function pointer to t0, in order to save it through argument forwarding.
# Also make a copy of the SP to CallData::old_sp because the callback system might need it.
# Save SP in s1, and use carefully assembled stack provided by caller.
.macro prologue
    addi sp, sp, -16
    mv t0, a0
    sd ra, 0(sp)
    sd s1, 8(sp)
    mv s1, sp
    sd sp, 0(a2)
    addi sp, a1, 128
.endm

# Call native function.
# Once done, restore normal stack pointer and return.
# The return value is passed untouched through registers.
.macro epilogue
    jalr t0
    mv sp, s1
    ld ra, 0(sp)
    ld s1, 8(sp)
    addi sp, sp, 16
    ret
.endm

# Prepare general purpose argument registers from array passed by caller.
.macro forward_gpr
    ld a7, 120(a1)
    ld a6, 112(a1)
    ld a5, 104(a1)
    ld a4, 96(a1)
    ld a3, 88(a1)
    ld a2, 80(a1)
    ld a0, 64(a1)
    ld a1, 72(a1)
.endm

# Prepare vector argument registers from array passed by caller.
.macro forward_vec
    fld fa7, 56(a1)
    fld fa6, 48(a1)
    fld fa5, 40(a1)
    fld fa4, 32(a1)
    fld fa3, 24(a1)
    fld fa2, 16(a1)
    fld fa1, 8(a1)
    fld fa0, 0(a1)
.endm

ForwardCallGG:
    prologue
    forward_gpr
    epilogue

ForwardCallF:
    prologue
    forward_gpr
    epilogue

ForwardCallDG:
    prologue
    forward_gpr
    epilogue

ForwardCallGD:
    prologue
    forward_gpr
    epilogue

ForwardCallDD:
    prologue
    forward_gpr
    epilogue

ForwardCallXGG:
    prologue
    forward_vec
    forward_gpr
    epilogue

ForwardCallXF:
    prologue
    forward_vec
    forward_gpr
    epilogue

ForwardCallXDG:
    prologue
    forward_vec
    forward_gpr
    epilogue

ForwardCallXGD:
    prologue
    forward_vec
    forward_gpr
    epilogue

ForwardCallXDD:
    prologue
    forward_vec
    forward_gpr
    epilogue

# Callbacks
# ----------------------------

.global RelayCallback
.global CallSwitchStack

# First, make a copy of the GPR argument registers (a0 to a7).
# Then call the C function RelayCallback with the following arguments:
# static trampoline ID, a pointer to the saved GPR array, a pointer to the stack
# arguments of this call, and a pointer to a struct that will contain the result registers.
# After the call, simply load these registers from the output struct.
.macro trampoline id
    addi sp, sp, -176
    sd ra, 0(sp)
    sd a0, 8(sp)
    sd a1, 16(sp)
    sd a2, 24(sp)
    sd a3, 32(sp)
    sd a4, 40(sp)
    sd a5, 48(sp)
    sd a6, 56(sp)
    sd a7, 64(sp)
    li a0, \id
    addi a1, sp, 8
    addi a2, sp, 176
    addi a3, sp, 136
    call RelayCallback
    ld ra, 0(sp)
    ld a0, 136(sp)
    ld a1, 144(sp)
    addi sp, sp, 176
    ret
.endm

# Same thing, but also forwards the floating-point argument registers and loads them at the end.
.macro trampoline_vec id
    addi sp, sp, -176
    sd ra, 0(sp)
    sd a0, 8(sp)
    sd a1, 16(sp)
    sd a2, 24(sp)
    sd a3, 32(sp)
    sd a4, 40(sp)
    sd a5, 48(sp)
    sd a6, 56(sp)
    sd a7, 64(sp)
    fsd fa0, 72(sp)
    fsd fa1, 80(sp)
    fsd fa2, 88(sp)
    fsd fa3, 96(sp)
    fsd fa4, 104(sp)
    fsd fa5, 112(sp)
    fsd fa6, 120(sp)
    fsd fa7, 128(sp)
    li a0, \id
    addi a1, sp, 8
    addi a2, sp, 176
    addi a3, sp, 136
    call RelayCallback
    ld ra, 0(sp)
    ld a0, 136(sp)
    ld a1, 144(sp)
    fld fa0, 152(sp)
    fld fa1, 160(sp)
    addi sp, sp, 176
    ret
.endm

# When a callback is relayed, Koffi will call into Node.js and V8 to execute Javascript.
# The problem is that we're still running on the separate Koffi stack, and V8 will
# probably misdetect this as a "stack overflow". We have to restore the old
# stack pointer, call Node.js/V8 and go back to ours.
# The first three parameters (a0, a1, a2) are passed through untouched.
CallSwitchStack:
    addi sp, sp, -16
    sd ra, 0(sp)
    sd s1, 8(sp)
    mv s1, sp
    ld t0, 0(a4)
    sub t0, sp, t0
    andi t0, t0, -16
    sd t0, 8(a4)
    mv sp, a3
    jalr a5
    mv sp, s1
    ld ra, 0(sp)
    ld s1, 8(sp)
    addi sp, sp, 16
    ret

# Trampolines
# ----------------------------

#include "trampolines/gnu.inc"
