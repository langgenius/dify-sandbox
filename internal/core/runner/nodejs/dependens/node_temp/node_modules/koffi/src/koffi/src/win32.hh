// Copyright 2023 Niels Martignène <niels.martignene@protonmail.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the “Software”), to deal in 
// the Software without restriction, including without limitation the rights to use,
// copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the
// Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
// OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.

#pragma once

#include "src/core/libcc/libcc.hh"

#include <intrin.h>
#include <napi.h>

namespace RG {

struct PE_DOS_HEADER {
    uint16_t e_magic;
    uint16_t e_cblp;
    uint16_t e_cp;
    uint16_t e_crlc;
    uint16_t e_cparhdr;
    uint16_t e_minalloc;
    uint16_t e_maxalloc;
    uint16_t e_ss;
    uint16_t e_sp;
    uint16_t e_csum;
    uint16_t e_ip;
    uint16_t e_cs;
    uint16_t e_lfarlc;
    uint16_t e_ovno;
    uint16_t e_res[4];
    uint16_t e_oemid;
    uint16_t e_oeminfo;
    uint16_t e_res2[10];
    uint32_t e_lfanew;
};

struct PE_FILE_HEADER {
    uint16_t Machine;
    uint16_t NumberOfSections;
    uint32_t TimeDateStamp;
    uint32_t PointerToSymbolTable;
    uint32_t NumberOfSymbols;
    uint16_t SizeOfOptionalHeader;
    uint16_t Characteristics;
};

struct PE_NT_HEADERS {
    uint32_t Signature;
    PE_FILE_HEADER FileHeader;

    // ... OptionalHeader;
};

#if _WIN64

struct TEB {
    void *ExceptionList;
    void *StackBase;
    void *StackLimit;
    char _pad1[80];
    unsigned long LastErrorValue;
    char _pad2[5132];
    void *DeallocationStack;
    char _pad3[712];
    uint32_t GuaranteedStackBytes;
    char _pad4[162];
    uint16_t SameTebFlags;
};
static_assert(RG_OFFSET_OF(TEB, DeallocationStack) == 0x1478);
static_assert(RG_OFFSET_OF(TEB, GuaranteedStackBytes) == 0x1748);
static_assert(RG_OFFSET_OF(TEB, SameTebFlags) == 0x17EE);

#else

struct TEB {
    void *ExceptionList;
    void *StackBase;
    void *StackLimit;
    char _pad1[40];
    unsigned long LastErrorValue;
    char _pad2[3540];
    void *DeallocationStack;
    char _pad3[360];
    uint32_t GuaranteedStackBytes;
    char _pad4[78];
    uint16_t SameTebFlags;
};
static_assert(RG_OFFSET_OF(TEB, DeallocationStack) == 0xE0C);
static_assert(RG_OFFSET_OF(TEB, GuaranteedStackBytes) == 0x0F78);
static_assert(RG_OFFSET_OF(TEB, SameTebFlags) == 0xFCA);

#endif

static inline TEB *GetTEB()
{
#if defined(__aarch64__) || defined(_M_ARM64)
    TEB *teb = (TEB *)__getReg(18);
#elif defined(__x86_64__) || defined(_M_AMD64)
    TEB *teb = (TEB *)__readgsqword(0x30);
#else
    TEB *teb = (TEB *)__readfsdword(0x18);
#endif

    return teb;
}

extern const HashMap<int, const char *> WindowsMachineNames;

void *LoadWindowsLibrary(Napi::Env env, Span<const char> path); // Returns HANDLE

int GetSelfMachine();
int GetDllMachine(const wchar_t *filename);

}
