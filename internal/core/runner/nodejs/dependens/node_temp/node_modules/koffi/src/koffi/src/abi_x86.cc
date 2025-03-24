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

#if defined(__i386__) || defined(_M_IX86)

#include "src/core/libcc/libcc.hh"
#include "ffi.hh"
#include "call.hh"
#include "util.hh"
#ifdef _WIN32
    #include "win32.hh"
#endif

#include <napi.h>

namespace RG {

struct BackRegisters {
    uint32_t eax;
    uint32_t edx;
    union {
        double d;
        float f;
    } x87;
    bool x87_double;
    int ret_pop;
};

extern "C" uint64_t ForwardCallG(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" float ForwardCallF(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" double ForwardCallD(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" uint64_t ForwardCallRG(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" float ForwardCallRF(const void *func, uint8_t *sp, uint8_t **out_old_sp);
extern "C" double ForwardCallRD(const void *func, uint8_t *sp, uint8_t **out_old_sp);

extern "C" napi_value CallSwitchStack(Napi::Function *func, size_t argc, napi_value *argv,
                                      uint8_t *old_sp, Span<uint8_t> *new_stack,
                                      napi_value (*call)(Napi::Function *func, size_t argc, napi_value *argv));

#include "trampolines/prototypes.inc"

bool AnalyseFunction(Napi::Env env, InstanceData *instance, FunctionInfo *func)
{
    if (!func->lib && func->convention != CallConvention::Cdecl &&
                      func->convention != CallConvention::Stdcall) {
        ThrowError<Napi::Error>(env, "Only Cdecl and Stdcall callbacks are supported");
        return false;
    }

    int fast = (func->convention == CallConvention::Fastcall) ? 2 :
               (func->convention == CallConvention::Thiscall) ? 1 : 0;
    func->fast = fast;

    if (func->ret.type->primitive != PrimitiveKind::Record &&
            func->ret.type->primitive != PrimitiveKind::Union) {
        func->ret.trivial = true;
#if defined(_WIN32) || defined(__FreeBSD__) || defined(__OpenBSD__) || defined(__DragonFly__)
    } else {
        func->ret.trivial = IsRegularSize(func->ret.type->size, 8);

    #if defined(__FreeBSD__) || defined(__OpenBSD__) || defined(__DragonFly__)
        if (func->ret.type->members.len == 1) {
            const RecordMember &member = func->ret.type->members[0];

            if (member.type->primitive == PrimitiveKind::Float32) {
                func->ret.fast = 1;
            } else if (member.type->primitive == PrimitiveKind::Float64) {
                func->ret.fast = 2;
            }
        }
    #endif
#endif
    }
#ifndef _WIN32
    if (fast && !func->ret.trivial) {
        func->ret.fast = 1;
        fast--;
    }
#endif

    Size params_size = 0;
    for (ParameterInfo &param: func->parameters) {
        if (fast && param.type->size <= 4) {
            param.fast = 1;
            fast--;
        }

        params_size += std::max(4, AlignLen(param.type->size, 4));
    }
    func->args_size = params_size + 4 * !func->ret.trivial;

    switch (func->convention) {
        case CallConvention::Cdecl: {
            func->decorated_name = Fmt(&instance->str_alloc, "_%1", func->name).ptr;
        } break;
        case CallConvention::Stdcall: {
            RG_ASSERT(!func->variadic);
            func->decorated_name = Fmt(&instance->str_alloc, "_%1@%2", func->name, params_size).ptr;
        } break;
        case CallConvention::Fastcall: {
            RG_ASSERT(!func->variadic);
            func->decorated_name = Fmt(&instance->str_alloc, "@%1@%2", func->name, params_size).ptr;
            func->args_size += 16;
        } break;
        case CallConvention::Thiscall: {
            RG_ASSERT(!func->variadic);
            func->args_size += 16;
        } break;
    }

    return true;
}

bool CallData::Prepare(const FunctionInfo *func, const Napi::CallbackInfo &info)
{
    uint32_t *args_ptr = nullptr;
    uint32_t *fast_ptr = nullptr;

    // Pass return value in register or through memory
    if (!AllocStack(func->args_size, 16, &args_ptr)) [[unlikely]]
        return false;
    if (func->fast) {
        fast_ptr = args_ptr;
        args_ptr += 4;
    }
    if (!func->ret.trivial) {
        return_ptr = AllocHeap(func->ret.type->size, 16);
        *((func->ret.fast ? fast_ptr : args_ptr)++) = (uint32_t)return_ptr;
    }

#define PUSH_INTEGER_32(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            *((param.fast ? fast_ptr : args_ptr)++) = (uint32_t)v; \
        } while (false)
#define PUSH_INTEGER_32_SWAP(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            *((param.fast ? fast_ptr : args_ptr)++) = (uint32_t)ReverseBytes(v); \
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
            *(uint64_t *)args_ptr = (uint64_t)v; \
            args_ptr += 2; \
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
            *(uint64_t *)args_ptr = (uint64_t)ReverseBytes(v); \
            args_ptr += 2; \
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
                *(bool *)((param.fast ? fast_ptr : args_ptr)++) = b;
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

                *(const char **)((param.fast ? fast_ptr : args_ptr)++) = str;
            } break;
            case PrimitiveKind::String16: {
                const char16_t *str16;
                if (!PushString16(value, param.directions, &str16)) [[unlikely]]
                    return false;

                *(const char16_t **)((param.fast ? fast_ptr : args_ptr)++) = str16;
            } break;
            case PrimitiveKind::String32: {
                const char32_t *str32;
                if (!PushString32(value, param.directions, &str32)) [[unlikely]]
                    return false;

                *(const char32_t **)((param.fast ? fast_ptr : args_ptr)++) = str32;
            } break;
            case PrimitiveKind::Pointer: {
                void *ptr;
                if (!PushPointer(value, param.type, param.directions, &ptr)) [[unlikely]]
                    return false;

                *(void **)((param.fast ? fast_ptr : args_ptr)++) = ptr;
            } break;
            case PrimitiveKind::Record:
            case PrimitiveKind::Union: {
                if (!IsObject(value)) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected object", GetValueType(instance, value));
                    return false;
                }

                Napi::Object obj = value.As<Napi::Object>();

                if (param.fast) {
                    uint8_t *ptr = (uint8_t *)(fast_ptr++);
                    if (!PushObject(obj, param.type, ptr))
                        return false;
                } else {
                    uint8_t *ptr = (uint8_t *)args_ptr;
                    if (!PushObject(obj, param.type, ptr))
                        return false;
                    args_ptr = (uint32_t *)AlignUp(ptr + param.type->size, 4);
                }
            } break;
            case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
            case PrimitiveKind::Float32: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                float f = GetNumber<float>(value);
                *(float *)((param.fast ? fast_ptr : args_ptr)++) = f;
            } break;
            case PrimitiveKind::Float64: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                double d = GetNumber<double>(value);
                *(double *)args_ptr = d;
                args_ptr += 2;
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

                *(void **)((param.fast ? fast_ptr : args_ptr)++) = ptr;
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
#ifdef _WIN32
    TEB *teb = GetTEB();

    // Restore previous stack limits at the end
    RG_DEFER_C(exception_list = teb->ExceptionList,
               base = teb->StackBase,
               limit = teb->StackLimit,
               dealloc = teb->DeallocationStack,
               guaranteed = teb->GuaranteedStackBytes,
               stfs = teb->SameTebFlags) {
        teb->ExceptionList = exception_list;
        teb->StackBase = base;
        teb->StackLimit = limit;
        teb->DeallocationStack = dealloc;
        teb->GuaranteedStackBytes = guaranteed;
        teb->SameTebFlags = stfs;

        instance->last_error = teb->LastErrorValue;
    };

    // Adjust stack limits so SEH works correctly
    teb->ExceptionList = (void *)-1; // EXCEPTION_CHAIN_END
    teb->StackBase = mem->stack0.end();
    teb->StackLimit = mem->stack0.ptr;
    teb->DeallocationStack = mem->stack0.ptr;
    teb->GuaranteedStackBytes = 0;
    teb->SameTebFlags &= ~0x200;

    teb->LastErrorValue = instance->last_error;
#endif

#define PERFORM_CALL(Suffix) \
        ([&]() { \
            auto ret = (func->fast ? ForwardCallR ## Suffix(native, new_sp, &old_sp) \
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
        case PrimitiveKind::Callback: { result.u64 = PERFORM_CALL(G); } break;
#if defined(__FreeBSD__) || defined(__OpenBSD__) || defined(__DragonFly__)
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            if (!func->ret.fast) {
                result.u64 = PERFORM_CALL(G);
            } else if (func->ret.fast == 1) {
                result.f = PERFORM_CALL(F);
            } else if (func->ret.fast == 2) {
                result.d = PERFORM_CALL(D);
            } else {
                RG_UNREACHABLE();
            }
        } break;
#else
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: { result.u64 = PERFORM_CALL(G); } break;
#endif
        case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
        case PrimitiveKind::Float32: { result.f = PERFORM_CALL(F); } break;
        case PrimitiveKind::Float64: { result.d = PERFORM_CALL(D); } break;

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

void CallData::Relay(Size idx, uint8_t *, uint8_t *caller_sp, bool switch_stack, BackRegisters *out_reg)
{
    if (env.IsExceptionPending()) [[unlikely]]
        return;

#ifdef _WIN32
    TEB *teb = GetTEB();

    // Restore previous stack limits at the end
    RG_DEFER_C(base = teb->StackBase,
               limit = teb->StackLimit,
               dealloc = teb->DeallocationStack) {
        teb->StackBase = base;
        teb->StackLimit = limit;
        teb->DeallocationStack = dealloc;
    };

    // Adjust stack limits so SEH works correctly
    teb->StackBase = instance->main_stack_max;
    teb->StackLimit = instance->main_stack_min;
    teb->DeallocationStack = instance->main_stack_min;
#endif

    const TrampolineInfo &trampoline = shared.trampolines[idx];

    const FunctionInfo *proto = trampoline.proto;
    Napi::Function func = trampoline.func.Value();

    uint32_t *args_ptr = (uint32_t *)caller_sp;

    uint8_t *return_ptr = !proto->ret.trivial ? (uint8_t *)args_ptr[0] : nullptr;
    args_ptr += !proto->ret.trivial;

    if (proto->convention == CallConvention::Stdcall) {
        out_reg->ret_pop = (int)proto->args_size;
    } else {
#ifdef _WIN32
        out_reg->ret_pop = 0;
#else
        out_reg->ret_pop = return_ptr ? 4 : 0;
#endif
    }

    RG_DEFER_N(err_guard) {
        int pop = out_reg->ret_pop;
        memset(out_reg, 0, RG_SIZE(*out_reg));
        out_reg->x87_double = true;
        out_reg->ret_pop = pop;
    };

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
                bool b = *(bool *)(args_ptr++);

                Napi::Value arg = Napi::Boolean::New(env, b);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int8: {
                double d = (double)*(int8_t *)(args_ptr++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt8: {
                double d = (double)*(uint8_t *)(args_ptr++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int16: {
                double d = (double)*(int16_t *)(args_ptr++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int16S: {
                int16_t v = *(int16_t *)(args_ptr++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt16: {
                double d = (double)*(uint16_t *)(args_ptr++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt16S: {
                uint16_t v = *(uint16_t *)(args_ptr++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int32: {
                double d = (double)*(int32_t *)(args_ptr++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int32S: {
                int32_t v = *(int32_t *)(args_ptr++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt32: {
                double d = (double)*(uint32_t *)(args_ptr++);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt32S: {
                uint32_t v = *(uint32_t *)(args_ptr++);
                double d = (double)ReverseBytes(v);

                Napi::Value arg = Napi::Number::New(env, d);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int64: {
                int64_t v = *(int64_t *)args_ptr;
                args_ptr += 2;

                Napi::Value arg = NewBigInt(env, v);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Int64S: {
                int64_t v = *(int64_t *)args_ptr;
                args_ptr += 2;

                Napi::Value arg = NewBigInt(env, ReverseBytes(v));
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt64: {
                uint64_t v = *(uint64_t *)args_ptr;
                args_ptr += 2;

                Napi::Value arg = NewBigInt(env, v);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::UInt64S: {
                uint64_t v = *(uint64_t *)args_ptr;
                args_ptr += 2;

                Napi::Value arg = NewBigInt(env, ReverseBytes(v));
                arguments.Append(arg);
            } break;
            case PrimitiveKind::String: {
                const char *str = *(const char **)(args_ptr++);

                Napi::Value arg = str ? Napi::String::New(env, str) : env.Null();
                arguments.Append(arg);

                if (param.type->dispose) {
                    param.type->dispose(env, param.type, str);
                }
            } break;
            case PrimitiveKind::String16: {
                const char16_t *str16 = *(const char16_t **)(args_ptr++);

                Napi::Value arg = str16 ? Napi::String::New(env, str16) : env.Null();
                arguments.Append(arg);

                if (param.type->dispose) {
                    param.type->dispose(env, param.type, str16);
                }
            } break;
            case PrimitiveKind::String32: {
                const char32_t *str32 = *(const char32_t **)(args_ptr++);

                Napi::Value arg = str32 ? MakeStringFromUTF32(env, str32) : env.Null();
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Pointer:
            case PrimitiveKind::Callback: {
                void *ptr2 = *(void **)(args_ptr++);

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
                RG_ASSERT(!param.fast);

                uint8_t *ptr = (uint8_t *)args_ptr;

                Napi::Object obj2 = DecodeObject(env, ptr, param.type);
                arguments.Append(obj2);

                args_ptr = (uint32_t *)AlignUp(ptr + param.type->size, 4);
            } break;
            case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
            case PrimitiveKind::Float32: {
                float f = *(float *)(args_ptr++);

                Napi::Value arg = Napi::Number::New(env, (double)f);
                arguments.Append(arg);
            } break;
            case PrimitiveKind::Float64: {
                double d = *(double *)args_ptr;
                args_ptr += 2;

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
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            out_reg->eax = (uint32_t)v; \
        } while (false)
#define RETURN_INTEGER_32_SWAP(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            out_reg->eax = (uint32_t)ReverseBytes(v); \
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
            out_reg->eax = (uint32_t)((uint64_t)v >> 32); \
            out_reg->edx = (uint32_t)((uint64_t)v & 0xFFFFFFFFu); \
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
            out_reg->eax = (uint32_t)((uint64_t)v >> 32); \
            out_reg->edx = (uint32_t)((uint64_t)v & 0xFFFFFFFFu); \
        } while (false)

    switch (type->primitive) {
        case PrimitiveKind::Void: {} break;
        case PrimitiveKind::Bool: {
            if (!value.IsBoolean()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected boolean", GetValueType(instance, value));
                return;
            }

            bool b = value.As<Napi::Boolean>();
            out_reg->eax = (uint32_t)b;
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

            out_reg->eax = (uint32_t)str;
        } break;
        case PrimitiveKind::String16: {
            const char16_t *str16;
            if (!PushString16(value, 1, &str16)) [[unlikely]]
                return;

            out_reg->eax = (uint32_t)str16;
        } break;
        case PrimitiveKind::String32: {
            const char32_t *str32;
            if (!PushString32(value, 1, &str32)) [[unlikely]]
                return;

            out_reg->eax = (uint32_t)str32;
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

            out_reg->eax = (uint32_t)ptr;
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
                out_reg->eax = (uint32_t)return_ptr;
            } else {
                PushObject(obj, type, (uint8_t *)&out_reg->eax);
            }
        } break;
        case PrimitiveKind::Array: { RG_UNREACHABLE(); } break;
        case PrimitiveKind::Float32: {
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                return;
            }

            out_reg->x87.f = GetNumber<float>(value);
            out_reg->x87_double = false;
        } break;
        case PrimitiveKind::Float64: {
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                return;
            }

            out_reg->x87.d = GetNumber<double>(value);
            out_reg->x87_double = true;
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

            out_reg->eax = (uint32_t)ptr;
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
    bool x87 = IsFloat(proto->ret.type);
    return Trampolines[idx][x87];
}

}

#endif
