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
#include "ffi.hh"
#include "util.hh"

#include <napi.h>

namespace RG {

bool AnalyseFunction(Napi::Env env, InstanceData *instance, FunctionInfo *func);

struct BackRegisters;

// I'm not sure why the alignas(8), because alignof(CallData) is 8 without it.
// But on Windows i386, without it, the alignment may not be correct (compiler bug?).
class alignas(8) CallData {
    struct OutArgument {
        enum class Kind {
            Array,
            Buffer,
            String,
            String16,
            String32,
            Object
        };

        Kind kind;

        napi_ref ref;
        const uint8_t *ptr;
        const TypeInfo *type;

        Size max_len; // Only for indirect strings
    };

    Napi::Env env;
    InstanceData *instance;

    InstanceMemory *mem;
    Span<uint8_t> old_stack_mem;
    Span<uint8_t> old_heap_mem;

    uint8_t *new_sp;
    uint8_t *old_sp;

    union {
        int8_t i8;
        uint8_t u8;
        int16_t i16;
        uint16_t u16;
        int32_t i32;
        uint32_t u32;
        int64_t i64;
        uint64_t u64;
        float f;
        double d;
        void *ptr;
        uint8_t buf[32];
    } result;
    uint8_t *return_ptr = nullptr;

    LocalArray<int16_t, 16> used_trampolines;
    HeapArray<OutArgument> out_arguments;

    BlockAllocator call_alloc;

public:
    CallData(Napi::Env env, InstanceData *instance, InstanceMemory *mem);
    ~CallData();

    void Dispose();

#ifdef UNITY_BUILD
    #ifdef _MSC_VER
        #define INLINE_IF_UNITY __forceinline
    #else
        #define INLINE_IF_UNITY __attribute__((always_inline)) inline
    #endif
#else
    #define INLINE_IF_UNITY
#endif

    INLINE_IF_UNITY bool Prepare(const FunctionInfo *func, const Napi::CallbackInfo &info);
    INLINE_IF_UNITY void Execute(const FunctionInfo *func, void *native);
    INLINE_IF_UNITY Napi::Value Complete(const FunctionInfo *func);

#undef INLINE_IF_UNITY

    void Relay(Size idx, uint8_t *own_sp, uint8_t *caller_sp, bool switch_stack, BackRegisters *out_reg);
    void RelaySafe(Size idx, uint8_t *own_sp, uint8_t *caller_sp, bool outside_call, BackRegisters *out_reg);
    static void RelayAsync(napi_env, napi_value, void *, void *udata);

    void DumpForward(const FunctionInfo *func) const;

    bool PushString(Napi::Value value, int directions, const char **out_str);
    Size PushStringValue(Napi::Value value, const char **out_str);
    bool PushString16(Napi::Value value, int directions, const char16_t **out_str16);
    Size PushString16Value(Napi::Value value, const char16_t **out_str16);
    bool PushString32(Napi::Value value, int directions, const char32_t **out_str32);
    Size PushString32Value(Napi::Value value, const char32_t **out_str32);
    bool PushObject(Napi::Object obj, const TypeInfo *type, uint8_t *origin);
    bool PushNormalArray(Napi::Array array, Size len, const TypeInfo *type, uint8_t *origin);
    void PushBuffer(Span<const uint8_t> buffer, Size size, const TypeInfo *type, uint8_t *origin);
    bool PushStringArray(Napi::Value value, const TypeInfo *type, uint8_t *origin);
    bool PushPointer(Napi::Value value, const TypeInfo *type, int directions, void **out_ptr);
    Size PushIndirectString(Napi::Array array, const TypeInfo *ref, uint8_t **out_ptr);

    void *ReserveTrampoline(const FunctionInfo *proto, Napi::Function func);

    BlockAllocator *GetAllocator() { return &call_alloc; }

private:
    template <typename T>
    bool AllocStack(Size size, Size align, T **out_ptr);
    template <typename T = uint8_t>
    T *AllocHeap(Size size, Size align);

    void PopOutArguments();
};

template <typename T>
inline bool CallData::AllocStack(Size size, Size align, T **out_ptr)
{
    uint8_t *ptr = AlignDown(mem->stack.end() - size, align);
    Size delta = mem->stack.end() - ptr;

    // Keep 512 bytes for redzone (required in some ABIs)
    if (mem->stack.len - 512 < delta) [[unlikely]] {
        ThrowError<Napi::Error>(env, "FFI call is taking up too much memory");
        return false;
    }

#ifdef RG_DEBUG
    memset(ptr, 0, delta);
#endif

    mem->stack.len -= delta;

    *out_ptr = (T *)ptr;
    return true;
}

template <typename T>
inline T *CallData::AllocHeap(Size size, Size align)
{
    uint8_t *ptr = AlignUp(mem->heap.ptr, align);
    Size delta = size + (ptr - mem->heap.ptr);

    if (size < 4096 && delta <= mem->heap.len) [[likely]] {
#ifdef RG_DEBUG
        memset(mem->heap.ptr, 0, (size_t)delta);
#endif

        mem->heap.ptr += delta;
        mem->heap.len -= delta;

        return ptr;
    } else {
#ifdef RG_DEBUG
        int flags = (int)AllocFlag::Zero;
#else
        int flags = 0;
#endif

        ptr = (uint8_t *)AllocateRaw(&call_alloc, size + align, flags);
        ptr = AlignUp(ptr, align);

        return ptr;
    }
}

void *GetTrampoline(int16_t idx, const FunctionInfo *proto);

}
