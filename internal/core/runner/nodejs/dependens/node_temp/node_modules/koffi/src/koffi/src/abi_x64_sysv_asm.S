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

#ifdef __APPLE__
    #define SYMBOL(Symbol) _ ## Symbol
#else
    #define SYMBOL(Symbol) Symbol
#endif

# Forward
# ----------------------------

# These five are the same, but they differ (in the C side) by their return type.
# Unlike the five next functions, these ones don't forward XMM argument registers.
.global SYMBOL(ForwardCallGG)
.global SYMBOL(ForwardCallF)
.global SYMBOL(ForwardCallDG)
.global SYMBOL(ForwardCallGD)
.global SYMBOL(ForwardCallDD)

# The X variants are slightly slower, and are used when XMM arguments must be forwarded.
.global SYMBOL(ForwardCallXGG)
.global SYMBOL(ForwardCallXF)
.global SYMBOL(ForwardCallXDG)
.global SYMBOL(ForwardCallXGD)
.global SYMBOL(ForwardCallXDD)

#define ENDBR64 .byte 0xf3, 0x0f, 0x1e, 0xfa

# Copy function pointer to R11, in order to save it through argument forwarding.
# Also make a copy of the SP to CallData::old_sp because the callback system might need it.
# Save RSP in RBX (non-volatile), and use carefully assembled stack provided by caller.
.macro prologue
    .cfi_startproc
    .cfi_def_cfa rsp, 8
    ENDBR64
    movq %rdi, %r11
    pushq %rbp
    .cfi_def_cfa rsp, 16
    movq %rsp, (%rdx)
    movq %rsp, %rbp
    .cfi_def_cfa rbp, 16
    leaq 112(%rsi), %rsp
.endm

# Call native function.
# Once done, restore normal stack pointer and return.
# The return value is passed untouched through RAX or XMM0.
.macro epilogue
    call *%r11
    movq %rbp, %rsp
    popq %rbp
    .cfi_def_cfa rsp, 8
    ret
    .cfi_endproc
.endm

# Prepare integer argument registers from array passed by caller.
.macro forward_gpr
    movq 40(%rsi), %r9
    movq 32(%rsi), %r8
    movq 24(%rsi), %rcx
    movq 16(%rsi), %rdx
    movq 0(%rsi), %rdi
    movq 8(%rsi), %rsi
.endm

# Prepare XMM argument registers from array passed by caller.
.macro forward_xmm
    movsd 104(%rsi), %xmm7
    movsd 96(%rsi), %xmm6
    movsd 88(%rsi), %xmm5
    movsd 80(%rsi), %xmm4
    movsd 72(%rsi), %xmm3
    movsd 64(%rsi), %xmm2
    movsd 56(%rsi), %xmm1
    movsd 48(%rsi), %xmm0
.endm

SYMBOL(ForwardCallGG):
    prologue
    forward_gpr
    movb $0, %al
    epilogue

SYMBOL(ForwardCallF):
    prologue
    forward_gpr
    movb $0, %al
    epilogue

SYMBOL(ForwardCallDG):
    prologue
    forward_gpr
    movb $0, %al
    epilogue

SYMBOL(ForwardCallGD):
    prologue
    forward_gpr
    movb $0, %al
    epilogue

SYMBOL(ForwardCallDD):
    prologue
    forward_gpr
    movb $0, %al
    epilogue

SYMBOL(ForwardCallXGG):
    prologue
    forward_xmm
    forward_gpr
    movb $8, %al
    epilogue

SYMBOL(ForwardCallXF):
    prologue
    forward_xmm
    forward_gpr
    movb $8, %al
    epilogue

SYMBOL(ForwardCallXDG):
    prologue
    forward_xmm
    forward_gpr
    movb $8, %al
    epilogue

SYMBOL(ForwardCallXGD):
    prologue
    forward_xmm
    forward_gpr
    movb $8, %al
    epilogue

SYMBOL(ForwardCallXDD):
    prologue
    forward_xmm
    forward_gpr
    movb $8, %al
    epilogue

# Callbacks
# ----------------------------

.global SYMBOL(RelayCallback)
.global SYMBOL(CallSwitchStack)

# First, make a copy of the GPR argument registers (rdi, rsi, rdx, rcx, r8, r9).
# Then call the C function RelayCallback with the following arguments:
# static trampoline ID, a pointer to the saved GPR array, a pointer to the stack
# arguments of this call, and a pointer to a struct that will contain the result registers.
# After the call, simply load these registers from the output struct.
.macro trampoline id
    .cfi_startproc
    .cfi_def_cfa rsp, 8
    ENDBR64
    subq $152, %rsp
    .cfi_def_cfa rsp, 160
    movq %rdi, 0(%rsp)
    movq %rsi, 8(%rsp)
    movq %rdx, 16(%rsp)
    movq %rcx, 24(%rsp)
    movq %r8, 32(%rsp)
    movq %r9, 40(%rsp)
    movq $\id, %rdi
    movq %rsp, %rsi
    leaq 160(%rsp), %rdx
    leaq 112(%rsp), %rcx
#ifdef __linux__
    call *RelayCallback@GOTPCREL(%rip)
#else
    call SYMBOL(RelayCallback)
#endif
    movq 112(%rsp), %rax
    movq 120(%rsp), %rdx
    addq $152, %rsp
    .cfi_def_cfa rsp, 8
    ret
    .cfi_endproc
.endm

# Same thing, but also forward the XMM argument registers and load the XMM result registers.
.macro trampoline_vec id
    .cfi_startproc
    .cfi_def_cfa rsp, 8
    ENDBR64
    subq $152, %rsp
    .cfi_def_cfa rsp, 160
    movq %rdi, 0(%rsp)
    movq %rsi, 8(%rsp)
    movq %rdx, 16(%rsp)
    movq %rcx, 24(%rsp)
    movq %r8, 32(%rsp)
    movq %r9, 40(%rsp)
    movsd %xmm0, 48(%rsp)
    movsd %xmm1, 56(%rsp)
    movsd %xmm2, 64(%rsp)
    movsd %xmm3, 72(%rsp)
    movsd %xmm4, 80(%rsp)
    movsd %xmm5, 88(%rsp)
    movsd %xmm6, 96(%rsp)
    movsd %xmm7, 104(%rsp)
    movq $\id, %rdi
    movq %rsp, %rsi
    leaq 160(%rsp), %rdx
    leaq 112(%rsp), %rcx
#ifdef __linux__
    call *RelayCallback@GOTPCREL(%rip)
#else
    call SYMBOL(RelayCallback)
#endif
    movq 112(%rsp), %rax
    movq 120(%rsp), %rdx
    movsd 128(%rsp), %xmm0
    movsd 136(%rsp), %xmm1
    addq $152, %rsp
    .cfi_def_cfa rsp, 8
    ret
    .cfi_endproc
.endm

# When a callback is relayed, Koffi will call into Node.js and V8 to execute Javascript.
# The problem is that we're still running on the separate Koffi stack, and V8 will
# probably misdetect this as a "stack overflow". We have to restore the old
# stack pointer, call Node.js/V8 and go back to ours.
# The first three parameters (rdi, rsi, rdx) are passed through untouched.
SYMBOL(CallSwitchStack):
    .cfi_startproc
    .cfi_def_cfa rsp, 8
    ENDBR64
    push %rbp
    .cfi_def_cfa rsp, 16
    movq %rsp, %rbp
    movq %rsp, %r10
    subq 0(%r8), %r10
    andq $-16, %r10
    movq %r10, 8(%r8)
    movq %rcx, %rsp
    .cfi_def_cfa rsp, 16
    call *%r9
    mov %rbp, %rsp
    .cfi_def_cfa rsp, 16
    pop %rbp
    .cfi_def_cfa rsp, 8
    ret
    .cfi_endproc

# Trampolines
# ----------------------------

#include "trampolines/gnu.inc"
