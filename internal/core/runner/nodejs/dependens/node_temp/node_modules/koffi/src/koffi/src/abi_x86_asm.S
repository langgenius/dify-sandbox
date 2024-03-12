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

.global ForwardCallG
.global ForwardCallF
.global ForwardCallD
.global ForwardCallRG
.global ForwardCallRF
.global ForwardCallRD

#define ENDBR32 .byte 0xf3, 0x0f, 0x1e, 0xfb

# Copy function pointer to EAX, in order to save it through argument forwarding.
# Also make a copy of the SP to CallData::old_sp because the callback system might need it.
# Save ESP in EBX (non-volatile), and use carefully assembled stack provided by caller.
.macro prologue
    .cfi_startproc
    .cfi_def_cfa esp, 4
    ENDBR32
    push %ebp
    .cfi_def_cfa esp, 8
    movl %esp, %ebp
    .cfi_def_cfa ebp, 8
    movl 16(%esp), %eax
    movl %esp, 0(%eax)
    movl 8(%esp), %eax
    movl 12(%esp), %esp
.endm

.macro fastcall
    movl 0(%esp), %ecx
    movl 4(%esp), %edx
    addl $16, %esp
.endm

# Call native function.
# Once done, restore normal stack pointer and return.
# The return value is passed back untouched.
.macro epilogue
    call *%eax
    movl %ebp, %esp
    pop %ebp
    .cfi_def_cfa esp, 4
    ret
    .cfi_endproc
.endm

ForwardCallG:
    prologue
    epilogue

ForwardCallF:
    prologue
    epilogue

ForwardCallD:
    prologue
    epilogue

ForwardCallRG:
    prologue
    fastcall
    epilogue

ForwardCallRF:
    prologue
    fastcall
    epilogue

ForwardCallRD:
    prologue
    fastcall
    epilogue

# Callbacks
# ----------------------------

.global RelayCallback
.global CallSwitchStack

# Call the C function RelayCallback with the following arguments:
# static trampoline ID, the current stack pointer, a pointer to the stack arguments of this call,
# and a pointer to a struct that will contain the result registers.
# After the call, simply load these registers from the output struct.
# Depending on ABI, call convention and return value size, we need to issue ret <something>. Since ret
# only takes an immediate value, and I prefer not to branch, the return address is moved instead according
# to BackRegisters::ret_pop before ret is issued.
.macro trampoline id
    .cfi_startproc
    .cfi_def_cfa esp, 4
    ENDBR32
    sub $44, %esp
    .cfi_def_cfa esp, 48
    movl $\id, 0(%esp)
    movl %esp, 4(%esp)
    leal 48(%esp), %eax
    movl %eax, 8(%esp)
    leal 16(%esp), %eax
    movl %eax, 12(%esp)
    call GetEIP
    addl $_GLOBAL_OFFSET_TABLE_, %ecx
    call *RelayCallback@GOT(%ecx)
    movl 44(%esp), %edx
    movl 36(%esp), %ecx
    movl %edx, 44(%esp, %ecx)
    movl 16(%esp), %eax
    movl 20(%esp), %edx
    leal 44(%esp, %ecx), %esp
    .cfi_def_cfa esp, 4
    ret
    .cfi_endproc
.endm

# This version also loads the x87 stack with the result, if need be.
# We have to branch to avoid x87 stack imbalance.
.macro trampoline_vec id
    .cfi_startproc
    .cfi_def_cfa esp, 4
    ENDBR32
    sub $44, %esp
    .cfi_def_cfa esp, 48
    movl $\id, 0(%esp)
    movl %esp, 4(%esp)
    leal 48(%esp), %eax
    movl %eax, 8(%esp)
    leal 16(%esp), %eax
    movl %eax, 12(%esp)
    call GetEIP
    addl $_GLOBAL_OFFSET_TABLE_, %ecx
    call *RelayCallback@GOT(%ecx)
    movl 44(%esp), %edx
    movl 36(%esp), %ecx
    movl %edx, 44(%esp, %ecx, 4)
    cmpb $0, 32(%esp)
    jne 2f
1:
    flds 24(%esp)
    leal 44(%esp, %ecx), %esp
    .cfi_def_cfa esp, 4
    ret
2:
    fldl 24(%esp)
    leal 44(%esp, %ecx), %esp
    .cfi_def_cfa esp, 4
    ret
    .cfi_endproc
.endm

GetEIP:
    movl (%esp), %ecx
    ret

# When a callback is relayed, Koffi will call into Node.js and V8 to execute Javascript.
# The problem is that we're still running on the separate Koffi stack, and V8 will
# probably misdetect this as a "stack overflow". We have to restore the old
# stack pointer, call Node.js/V8 and go back to ours.
CallSwitchStack:
    .cfi_startproc
    .cfi_def_cfa esp, 4
    ENDBR32
    push %ebp
    .cfi_def_cfa esp, 8
    movl %esp, %ebp
    .cfi_def_cfa ebp, 8
    movl 28(%esp), %edx
    movl 24(%esp), %ecx
    movl %esp, %eax
    subl 0(%ecx), %eax
    andl $-16, %eax
    movl %eax, 4(%ecx)
    movl 20(%esp), %esp
    subl $28, %esp
    movl 8(%ebp), %eax
    movl %eax, 0(%esp)
    movl 12(%ebp), %eax
    movl %eax, 4(%esp)
    movl 16(%ebp), %eax
    movl %eax, 8(%esp)
    call *%edx
    mov %ebp, %esp
    .cfi_def_cfa esp, 8
    pop %ebp
    .cfi_def_cfa esp, 4
    ret
    .cfi_endproc

# Trampolines
# ----------------------------

#include "trampolines/gnu.inc"
