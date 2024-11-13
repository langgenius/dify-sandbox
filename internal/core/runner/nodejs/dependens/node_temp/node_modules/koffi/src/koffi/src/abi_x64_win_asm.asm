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

; Forward
; ----------------------------

; These three are the same, but they differ (in the C side) by their return type.
; Unlike the three next functions, these ones don't forward XMM argument registers.
public ForwardCallG
public ForwardCallF
public ForwardCallD

; The X variants are slightly slower, and are used when XMM arguments must be forwarded.
public ForwardCallXG
public ForwardCallXF
public ForwardCallXD

.code

; Copy function pointer to RAX, in order to save it through argument forwarding.
; Also make a copy of the SP to CallData::old_sp because the callback system might need it.
; Save RSP in RBX (non-volatile), and use carefully assembled stack provided by caller.
prologue macro
    endbr64
    mov rax, rcx
    push rbp
    .pushreg rbp
    mov rbp, rsp
    mov qword ptr [r8+0], rsp
    .setframe rbp, 0
    .endprolog
    mov rsp, rdx
endm

; Call native function.
; Once done, restore normal stack pointer and return.
; The return value is passed untouched through RAX or XMM0.
epilogue macro
    call rax
    mov rsp, rbp
    pop rbp
    ret
endm

; Prepare integer argument registers from array passed by caller.
forward_gpr macro
    mov r9, qword ptr [rdx+24]
    mov r8, qword ptr [rdx+16]
    mov rcx, qword ptr [rdx+0]
    mov rdx, qword ptr [rdx+8]
endm

; Prepare XMM argument registers from array passed by caller.
forward_xmm macro
    movsd xmm3, qword ptr [rdx+24]
    movsd xmm2, qword ptr [rdx+16]
    movsd xmm1, qword ptr [rdx+8]
    movsd xmm0, qword ptr [rdx+0]
endm

ForwardCallG proc frame
    prologue
    forward_gpr
    epilogue
ForwardCallG endp

ForwardCallF proc frame
    prologue
    forward_gpr
    epilogue
ForwardCallF endp

ForwardCallD proc frame
    prologue
    forward_gpr
    epilogue
ForwardCallD endp

ForwardCallXG proc frame
    prologue
    forward_xmm
    forward_gpr
    epilogue
ForwardCallXG endp

ForwardCallXF proc frame
    prologue
    forward_xmm
    forward_gpr
    epilogue
ForwardCallXF endp

ForwardCallXD proc frame
    prologue
    forward_xmm
    forward_gpr
    epilogue
ForwardCallXD endp

; Callbacks
; ----------------------------

extern RelayCallback : PROC
public CallSwitchStack

; First, make a copy of the GPR argument registers (rcx, rdx, r8, r9).
; Then call the C function RelayCallback with the following arguments:
; static trampoline ID, a pointer to the saved GPR array, a pointer to the stack
; arguments of this call, and a pointer to a struct that will contain the result registers.
; After the call, simply load these registers from the output struct.
trampoline macro ID
    endbr64
    sub rsp, 120
    .allocstack 120
    .endprolog
    mov qword ptr [rsp+32], rcx
    mov qword ptr [rsp+40], rdx
    mov qword ptr [rsp+48], r8
    mov qword ptr [rsp+56], r9
    mov rcx, ID
    lea rdx, qword ptr [rsp+32]
    lea r8, qword ptr [rsp+160]
    lea r9, qword ptr [rsp+96]
    call RelayCallback
    mov rax, qword ptr [rsp+96]
    add rsp, 120
    ret
endm

; Same thing, but also forward the XMM argument registers and load the XMM result registers.
trampoline_vec macro ID
    endbr64
    sub rsp, 120
    .allocstack 120
    .endprolog
    mov qword ptr [rsp+32], rcx
    mov qword ptr [rsp+40], rdx
    mov qword ptr [rsp+48], r8
    mov qword ptr [rsp+56], r9
    movsd qword ptr [rsp+64], xmm0
    movsd qword ptr [rsp+72], xmm1
    movsd qword ptr [rsp+80], xmm2
    movsd qword ptr [rsp+88], xmm3
    mov rcx, ID
    lea rdx, qword ptr [rsp+32]
    lea r8, qword ptr [rsp+160]
    lea r9, qword ptr [rsp+96]
    call RelayCallback
    mov rax, qword ptr [rsp+96]
    movsd xmm0, qword ptr [rsp+104]
    add rsp, 120
    ret
endm

; When a callback is relayed, Koffi will call into Node.js and V8 to execute Javascript.
; The problem is that we're still running on the separate Koffi stack, and V8 will
; probably misdetect this as a "stack overflow". We have to restore the old
; stack pointer, call Node.js/V8 and go back to ours.
; The first three parameters (rcx, rdx, r8) are passed through untouched.
CallSwitchStack proc frame
    endbr64
    push rbp
    .pushreg rbp
    mov rbp, rsp
    .setframe rbp, 0
    .endprolog
    mov rax, qword ptr [rsp+56]
    mov r10, rsp
    mov r11, qword ptr [rsp+48]
    sub r10, qword ptr [r11+0]
    and r10, -16
    mov qword ptr [r11+8], r10
    lea rsp, [r9-32]
    call rax
    mov rsp, rbp
    pop rbp
    ret
CallSwitchStack endp

; Trampolines
; ----------------------------

include trampolines/masm64.inc

end
