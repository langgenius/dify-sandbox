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

#if defined(__arm__) || (defined(__M_ARM) && !defined(_M_ARM64))

#include "src/core/libcc/libcc.hh"
#include "ffi.hh"
#include "call.hh"
#include "util.hh"

#include <napi.h>
#include <signal.h>
#include <setjmp.h>

namespace RG {

struct HfaRet {
    double d0;
    double d1;
    double d2;
    double d3;
};

struct BackRegisters {
    uint32_t r0;
    uint32_t r1;
    double d0;
    double d1;
    double d2;
    double d3;
};

extern "C" uint64_t ForwardCallGG(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" float ForwardCallF(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" HfaRet ForwardCallDDDD(const void *func, uint8_t *sp, uint8_t **out_old_sp);

extern "C" uint64_t ForwardCallXGG(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" float ForwardCallXF(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" HfaRet ForwardCallXDDDD(const void *func, uint8_t *sp, uint8_t **out_old_sp);

extern "C" napi_value CallSwitchStack(Napi::Function *func, size_t argc, napi_value *argv,
                                      uint8_t *old_sp, Span<uint8_t> *new_stack,
                                      napi_value (*call)(Napi::Function *func, size_t argc, napi_value *argv));

#include "trampolines/prototypes.inc"

static int IsHFA(const TypeInfo *type)
{
#ifdef __ARM_PCS_VFP
    bool float32 = false;
    bool float64 = false;
    int count = 0;

    count = AnalyseFlat(type, [&](const TypeInfo *type, int, int) {
        if (type->primitive == PrimitiveKind::Float32) {
            float32 = true;
        } else if (type->primitive == PrimitiveKind::Float64) {
            float64 = true;
        } else {
            float32 = true;
            float64 = true;
        }
    });

    if (count < 1 || count > 4)
        return 0;
    if (float32 && float64)
        return 0;

    return count;
#else
    return 0;
#endif
}

bool AnalyseFunction(Napi::Env, InstanceData *, FunctionInfo *func)
{
    if (int hfa = IsHFA(func->ret.type); hfa) {
        func->ret.vec_count = hfa;
    } else if (func->ret.type->primitive != PrimitiveKind::Record &&
               func->ret.type->primitive != PrimitiveKind::Union) {
        func->ret.gpr_count = (func->ret.type->size > 4) ? 2 : 1;
    } else if (func->ret.type->size <= 4) {
        func->ret.gpr_count = 1;
    } else {
        func->ret.use_memory = true;
    }

    int gpr_avail = 4 - func->ret.use_memory;
    int vec_avail = 16;
    bool started_stack = false;

    for (ParameterInfo &param: func->parameters) {
        switch (param.type->primitive) {
            case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

            case PrimitiveKind::Bool:
            case PrimitiveKind::Int8:
            case PrimitiveKind::UInt8:
            case PrimitiveKind::Int16:
            case PrimitiveKind::Int16S:
            case PrimitiveKind::UInt16:
            case PrimitiveKind::UInt16S:
            case PrimitiveKind::Int32:
            case PrimitiveKind::Int32S:
            case PrimitiveKind::UInt32:
            case PrimitiveKind::UInt32S:
            case PrimitiveKind::String:
            case PrimitiveKind::String16:
            case PrimitiveKind::String32:
            case PrimitiveKind::Pointer:
            case PrimitiveKind::Callback: {
                if (gpr_avail) {
                    param.gpr_count = 1;
                    gpr_avail--;
                } else {
                    started_stack = true;
                }
            } break;
            case PrimitiveKind::Int64:
            case PrimitiveKind::Int64S:
            case PrimitiveKind::UInt64:
            case PrimitiveKind::UInt64S: {
                bool realign = gpr_avail % 2;
                int need = 2 + realign;

                if (gpr_avail >= need) {
                    param.gpr_count = 2;
                    gpr_avail -= need;
                } else {
                    started_stack = true;
                }
            } break;
            case PrimitiveKind::Record:
            case PrimitiveKind::Union: {
                int hfa = IsHFA(param.type);

                if (hfa) {
                    if (hfa <= vec_avail) {
                        param.vec_count = hfa;
                        vec_avail -= hfa;
                    } else {
                        vec_avail = 0;
                        started_stack = true;
                    }
                } else {
                    bool realign = (param.type->align == 8 && (gpr_avail % 2));
                    int need = (param.type->size + 3) / 4 + realign;

                    if (need <= gpr_avail) {
                        param.gpr_count = need - realign;
                        gpr_avail -= need;
                    } else if (!started_stack) {
                        param.gpr_count = gpr_avail - realign;
                        gpr_avail = 0;

                        started_stack = true;
                    }
                }
            } break;
            case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
            case PrimitiveKind::Float32:
            case PrimitiveKind::Float64: {
#ifdef __ARM_PCS_VFP
                bool vfp = !param.variadic;
#else
                bool vfp = false;
#endif

                int need = param.type->size / 4;

                if (vfp) {
                    if (need <= vec_avail) {
                        param.vec_count = need;
                        vec_avail -= need;
                    } else {
                        started_stack = true;
                    }
                } else {
                    need += (gpr_avail % 2);

                    if (need <= gpr_avail) {
                        param.gpr_count = 2;
                        gpr_avail -= need;
                    } else {
                        started_stack = true;
                    }
                }
            } break;

            case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
        }

        func->args_size += AlignLen(param.type->size, 16);
    }

    func->forward_fp = (vec_avail < 16);

    return true;
}

bool CallData::Prepare(const FunctionInfo *func, const Napi::CallbackInfo &info)
{
    uint32_t *args_ptr = nullptr;
    uint32_t *gpr_ptr = nullptr;
    uint32_t *vec_ptr = nullptr;

    // Unlike other call conventions, here we put the general-purpose
    // registers just before the stack (so behind the vector ones).
    // In the armv7hf calling convention, some arguments can end up
    // partially in GPR, partially in the stack.
    if (!AllocStack(func->args_size, 16, &args_ptr)) [[unlikely]]
        return false;
    if (!AllocStack(4 * 4, 8, &gpr_ptr)) [[unlikely]]
        return false;
    if (!AllocStack(8 * 8, 8, &vec_ptr)) [[unlikely]]
        return false;
    if (func->ret.use_memory) {
        return_ptr = AllocHeap(func->ret.type->size, 16);
        *(uint8_t **)(gpr_ptr++) = return_ptr;
    }

#define PUSH_INTEGER_32(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            *((param.gpr_count ? gpr_ptr : args_ptr)++) = (uint32_t)v; \
        } while (false)
#define PUSH_INTEGER_32_SWAP(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            *((param.gpr_count ? gpr_ptr : args_ptr)++) = (uint32_t)ReverseBytes(v); \
        } while (false)
#define PUSH_INTEGER_64(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
             \
            if (param.gpr_count) [[likely]] { \
                gpr_ptr = AlignUp(gpr_ptr, 8); \
                *(uint64_t *)gpr_ptr = (uint64_t)v; \
                gpr_ptr += param.gpr_count; \
            } else { \
                args_ptr = AlignUp(args_ptr, 8); \
                *(uint64_t *)args_ptr = (uint64_t)v; \
                args_ptr += 2; \
            } \
        } while (false)
#define PUSH_INTEGER_64_SWAP(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
             \
            if (param.gpr_count) [[likely]] { \
                gpr_ptr = AlignUp(gpr_ptr, 8); \
                *(uint64_t *)gpr_ptr = (uint64_t)ReverseBytes(v); \
                gpr_ptr += param.gpr_count; \
            } else { \
                args_ptr = AlignUp(args_ptr, 8); \
                *(uint64_t *)args_ptr = (uint64_t)ReverseBytes(v); \
                args_ptr += 2; \
            } \
        } while (false)

    // Push arguments
    for (Size i = 0; i < func->parameters.len; i++) {
        const ParameterInfo &param = func->parameters[i];
        RG_ASSERT(param.directions >= 1 && param.directions <= 3);

        Napi::Value value = info[param.offset];

        switch (param.type->primitive) {
            case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

            case PrimitiveKind::Bool: {
                if (!value.IsBoolean()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected boolean", GetValueType(instance, value));
                    return false;
                }

                bool b = value.As<Napi::Boolean>();
                *((param.gpr_count ? gpr_ptr : args_ptr)++) = (uint32_t)b;
            } break;
            case PrimitiveKind::Int8: { PUSH_INTEGER_32(int8_t); } break;
            case PrimitiveKind::UInt8: { PUSH_INTEGER_32(uint8_t); } break;
            case PrimitiveKind::Int16: { PUSH_INTEGER_32(int16_t); } break;
            case PrimitiveKind::Int16S: { PUSH_INTEGER_32_SWAP(int16_t); } break;
            case PrimitiveKind::UInt16: { PUSH_INTEGER_32(uint16_t); } break;
            case PrimitiveKind::UInt16S: { PUSH_INTEGER_32_SWAP(uint16_t); } break;
            case PrimitiveKind::Int32: { PUSH_INTEGER_32(int32_t); } break;
            case PrimitiveKind::Int32S: { PUSH_INTEGER_32_SWAP(int32_t); } break;
            case PrimitiveKind::UInt32: { PUSH_INTEGER_32(uint32_t); } break;
            case PrimitiveKind::UInt32S: { PUSH_INTEGER_32_SWAP(uint32_t); } break;
            case PrimitiveKind::Int64: { PUSH_INTEGER_64(int64_t); } break;
            case PrimitiveKind::Int64S: { PUSH_INTEGER_64_SWAP(int64_t); } break;
            case PrimitiveKind::UInt64: { PUSH_INTEGER_64(uint64_t); } break;
            case PrimitiveKind::UInt64S: { PUSH_INTEGER_64_SWAP(uint64_t); } break;
            case PrimitiveKind::String: {
                const char *str;
                if (!PushString(value, param.directions, &str)) [[unlikely]]
                    return false;

                *(const char **)((param.gpr_count ? gpr_ptr : args_ptr)++) = str;
            } break;
            case PrimitiveKind::String16: {
                const char16_t *str16;
                if (!PushString16(value, param.directions, &str16)) [[unlikely]]
                    return false;

                *(const char16_t **)((param.gpr_count ? gpr_ptr : args_ptr)++) = str16;
            } break;
            case PrimitiveKind::String32: {
                const char32_t *str32;
                if (!PushString32(value, param.directions, &str32)) [[unlikely]]
                    return false;

                *(const char32_t **)((param.gpr_count ? gpr_ptr : args_ptr)++) = str32;
            } break;
            case PrimitiveKind::Pointer: {
                void *ptr;
                if (!PushPointer(value, param.type, param.directions, &ptr)) [[unlikely]]
                    return false;

                *(void **)((param.gpr_count ? gpr_ptr : args_ptr)++) = ptr;
            } break;
            case PrimitiveKind::Record:
            case PrimitiveKind::Union: {
                if (!IsObject(value)) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected object", GetValueType(instance, value));
                    return false;
                }

                Napi::Object obj = value.As<Napi::Object>();

                if (param.vec_count) {
                    if (!PushObject(obj, param.type, (uint8_t *)vec_ptr))
                        return false;
                    vec_ptr += param.vec_count;
                } else if (param.gpr_count) {
                    RG_ASSERT(param.type->align <= 8);

                    int16_t align = (param.type->align <= 4) ? 4 : 8;
                    gpr_ptr = AlignUp(gpr_ptr, align);

                    if (!PushObject(obj, param.type, (uint8_t *)gpr_ptr))
                        return false;

                    gpr_ptr += param.gpr_count;
                    args_ptr += (param.type->size - param.gpr_count * 4 + 3) / 4;
                } else if (param.type->size) {
                    int16_t align = (param.type->align <= 4) ? 4 : 8;
                    args_ptr = AlignUp(args_ptr, align);

                    if (!PushObject(obj, param.type, (uint8_t *)args_ptr))
                        return false;
                    args_ptr += (param.type->size + 3) / 4;
                }
            } break;
            case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
            case PrimitiveKind::Float32: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                float f = GetNumber<float>(value);

                if (param.vec_count) [[likely]] {
                    *(float *)(vec_ptr++) = f;
                } else if (param.gpr_count) {
                    *(float *)(gpr_ptr++) = f;
                } else {
                    *(float *)(args_ptr++) = f;
                }
            } break;
            case PrimitiveKind::Float64: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                double d = GetNumber<double>(value);

                if (param.vec_count) [[likely]] {
                    *(double *)vec_ptr = d;
                    vec_ptr += 2;
                } else if (param.gpr_count) {
                    gpr_ptr = AlignUp(gpr_ptr, 8);
                    *(double *)gpr_ptr = d;
                    gpr_ptr += 2;
                } else {
                    args_ptr = AlignUp(args_ptr, 8);
                    *(double *)args_ptr = d;
                    args_ptr += 2;
                }
            } break;
            case PrimitiveKind::Callback: {
                void *ptr;

                if (value.IsFunction()) {
                    Napi::Function func = value.As<Napi::Function>();

                    ptr = ReserveTrampoline(param.type->ref.proto, func);
                    if (!ptr) [[unlikely]]
                        return false;
                } else if (CheckValueTag(instance, value, param.type->ref.marker)) {
                    ptr = value.As<Napi::External<void>>().Data();
                } else if (IsNullOrUndefined(value)) {
                    ptr = nullptr;
                } else {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), param.type->name);
                    return false;
                }

                *(void **)((param.gpr_count ? gpr_ptr : args_ptr)++) = ptr;
            } break;

            case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
        }
    }

#undef PUSH_INTEGER_64_SWAP
#undef PUSH_INTEGER_64
#undef PUSH_INTEGER_32_SWAP
#undef PUSH_INTEGER_32

    new_sp = mem->stack.end();

    return true;
}

void CallData::Execute(const FunctionInfo *func, void *native)
{
#define PERFORM_CALL(Suffix) \
        ([&]() { \
            auto ret = (func->forward_fp ? ForwardCallX ## Suffix(native, new_sp, &old_sp) \
                                         : ForwardCall ## Suffix(native, new_sp, &old_sp)); \
            return ret; \
        })()

    // Execute and convert return value
    switch (func->ret.type->primitive) {
        case PrimitiveKind::Void:
        case PrimitiveKind::Bool:
        case PrimitiveKind::Int8:
        case PrimitiveKind::UInt8:
        case PrimitiveKind::Int16:
        case PrimitiveKind::Int16S:
        case PrimitiveKind::UInt16:
        case PrimitiveKind::UInt16S:
        case PrimitiveKind::Int32:
        case PrimitiveKind::Int32S:
        case PrimitiveKind::UInt32:
        case PrimitiveKind::UInt32S:
        case PrimitiveKind::Int64:
        case PrimitiveKind::Int64S:
        case PrimitiveKind::UInt64:
        case PrimitiveKind::UInt64S:
        case PrimitiveKind::String:
        case PrimitiveKind::String16:
        case PrimitiveKind::String32:
        case PrimitiveKind::Pointer:
        case PrimitiveKind::Callback: { result.u64 = PERFORM_CALL(GG); } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            if (func->ret.vec_count) {
                HfaRet ret = PERFORM_CALL(DDDD);
                memcpy(&result.buf, &ret, RG_SIZE(ret));
            } else {
                result.u64 = PERFORM_CALL(GG);
            }
        } break;
        case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
        case PrimitiveKind::Float32: { result.f = PERFORM_CALL(F); } break;
        case PrimitiveKind::Float64: { result.d = PERFORM_CALL(DDDD).d0; } break;

        case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
    }

#undef PERFORM_CALL
}

Napi::Value CallData::Complete(const FunctionInfo *func)
{
    RG_DEFER {
       PopOutArguments();

        if (func->ret.type->dispose) {
            func->ret.type->dispose(env, func->ret.type, result.ptr);
        }
    };

    switch (func->ret.type->primitive) {
        case PrimitiveKind::Void: return env.Undefined();
        case PrimitiveKind::Bool: return Napi::Boolean::New(env, result.u8 & 0x1);
        case PrimitiveKind::Int8: return Napi::Number::New(env, (double)result.i8);
        case PrimitiveKind::UInt8: return Napi::Number::New(env, (double)result.u8);
        case PrimitiveKind::Int16: return Napi::Number::New(env, (double)result.i16);
        case PrimitiveKind::Int16S: return Napi::Number::New(env, (double)ReverseBytes(result.i16));
        case PrimitiveKind::UInt16: return Napi::Number::New(env, (double)result.u16);
        case PrimitiveKind::UInt16S: return Napi::Number::New(env, (double)ReverseBytes(result.u16));
        case PrimitiveKind::Int32: return Napi::Number::New(env, (double)result.i32);
        case PrimitiveKind::Int32S: return Napi::Number::New(env, (double)ReverseBytes(result.i32));
        case PrimitiveKind::UInt32: return Napi::Number::New(env, (double)result.u32);
        case PrimitiveKind::UInt32S: return Napi::Number::New(env, (double)ReverseBytes(result.u32));
        case PrimitiveKind::Int64: return NewBigInt(env, result.i64);
        case PrimitiveKind::Int64S: return NewBigInt(env, ReverseBytes(result.i64));
        case PrimitiveKind::UInt64: return NewBigInt(env, result.u64);
        case PrimitiveKind::UInt64S: return NewBigInt(env, ReverseBytes(result.u64));
        case PrimitiveKind::String: return result.ptr ? Napi::String::New(env, (const char *)result.ptr) : env.Null();
        case PrimitiveKind::String16: return result.ptr ? Napi::String::New(env, (const char16_t *)result.ptr) : env.Null();
        case PrimitiveKind::String32: return result.ptr ? MakeStringFromUTF32(env, (const char32_t *)result.ptr) : env.Null();
        case PrimitiveKind::Pointer:
        case PrimitiveKind::Callback: {
            if (result.ptr) {
                Napi::External<void> external = Napi::External<void>::New(env, result.ptr);
                SetValueTag(instance, external, func->ret.type->ref.marker);

                return external;
            } else {
                return env.Null();
            }
        } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            const uint8_t *ptr = return_ptr ? (const uint8_t *)return_ptr
                                            : (const uint8_t *)&result.buf;

            Napi::Object obj = DecodeObject(env, ptr, func->ret.type);
            return obj;
        } break;
        case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
        case PrimitiveKind::Float32: return Napi::Number::New(env, (double)result.f);
        case PrimitiveKind::Float64: return Napi::Number::New(env, result.d);

        case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
    }

    RG_UNREACHABLE();
}

void CallData::Relay(Size idx, uint8_t *own_sp, uint8_t *caller_sp, bool switch_stack, BackRegisters *out_reg)
{
    if (env.IsExceptionPending()) [[unlikely]]
        return;

    const TrampolineInfo &trampoline = shared.trampolines[idx];

    const FunctionInfo *proto = trampoline.proto;
    Napi::Function func = trampoline.func.Value();

    uint32_t *vec_ptr = (uint32_t *)own_sp;
    uint32_t *gpr_ptr = vec_ptr + 16;
    uint32_t *args_ptr = (uint32_t *)caller_sp;

    uint8_t *return_ptr = proto->ret.use_memory ? (uint8_t *)gpr_ptr[0] : nullptr;
    gpr_ptr += proto->ret.use_memory;

    RG_DEFER_N(err_guard) { memset(out_reg, 0, RG_SIZE(*out_reg)); };

    if (trampoline.generation >= 0 && trampoline.generation != (int32_t)mem->generation) [[unlikely]] {
        ThrowError<Napi::Error>(env, "Cannot use non-registered callback beyond FFI call");
        return;
    }

    LocalArray<napi_value, MaxParameters + 1> arguments;

    arguments.Append(!trampoline.recv.IsEmpty() ? trampoline.recv.Value() : env.Undefined());

    // Convert to JS arguments
    for (Size i = 0; i < proto->parameters.len; i++) {
        const ParameterInfo &param = proto->parameters[i];
        RG_ASSERT(param.directions >= 1 && param.directions <= 3);

        switch (param.type->primitive) {
            case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

            case PrimitiveKind::Bool: {
                bool b = *(bool *)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = Napi::Boolean::New(env, b);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int8: {
                double d = (double)*(int8_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt8: {
                double d = (double)*(uint8_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int16: {
                double d = (double)*(int16_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int16S: {
                int16_t v = *(int16_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt16: {
                double d = (double)*(uint16_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt16S: {
                uint16_t v = *(uint16_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int32: {
                double d = (double)*(int32_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int32S: {
                int32_t v = *(int32_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt32: {
                double d = (double)*(uint32_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt32S: {
                uint32_t v = *(uint32_t *)((param.gpr_count ? gpr_ptr : args_ptr)++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int64: {
                gpr_ptr = AlignUp(gpr_ptr, 8);

                int64_t v = *(int64_t *)(param.gpr_count ? gpr_ptr : args_ptr);
                (param.gpr_count ? gpr_ptr : args_ptr) += 2;

                Napi::Value arg = NewBigInt(env, v);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int64S: {
                gpr_ptr = AlignUp(gpr_ptr, 8);

                int64_t v = *(int64_t *)(param.gpr_count ? gpr_ptr : args_ptr);
                (param.gpr_count ? gpr_ptr : args_ptr) += 2;

                Napi::Value arg = NewBigInt(env, ReverseBytes(v));
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt64: {
                gpr_ptr = AlignUp(gpr_ptr, 8);

                uint64_t v = *(uint64_t *)(param.gpr_count ? gpr_ptr : args_ptr);
                (param.gpr_count ? gpr_ptr : args_ptr) += 2;

                Napi::Value arg = NewBigInt(env, v);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt64S: {
                gpr_ptr = AlignUp(gpr_ptr, 8);

                uint64_t v = *(uint64_t *)(param.gpr_count ? gpr_ptr : args_ptr);
                (param.gpr_count ? gpr_ptr : args_ptr) += 2;

                Napi::Value arg = NewBigInt(env, ReverseBytes(v));
                arguments.Append(arg);
            } break;
            case PrimitiveKind::String: {
                const char *str = *(const char **)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = str ? Napi::String::New(env, str) : env.Null();
                arguments.Append(arg);

                if (param.type->dispose) {
                    param.type->dispose(env, param.type, str);
                }
            } break;
            case PrimitiveKind::String16: {
                const char16_t *str16 = *(const char16_t **)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = str16 ? Napi::String::New(env, str16) : env.Null();
                arguments.Append(arg);

                if (param.type->dispose) {
                    param.type->dispose(env, param.type, str16);
                }
            } break;
            case PrimitiveKind::String32: {
                const char32_t *str32 = *(const char32_t **)((param.gpr_count ? gpr_ptr : args_ptr)++);

                Napi::Value arg = str32 ? MakeStringFromUTF32(env, str32) : env.Null();
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Pointer:
            case PrimitiveKind::Callback: {
                void *ptr2 = *(void **)((param.gpr_count ? gpr_ptr : args_ptr)++);

                if (ptr2) {
                    Napi::External<void> external = Napi::External<void>::New(env, ptr2);
                    SetValueTag(instance, external, param.type->ref.marker);

                    arguments.Append(external);
                } else {
                    arguments.Append(env.Null());
                }

                if (param.type->dispose) {
                    param.type->dispose(env, param.type, ptr2);
                }
            } break;
            case PrimitiveKind::Record:
            case PrimitiveKind::Union: {
                if (param.vec_count) {
                    Napi::Object obj = DecodeObject(env, (const uint8_t *)vec_ptr, param.type);
                    arguments.Append(obj);

                    vec_ptr += param.vec_count;
                } else if (param.gpr_count) {
                    RG_ASSERT(param.type->align <= 8);

                    int16_t gpr_size = param.gpr_count * 4;
                    int16_t align = (param.type->align <= 4) ? 4 : 8;
                    gpr_ptr = AlignUp(gpr_ptr, align);

                    if (param.type->size > gpr_size) {
                        // XXX: Expensive, can we do better?
                        // The problem is that the object is split between the GPRs and the caller stack.
                        uint8_t *ptr = AllocHeap(param.type->size, 16);

                        memcpy(ptr, gpr_ptr, gpr_size);
                        memcpy(ptr + gpr_size, args_ptr, param.type->size - gpr_size);

                        Napi::Object obj = DecodeObject(env, ptr, param.type);
                        arguments.Append(obj);

                        gpr_ptr += param.gpr_count;
                        args_ptr += (param.type->size - gpr_size + 3) / 4;
                    } else {
                        Napi::Object obj = DecodeObject(env, (const uint8_t *)gpr_ptr, param.type);
                        arguments.Append(obj);

                        gpr_ptr += param.gpr_count;
                    }
                } else if (param.type->size) {
                    int16_t align = (param.type->align <= 4) ? 4 : 8;
                    args_ptr = AlignUp(args_ptr, align);

                    Napi::Object obj = DecodeObject(env, (const uint8_t *)args_ptr, param.type);
                    arguments.Append(obj);

                    args_ptr += (param.type->size + 3) / 4;
                }
            } break;
            case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
            case PrimitiveKind::Float32: {
                float f;
                if (param.vec_count) [[likely]] {
                    f = *(float *)(vec_ptr++);
                } else if (param.gpr_count) {
                    f = *(float *)(gpr_ptr++);
                } else {
                    f = *(float *)(args_ptr++);
                }

                Napi::Value arg = Napi::Number::New(env, (double)f);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Float64: {
                double d;
                if (param.vec_count) [[likely]] {
                    d = *(double *)vec_ptr;
                    vec_ptr += 2;
                } else if (param.gpr_count) {
                    gpr_ptr = AlignUp(gpr_ptr, 8);
                    d = *(double *)gpr_ptr;
                    gpr_ptr += 2;
                } else {
                    args_ptr = AlignUp(args_ptr, 8);
                    d = *(double *)args_ptr;
                    args_ptr += 2;
                }

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;

            case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
        }
    }

    const TypeInfo *type = proto->ret.type;

    // Make the call
    napi_value ret;
    if (switch_stack) {
        ret = CallSwitchStack(&func, (size_t)arguments.len, arguments.data, old_sp, &mem->stack,
                              [](Napi::Function *func, size_t argc, napi_value *argv) { return (napi_value)func->Call(argv[0], argc - 1, argv + 1); });
    } else {
        ret = (napi_value)func.Call(arguments[0], arguments.len - 1, arguments.data + 1);
    }
    Napi::Value value(env, ret);

    if (env.IsExceptionPending()) [[unlikely]]
        return;

#define RETURN_INTEGER_32(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value for return value, expected number", GetValueType(instance, value)); \
                return; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            out_reg->r0 = (uint32_t)v; \
        } while (false)
#define RETURN_INTEGER_32_SWAP(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            out_reg->r0 = (uint32_t)ReverseBytes(v); \
        } while (false)
#define RETURN_INTEGER_64(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return; \
            } \
             \
            CType v = GetNumber<CType>(value); \
             \
            out_reg->r0 = (uint32_t)((uint64_t)v >> 32); \
            out_reg->r1 = (uint32_t)((uint64_t)v & 0xFFFFFFFFu); \
        } while (false)
#define RETURN_INTEGER_64_SWAP(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return; \
            } \
             \
            CType v = ReverseBytes(GetNumber<CType>(value)); \
             \
            out_reg->r0 = (uint32_t)((uint64_t)v >> 32); \
            out_reg->r1 = (uint32_t)((uint64_t)v & 0xFFFFFFFFu); \
        } while (false)

    // Convert the result
    switch (type->primitive) {
        case PrimitiveKind::Void: {} break;
        case PrimitiveKind::Bool: {
            if (!value.IsBoolean()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected boolean", GetValueType(instance, value));
                return;
            }

            bool b = value.As<Napi::Boolean>();
            out_reg->r0 = (uint32_t)b;
        } break;
        case PrimitiveKind::Int8: { RETURN_INTEGER_32(int8_t); } break;
        case PrimitiveKind::UInt8: { RETURN_INTEGER_32(uint8_t); } break;
        case PrimitiveKind::Int16: { RETURN_INTEGER_32(int16_t); } break;
        case PrimitiveKind::Int16S: { RETURN_INTEGER_32_SWAP(int16_t); } break;
        case PrimitiveKind::UInt16: { RETURN_INTEGER_32(uint16_t); } break;
        case PrimitiveKind::UInt16S: { RETURN_INTEGER_32_SWAP(uint16_t); } break;
        case PrimitiveKind::Int32: { RETURN_INTEGER_32(int32_t); } break;
        case PrimitiveKind::Int32S: { RETURN_INTEGER_32_SWAP(int32_t); } break;
        case PrimitiveKind::UInt32: { RETURN_INTEGER_32(uint32_t); } break;
        case PrimitiveKind::UInt32S: { RETURN_INTEGER_32_SWAP(uint32_t); } break;
        case PrimitiveKind::Int64: { RETURN_INTEGER_64(int64_t); } break;
        case PrimitiveKind::Int64S: { RETURN_INTEGER_64_SWAP(int64_t); } break;
        case PrimitiveKind::UInt64: { RETURN_INTEGER_64(uint64_t); } break;
        case PrimitiveKind::UInt64S: { RETURN_INTEGER_64_SWAP(uint64_t); } break;
        case PrimitiveKind::String: {
            const char *str;
            if (!PushString(value, 1, &str)) [[unlikely]]
                return;

            out_reg->r0 = (uint32_t)str;
        } break;
        case PrimitiveKind::String16: {
            const char16_t *str16;
            if (!PushString16(value, 1, &str16)) [[unlikely]]
                return;

            out_reg->r0 = (uint32_t)str16;
        } break;
        case PrimitiveKind::String32: {
            const char32_t *str32;
            if (!PushString32(value, 1, &str32)) [[unlikely]]
                return;

            out_reg->r0 = (uint32_t)str32;
        } break;
        case PrimitiveKind::Pointer: {
            uint8_t *ptr;

            if (CheckValueTag(instance, value, type->ref.marker)) {
                ptr = value.As<Napi::External<uint8_t>>().Data();
            } else if (IsObject(value) && (type->ref.type->primitive == PrimitiveKind::Record ||
                                           type->ref.type->primitive == PrimitiveKind::Union)) {
                Napi::Object obj = value.As<Napi::Object>();

                ptr = AllocHeap(type->ref.type->size, 16);

                if (!PushObject(obj, type->ref.type, ptr))
                    return;
            } else if (IsNullOrUndefined(value)) {
                ptr = nullptr;
            } else {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), type->name);
                return;
            }

            out_reg->r0 = (uint32_t)ptr;
        } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            if (!IsObject(value)) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected object", GetValueType(instance, value));
                return;
            }

            Napi::Object obj = value.As<Napi::Object>();

            if (return_ptr) {
                if (!PushObject(obj, type, return_ptr))
                    return;
                out_reg->r0 = (uint32_t)return_ptr;
            } else if (proto->ret.vec_count) {
                PushObject(obj, type, (uint8_t *)&out_reg->d0);
            } else {
                PushObject(obj, type, (uint8_t *)&out_reg->r0);
            }
        } break;
        case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
        case PrimitiveKind::Float32: {
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                return;
            }

            float f = GetNumber<float>(value);
#ifdef __ARM_PCS_VFP
            memcpy(&out_reg->d0, &f, 4);
#else
            memcpy(&out_reg->r0, &f, 4);
#endif
        } break;
        case PrimitiveKind::Float64: {
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                return;
            }

            double d = GetNumber<double>(value);
#ifdef __ARM_PCS_VFP
            out_reg->d0 = d;
#else
            memcpy(&out_reg->r0, &d, 8);
#endif
        } break;
        case PrimitiveKind::Callback: {
            void *ptr;

            if (value.IsFunction()) {
                Napi::Function func2 = value.As<Napi::Function>();

                ptr = ReserveTrampoline(type->ref.proto, func2);
                if (!ptr) [[unlikely]]
                    return;
            } else if (CheckValueTag(instance, value, type->ref.marker)) {
                ptr = value.As<Napi::External<void>>().Data();
            } else if (IsNullOrUndefined(value)) {
                ptr = nullptr;
            } else {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), type->name);
                return;
            }

            out_reg->r0 = (uint32_t)ptr;
        } break;

        case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
    }

#undef RETURN_INTEGER_64_SWAP
#undef RETURN_INTEGER_64
#undef RETURN_INTEGER_32_SWAP
#undef RETURN_INTEGER_32

    err_guard.Disable();
}

void *GetTrampoline(int16_t idx, const FunctionInfo *proto)
{
    bool vec = proto->forward_fp || IsFloat(proto->ret.type);
    return Trampolines[idx][vec];
}

}

#endif
