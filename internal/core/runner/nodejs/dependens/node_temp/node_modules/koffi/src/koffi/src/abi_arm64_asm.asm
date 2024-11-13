; Copyright 2023 Niels Martignène <niels.martignene@protonmail.com>
;
; Permission is hereby granted, free of charge, to any person obtaining a copy of
; this software and associated documentation files (the “Software”), to deal in 
; the Software without restriction, including without limitation the rights to use,
; copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the
; Software, and to permit persons to whom the Software is furnished to do so,
; subject to the following conditions:
;
; The above copyright notice and this permission notice shall be included in all
; copies or substantial portions of the Software.
;
; THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND,
; EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
; OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
; NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
; HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
; WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
; FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
; OTHER DEALINGS IN THE SOFTWARE.

    AREA |.text|, CODE

; Forward
; ----------------------------

    ; These three are the same, but they differ (in the C side) by their return type.
    ; Unlike the three next functions, these ones don't forward XMM argument registers.
    EXPORT ForwardCallGG
    EXPORT ForwardCallF
    EXPORT ForwardCallDDDD

    ; The X variants are slightly slower, and are used when XMM arguments must be forwarded.
    EXPORT ForwardCallXGG
    EXPORT ForwardCallXF
    EXPORT ForwardCallXDDDD

    ; Copy function pointer to r9, in order to save it through argument forwarding.
    ; Save RSP in r29 (non-volatile), and use carefully assembled stack provided by caller.
    MACRO
    prologue

    stp x29, x30, [sp, -16]!
    mov x29, sp
    str x29, [x2, 0]
    mov x9, x0
    add sp, x1, #136
    MEND

    ; Call native function.
    ; Once done, restore normal stack pointer and return.
    ; The return value is passed untouched through r0, r1, v0 and/or v1.
    MACRO
    epilogue

    blr x9
    mov sp, x29
    ldp x29, x30, [sp], 16
    ret
    MEND

    ; Prepare general purpose argument registers from array passed by caller.
    MACRO
    forward_gpr

    ldr x8, [x1, 64]
    ldp x6, x7, [x1, 48]
    ldp x4, x5, [x1, 32]
    ldp x2, x3, [x1, 16]
    ldp x0, x1, [x1, 0]
    MEND

    ; Prepare vector argument registers from array passed by caller.
    MACRO
    forward_vec

    ldp d6, d7, [x1, 120]
    ldp d4, d5, [x1, 104]
    ldp d2, d3, [x1, 88]
    ldp d0, d1, [x1, 72]
    MEND

ForwardCallGG PROC
    prologue
    forward_gpr
    epilogue
    ENDP

ForwardCallF PROC
    prologue
    forward_gpr
    epilogue
    ENDP

ForwardCallDDDD PROC
    prologue
    forward_gpr
    epilogue
    ENDP

ForwardCallXGG PROC
    prologue
    forward_vec
    forward_gpr
    epilogue
    ENDP

ForwardCallXF PROC
    prologue
    forward_vec
    forward_gpr
    epilogue
    ENDP

ForwardCallXDDDD PROC
    prologue
    forward_vec
    forward_gpr
    epilogue
    ENDP

; Callbacks
; ----------------------------

    EXPORT RelayCallback
    EXTERN RelayCallback
    EXPORT CallSwitchStack

    ; First, make a copy of the GPR argument registers (x0 to x7).
    ; Then call the C function RelayCallback with the following arguments:
    ; static trampoline ID, a pointer to the saved GPR array, a pointer to the stack
    ; arguments of this call, and a pointer to a struct that will contain the result registers.
    ; After the call, simply load these registers from the output struct.
    MACRO
    trampoline $ID

    stp x29, x30, [sp, -16]!
    sub sp, sp, #192
    stp x0, x1, [sp, 0]
    stp x2, x3, [sp, 16]
    stp x4, x5, [sp, 32]
    stp x6, x7, [sp, 48]
    str x8, [sp, 64]
    mov x0, $ID
    mov x1, sp
    add x2, sp, #208
    add x3, sp, #136
    bl RelayCallback
    ldp x0, x1, [sp, 136]
    add sp, sp, #192
    ldp x29, x30, [sp], 16
    ret
    MEND

    ; Same thing, but also forwards the floating-point argument registers and loads them at the end.
    MACRO
    trampoline_vec $ID

    stp x29, x30, [sp, -16]!
    sub sp, sp, #192
    stp x0, x1, [sp, 0]
    stp x2, x3, [sp, 16]
    stp x4, x5, [sp, 32]
    stp x6, x7, [sp, 48]
    str x8, [sp, 64]
    stp d0, d1, [sp, 72]
    stp d2, d3, [sp, 88]
    stp d4, d5, [sp, 104]
    stp d6, d7, [sp, 120]
    mov x0, $ID
    mov x1, sp
    add x2, sp, #208
    add x3, sp, #136
    bl RelayCallback
    ldp x0, x1, [sp, 136]
    ldp d0, d1, [sp, 152]
    ldp d2, d3, [sp, 168]
    add sp, sp, #192
    ldp x29, x30, [sp], 16
    ret
    MEND

; When a callback is relayed, Koffi will call into Node.js and V8 to execute Javascript.
; The problem is that we're still running on the separate Koffi stack, and V8 will
; probably misdetect this as a "stack overflow". We have to restore the old
; stack pointer, call Node.js/V8 and go back to ours.
; The first three parameters (x0, x1, x2) are passed through untouched.
CallSwitchStack PROC
    stp x29, x30, [sp, -16]!
    mov x29, sp
    ldr x9, [x4, 0]
    sub x9, sp, x9
    and x9, x9, #-16
    str x9, [x4, 8]
    mov sp, x3
    blr x5
    mov sp, x29
    ldp x29, x30, [sp], 16
    ret
    ENDP

; Trampolines
; ----------------------------

    INCLUDE trampolines/armasm.inc

    END
