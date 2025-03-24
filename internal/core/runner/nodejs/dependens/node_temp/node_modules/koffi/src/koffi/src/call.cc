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

#include "src/core/libcc/libcc.hh"
#include "call.hh"
#include "ffi.hh"
#include "util.hh"

#include <napi.h>

namespace RG {

struct RelayContext {
    CallData *call;
    bool dispose_call;

    Size idx;
    uint8_t *own_sp;
    uint8_t *caller_sp;
    BackRegisters *out_reg;

    std::mutex mutex;
    std::condition_variable cv;
    bool done = false;
};

CallData::CallData(Napi::Env env, InstanceData *instance, InstanceMemory *mem)
    : env(env), instance(instance),
      mem(mem), old_stack_mem(mem->stack), old_heap_mem(mem->heap)
{
    mem->generation += !mem->depth;
    mem->depth++;

    RG_ASSERT(AlignUp(mem->stack.ptr, 16) == mem->stack.ptr);
    RG_ASSERT(AlignUp(mem->stack.end(), 16) == mem->stack.end());
}

CallData::~CallData()
{
    if (!instance)
        return;

    Dispose();
}

void CallData::Dispose()
{
    for (const OutArgument &out: out_arguments) {
        napi_delete_reference(env, out.ref);
    }

    mem->stack = old_stack_mem;
    mem->heap = old_heap_mem;

    if (used_trampolines.len) {
        std::lock_guard<std::mutex> lock(shared.mutex);

        for (Size i = used_trampolines.len - 1; i >= 0; i--) {
            int16_t idx = used_trampolines[i];
            TrampolineInfo *trampoline = &shared.trampolines[idx];

            RG_ASSERT(trampoline->instance == instance);
            RG_ASSERT(!trampoline->func.IsEmpty());

            trampoline->instance = nullptr;
            trampoline->func.Reset();
            trampoline->recv.Reset();

            shared.available.Append(idx);
        }
    }

    instance->temporaries -= mem->temporary;

    if (!--mem->depth) {
        mem->busy = false;

        if (mem->temporary) {
            delete mem;
        }
    }

    instance = nullptr;
}

void CallData::RelaySafe(Size idx, uint8_t *own_sp, uint8_t *caller_sp, bool outside_call, BackRegisters *out_reg)
{
    if (std::this_thread::get_id() != instance->main_thread_id) {
        // JS/V8 is single-threaded, and runs on main_thread_id. Forward the call
        // to the JS event loop.

        RelayContext ctx;

        ctx.call = this;
        ctx.dispose_call = outside_call;
        ctx.idx = idx;
        ctx.own_sp = own_sp;
        ctx.caller_sp = caller_sp;
        ctx.out_reg = out_reg;

        napi_call_threadsafe_function(instance->broker, &ctx, napi_tsfn_blocking);

        // Wait until it executes
        std::unique_lock<std::mutex> lock(ctx.mutex);
        while (!ctx.done) {
            ctx.cv.wait(lock);
        }
    } else {
        Napi::HandleScope scope(env);
        Relay(idx, own_sp, caller_sp, !outside_call, out_reg);
    }
}

void CallData::RelayAsync(napi_env, napi_value, void *, void *udata)
{
    RelayContext *ctx = (RelayContext *)udata;

    ctx->call->Relay(ctx->idx, ctx->own_sp, ctx->caller_sp, false, ctx->out_reg);

    if (ctx->dispose_call) {
        ctx->call->Dispose();
    }

    // We're done!
    std::lock_guard<std::mutex> lock(ctx->mutex);
    ctx->done = true;
    ctx->cv.notify_one();
}

bool CallData::PushString(Napi::Value value, int directions, const char **out_str)
{
    if (value.IsString()) {
        if (directions & 2) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected [string]", GetValueType(instance, value));
            return false;
        }

        PushStringValue(value, out_str);
        return true;
    } else if (IsNullOrUndefined(value)) {
        *out_str = nullptr;
        return true;
    } else if (value.IsArray()) {
        Napi::Array array = value.As<Napi::Array>();

        if (!(directions & 2)) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected string", GetValueType(instance, value));
            return false;
        }
        if (array.Length() != 1) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected [string]", GetValueType(instance, value));
            return false;
        }

        value = array[0u];

        if (!value.IsString()) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected [string]", GetValueType(instance, array[0u]));
            return false;
        }

        Size len = PushStringValue(value, out_str);
        if (len < 0) [[unlikely]]
            return false;

        // Create array type
        TypeInfo *type;
        {
            type = AllocateOne<TypeInfo>(&call_alloc, (int)AllocFlag::Zero);

            type->name = "<temporary>";

            type->primitive = PrimitiveKind::Array;
            type->align = 1;
            type->size = (int32_t)len;
            type->ref.type = instance->char_type;
            type->hint = ArrayHint::String;
        }

        // Prepare output argument
        {
            OutArgument *out = out_arguments.AppendDefault();

            napi_status status = napi_create_reference(env, array, 1, &out->ref);
            RG_ASSERT(status == napi_ok);

            out->kind = OutArgument::Kind::Array;
            out->ptr = (const uint8_t *)*out_str;
            out->type = type;
        }

        return true;
    } else {
        return PushPointer(value, instance->str_type, directions, (void **)out_str);
    }
}

Size CallData::PushStringValue(Napi::Value value, const char **out_str)
{
    Span<char> buf;
    size_t len = 0;
    napi_status status;

    buf.ptr = (char *)mem->heap.ptr;
    buf.len = std::max((Size)0, mem->heap.len - Kibibytes(32));

    status = napi_get_value_string_utf8(env, value, buf.ptr, (size_t)buf.len, &len);
    RG_ASSERT(status == napi_ok);

    len++;

    if (len < (size_t)buf.len) [[likely]] {
        mem->heap.ptr += (Size)len;
        mem->heap.len -= (Size)len;
    } else {
        status = napi_get_value_string_utf8(env, value, nullptr, 0, &len);
        RG_ASSERT(status == napi_ok);

        len++;
        buf = AllocateSpan<char>(&call_alloc, (Size)len);

        status = napi_get_value_string_utf8(env, value, buf.ptr, (size_t)buf.len, &len);
        RG_ASSERT(status == napi_ok);

        len++;
    }

    *out_str = buf.ptr;
    return (Size)len;
}

bool CallData::PushString16(Napi::Value value, int directions, const char16_t **out_str16)
{
    if (value.IsString()) {
        if (directions & 2) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected string", GetValueType(instance, value));
            return false;
        }

        PushString16Value(value, out_str16);
        return true;
    } else if (IsNullOrUndefined(value)) {
        *out_str16 = nullptr;
        return true;
    }  else if (value.IsArray()) {
        Napi::Array array = value.As<Napi::Array>();

        if (!(directions & 2)) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected string", GetValueType(instance, value));
            return false;
        }
        if (array.Length() != 1) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected [string]", GetValueType(instance, value));
            return false;
        }

        value = array[0u];

        if (!value.IsString()) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected [string]", GetValueType(instance, array[0u]));
            return false;
        }

        Size len = PushString16Value(value, out_str16);
        if (len < 0) [[unlikely]]
            return false;

        // Create array type
        TypeInfo *type;
        {
            type = AllocateOne<TypeInfo>(&call_alloc, (int)AllocFlag::Zero);

            type->name = "<temporary>";

            type->primitive = PrimitiveKind::Array;
            type->align = 2;
            type->size = (int32_t)(len * 2);
            type->ref.type = instance->char16_type;
            type->hint = ArrayHint::String;
        }

        // Prepare output argument
        {
            OutArgument *out = out_arguments.AppendDefault();

            napi_status status = napi_create_reference(env, array, 1, &out->ref);
            RG_ASSERT(status == napi_ok);

            out->kind = OutArgument::Kind::Array;
            out->ptr = (const uint8_t *)*out_str16;
            out->type = type;
        }

        return true;
    } else {
        return PushPointer(value, instance->str16_type, directions, (void **)out_str16);
    }
}

Size CallData::PushString16Value(Napi::Value value, const char16_t **out_str16)
{
    Span<char16_t> buf;
    size_t len = 0;
    napi_status status;

    mem->heap.ptr = AlignUp(mem->heap.ptr, 2);
    buf.ptr = (char16_t *)mem->heap.ptr;
    buf.len = std::max((Size)0, mem->heap.len - Kibibytes(32)) / 2;

    status = napi_get_value_string_utf16(env, value, buf.ptr, (size_t)buf.len, &len);
    RG_ASSERT(status == napi_ok);

    len++;

    if (len < (size_t)buf.len) [[likely]] {
        mem->heap.ptr += (Size)len * 2;
        mem->heap.len -= (Size)len * 2;
    } else {
        status = napi_get_value_string_utf16(env, value, nullptr, 0, &len);
        RG_ASSERT(status == napi_ok);

        len++;
        buf = AllocateSpan<char16_t>(&call_alloc, (Size)len);

        status = napi_get_value_string_utf16(env, value, buf.ptr, (size_t)buf.len, &len);
        RG_ASSERT(status == napi_ok);

        len++;
    }

    *out_str16 = buf.ptr;
    return (Size)len;
}

bool CallData::PushString32(Napi::Value value, int directions, const char32_t **out_str32)
{
    if (value.IsString()) {
        if (directions & 2) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected string", GetValueType(instance, value));
            return false;
        }

        PushString32Value(value, out_str32);
        return true;
    } else if (IsNullOrUndefined(value)) {
        *out_str32 = nullptr;
        return true;
    }  else if (value.IsArray()) {
        Napi::Array array = value.As<Napi::Array>();

        if (!(directions & 2)) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected string", GetValueType(instance, value));
            return false;
        }
        if (array.Length() != 1) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected [string]", GetValueType(instance, value));
            return false;
        }

        value = array[0u];

        if (!value.IsString()) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected [string]", GetValueType(instance, array[0u]));
            return false;
        }

        Size len = PushString32Value(value, out_str32);
        if (len < 0) [[unlikely]]
            return false;

        // Create array type
        TypeInfo *type;
        {
            type = AllocateOne<TypeInfo>(&call_alloc, (int)AllocFlag::Zero);

            type->name = "<temporary>";

            type->primitive = PrimitiveKind::Array;
            type->align = 4;
            type->size = (int32_t)(len * 4);
            type->ref.type = instance->char32_type;
            type->hint = ArrayHint::String;
        }

        // Prepare output argument
        {
            OutArgument *out = out_arguments.AppendDefault();

            napi_status status = napi_create_reference(env, array, 1, &out->ref);
            RG_ASSERT(status == napi_ok);

            out->kind = OutArgument::Kind::Array;
            out->ptr = (const uint8_t *)*out_str32;
            out->type = type;
        }

        return true;
    } else {
        return PushPointer(value, instance->str32_type, directions, (void **)out_str32);
    }
}

Size CallData::PushString32Value(Napi::Value value, const char32_t **out_str32)
{
    Span<char32_t> buf;

    Span<const char16_t> buf16;
    buf16.len = PushString16Value(value, &buf16.ptr);
    if (buf16.len < 0) [[unlikely]]
        return -1;

    mem->heap.ptr = AlignUp(mem->heap.ptr, 4);
    buf.ptr = (char32_t *)mem->heap.ptr;
    buf.len = std::max((Size)0, mem->heap.len - Kibibytes(32)) / 4;

    if (buf16.len < buf.len) [[likely]] {
        mem->heap.ptr += buf16.len * 4;
        mem->heap.len -= buf16.len * 4;
    } else {
        buf = AllocateSpan<char32_t>(&call_alloc, buf16.len);
    }

    Size j = 0;
    for (Size i = 0; i < buf16.len; i++) {
        char32_t uc = buf16[i];

        if (uc >= 0xD800 && uc <= 0xDBFF) {
            char16_t uc2 = buf16.ptr[++i];

            if (uc2 >= 0xDC00 && uc2 <= 0xDFFF) {
                uc = ((uc - 0xD800) << 10) + (uc2 - 0xDC00) + 0x10000u;
            } else {
                uc = '?';
            }
        } else if (uc >= 0xDC00 && uc <= 0xDFFF) {
            uc = '?';
        }

        buf[j++] = uc;
    }

    *out_str32 = buf.ptr;
    return j;
}

bool CallData::PushObject(Napi::Object obj, const TypeInfo *type, uint8_t *origin)
{
    RG_ASSERT(IsObject(obj));
    RG_ASSERT(type->primitive == PrimitiveKind::Record ||
              type->primitive == PrimitiveKind::Union);

    Span<const RecordMember> members = {};

    if (type->primitive == PrimitiveKind::Record) {
        members = type->members;
    } else if (type->primitive == PrimitiveKind::Union) {
        if (CheckValueTag(instance, obj, &MagicUnionMarker)) {
            MagicUnion *u = MagicUnion::Unwrap(obj);
            const uint8_t *raw = u->GetRaw();

            if (u->GetType() != type) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Expected union type %1, got %2", type->name, u->GetType()->name);
                return false;
            }

            // Fast path: encoded value already exists, just copy!
            if (raw) {
                memcpy(origin, raw, type->size);
                return true;
            }

            members.ptr = u->GetMember();
            members.len = 1;

            if (!members.ptr) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Cannot use ambiguous empty union");
                return false;
            }
        } else {
            Napi::Array properties = GetOwnPropertyNames(obj);

            if (properties.Length() != 1 || !properties.Get(0u).IsString()) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Expected object with single property name for union");
                return false;
            }

            std::string property = properties.Get(0u).As<Napi::String>();

            members.ptr = std::find_if(type->members.begin(), type->members.end(),
                                       [&](const RecordMember &member) { return TestStr(property.c_str(), member.name); });
            members.len = 1;

            if (members.ptr == type->members.end()) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Unknown member %1 in union type %2", property.c_str(), type->name);
                return false;
            }
        }
    } else {
        RG_UNREACHABLE();
    }

    memset(origin, 0, type->size);

    for (Size i = 0; i < members.len; i++) {
        const RecordMember &member = members[i];
        Napi::Value value = obj.Get(member.name);

        if (value.IsUndefined())
            continue;

        uint8_t *dest = origin + member.offset;

        switch (member.type->primitive) {
            case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

            case PrimitiveKind::Bool: {
                if (!value.IsBoolean()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected boolean", GetValueType(instance, value));
                    return false;
                }

                bool b = value.As<Napi::Boolean>();
                *(bool *)dest = b;
            } break;
            case PrimitiveKind::Int8: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                int8_t v = GetNumber<int8_t>(value);
                *(int8_t *)dest = v;
            } break;
            case PrimitiveKind::UInt8: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                uint8_t v = GetNumber<uint8_t>(value);
                *(uint8_t *)dest = v;
            } break;
            case PrimitiveKind::Int16: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                int16_t v = GetNumber<int16_t>(value);
                *(int16_t *)dest = v;
            } break;
            case PrimitiveKind::Int16S: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                int16_t v = GetNumber<int16_t>(value);
                *(int16_t *)dest = ReverseBytes(v);
            } break;
            case PrimitiveKind::UInt16: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                uint16_t v = GetNumber<uint16_t>(value);
                *(uint16_t *)dest = v;
            } break;
            case PrimitiveKind::UInt16S: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                uint16_t v = GetNumber<uint16_t>(value);
                *(uint16_t *)dest = ReverseBytes(v);
            } break;
            case PrimitiveKind::Int32: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                int32_t v = GetNumber<int32_t>(value);
                *(int32_t *)dest = v;
            } break;
            case PrimitiveKind::Int32S: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                int32_t v = GetNumber<int32_t>(value);
                *(int32_t *)dest = ReverseBytes(v);
            } break;
            case PrimitiveKind::UInt32: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                uint32_t v = GetNumber<uint32_t>(value);
                *(uint32_t *)dest = v;
            } break;
            case PrimitiveKind::UInt32S: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                uint32_t v = GetNumber<uint32_t>(value);
                *(uint32_t *)dest = ReverseBytes(v);
            } break;
            case PrimitiveKind::Int64: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                int64_t v = GetNumber<int64_t>(value);
                *(int64_t *)dest = v;
            } break;
            case PrimitiveKind::Int64S: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                int64_t v = GetNumber<int64_t>(value);
                *(int64_t *)dest = ReverseBytes(v);
            } break;
            case PrimitiveKind::UInt64: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                uint64_t v = GetNumber<uint64_t>(value);
                *(uint64_t *)dest = v;
            } break;
            case PrimitiveKind::UInt64S: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                uint64_t v = GetNumber<uint64_t>(value);
                *(uint64_t *)dest = ReverseBytes(v);
            } break;
            case PrimitiveKind::String: {
                const char *str;
                if (!PushString(value, 1, &str)) [[unlikely]]
                    return false;

                *(const char **)dest = str;
            } break;
            case PrimitiveKind::String16: {
                const char16_t *str16;
                if (!PushString16(value, 1, &str16)) [[unlikely]]
                    return false;

                *(const char16_t **)dest = str16;
            } break;
            case PrimitiveKind::String32: {
                const char32_t *str32;
                if (!PushString32(value, 1, &str32)) [[unlikely]]
                    return false;

                *(const char32_t **)dest = str32;
            } break;
            case PrimitiveKind::Pointer: {
                void *ptr;
                if (!PushPointer(value, member.type, 1, &ptr)) [[unlikely]]
                    return false;

                *(void **)dest = ptr;
            } break;
            case PrimitiveKind::Record:
            case PrimitiveKind::Union: {
                if (!IsObject(value)) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected object", GetValueType(instance, value));
                    return false;
                }

                Napi::Object obj2 = value.As<Napi::Object>();
                if (!PushObject(obj2, member.type, dest))
                    return false;
            } break;
            case PrimitiveKind::Array: {
                if (value.IsArray()) {
                    Napi::Array array = value.As<Napi::Array>();
                    Size len = (Size)member.type->size / member.type->ref.type->size;

                    if (!PushNormalArray(array, len, member.type, dest))
                        return false;
                } else if (IsRawBuffer(value)) {
                    Span<const uint8_t> buffer = GetRawBuffer(value);
                    PushBuffer(buffer, member.type->size, member.type, dest);
                } else if (value.IsString()) {
                    if (!PushStringArray(value, member.type, dest))
                        return false;
                } else {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected array", GetValueType(instance, value));
                    return false;
                }
            } break;
            case PrimitiveKind::Float32: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                float f = GetNumber<float>(value);
                *(float *)dest = f;
            } break;
            case PrimitiveKind::Float64: {
                if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                    return false;
                }

                double d = GetNumber<double>(value);
                *(double *)dest = d;
            } break;
            case PrimitiveKind::Callback: {
                void *ptr;

                if (value.IsFunction()) {
                    Napi::Function func = value.As<Napi::Function>();

                    ptr = ReserveTrampoline(member.type->ref.proto, func);
                    if (!ptr) [[unlikely]]
                        return false;
                } else if (CheckValueTag(instance, value, member.type->ref.marker)) {
                    Napi::External<void> external = value.As<Napi::External<void>>();
                    ptr = external.Data();
                } else if (IsNullOrUndefined(value)) {
                    ptr = nullptr;
                } else {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), member.type->name);
                    return false;
                }

                *(void **)dest = ptr;
            } break;

            case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
        }
    }

    return true;
}

bool CallData::PushNormalArray(Napi::Array array, Size len, const TypeInfo *type, uint8_t *origin)
{
    RG_ASSERT(array.IsArray());

    const TypeInfo *ref = type->ref.type;

    if (array.Length() != (size_t)len) [[unlikely]] {
        ThrowError<Napi::Error>(env, "Expected array of length %1, got %2", len, array.Length());
        return false;
    }

    Size offset = 0;

#define PUSH_ARRAY(Check, Expected, GetCode) \
        do { \
            for (Size i = 0; i < len; i++) { \
                Napi::Value value = array[(uint32_t)i]; \
                 \
                offset = AlignLen(offset, ref->align); \
                uint8_t *dest = origin + offset; \
                 \
                if (!(Check)) [[unlikely]] { \
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), (Expected)); \
                    return false; \
                } \
                 \
                GetCode \
                 \
                offset += ref->size; \
            } \
        } while (false)

    switch (ref->primitive) {
        case PrimitiveKind::Void: {
            ThrowError<Napi::TypeError>(env, "Ambigous parameter type %1, use koffi.as(value, type)", type->name); \
            return false;
        } break;

        case PrimitiveKind::Bool: {
            PUSH_ARRAY(value.IsBoolean(), "boolean", {
                bool b = value.As<Napi::Boolean>();
                *(bool *)dest = b;
            });
        } break;
        case PrimitiveKind::Int8: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                int8_t v = GetNumber<int8_t>(value);
                *(int8_t *)dest = v;
            });
        } break;
        case PrimitiveKind::UInt8: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                uint8_t v = GetNumber<uint8_t>(value);
                *(uint8_t *)dest = v;
            });
        } break;
        case PrimitiveKind::Int16: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                int16_t v = GetNumber<int16_t>(value);
                *(int16_t *)dest = v;
            });
        } break;
        case PrimitiveKind::Int16S: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                int16_t v = GetNumber<int16_t>(value);
                *(int16_t *)dest = ReverseBytes(v);
            });
        } break;
        case PrimitiveKind::UInt16: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                uint16_t v = GetNumber<uint16_t>(value);
                *(uint16_t *)dest = v;
            });
        } break;
        case PrimitiveKind::UInt16S: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                uint16_t v = GetNumber<uint16_t>(value);
                *(uint16_t *)dest = ReverseBytes(v);
            });
        } break;
        case PrimitiveKind::Int32: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                int32_t v = GetNumber<int32_t>(value);
                *(int32_t *)dest = v;
            });
        } break;
        case PrimitiveKind::Int32S: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                int32_t v = GetNumber<int32_t>(value);
                *(int32_t *)dest = ReverseBytes(v);
            });
        } break;
        case PrimitiveKind::UInt32: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                uint32_t v = GetNumber<uint32_t>(value);
                *(uint32_t *)dest = v;
            });
        } break;
        case PrimitiveKind::UInt32S: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                uint32_t v = GetNumber<uint32_t>(value);
                *(uint32_t *)dest = ReverseBytes(v);
            });
        } break;
        case PrimitiveKind::Int64: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                int64_t v = GetNumber<int64_t>(value);
                *(int64_t *)dest = v;
            });
        } break;
        case PrimitiveKind::Int64S: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                int64_t v = GetNumber<int64_t>(value);
                *(int64_t *)dest = ReverseBytes(v);
            });
        } break;
        case PrimitiveKind::UInt64: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                uint64_t v = GetNumber<uint64_t>(value);
                *(uint64_t *)dest = v;
            });
        } break;
        case PrimitiveKind::UInt64S: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                uint64_t v = GetNumber<uint64_t>(value);
                *(uint64_t *)dest = ReverseBytes(v);
            });
        } break;
        case PrimitiveKind::String: {
            PUSH_ARRAY(true, "string", {
                const char *str;
                if (!PushString(value, 1, &str)) [[unlikely]]
                    return false;

                *(const char **)dest = str;
            });
        } break;
        case PrimitiveKind::String16: {
            PUSH_ARRAY(true, "string", {
                const char16_t *str16;
                if (!PushString16(value, 1, &str16)) [[unlikely]]
                    return false;

                *(const char16_t **)dest = str16;
            });
        } break;
        case PrimitiveKind::String32: {
            PUSH_ARRAY(true, "string", {
                const char32_t *str32;
                if (!PushString32(value, 1, &str32)) [[unlikely]]
                    return false;

                *(const char32_t **)dest = str32;
            });
        } break;
        case PrimitiveKind::Pointer: {
            PUSH_ARRAY(true, ref->name, {
                void *ptr;
                if (!PushPointer(value, ref, 1, &ptr)) [[unlikely]]
                    return false;

                *(const void **)dest = ptr;
            });
        } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            PUSH_ARRAY(IsObject(value), "object", {
                Napi::Object obj2 = value.As<Napi::Object>();
                if (!PushObject(obj2, ref, dest))
                    return false;
            });
        } break;
        case PrimitiveKind::Array: {
            for (Size i = 0; i < len; i++) {
                Napi::Value value = array[(uint32_t)i];

                offset = AlignLen(offset, ref->align);

                uint8_t *dest = origin + offset;

                if (value.IsArray()) {
                    Napi::Array array2 = value.As<Napi::Array>();
                    Size len2 = (Size)ref->size / ref->ref.type->size;

                    if (!PushNormalArray(array2, len2, ref, dest))
                        return false;
                } else if (IsRawBuffer(value)) {
                    Span<const uint8_t> buffer = GetRawBuffer(value);
                    PushBuffer(buffer, ref->size, ref, dest);
                } else if (value.IsString()) {
                    if (!PushStringArray(value, ref, dest))
                        return false;
                } else {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected array", GetValueType(instance, value));
                    return false;
                }

                offset += ref->size;
            }
        } break;
        case PrimitiveKind::Float32: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                float f = GetNumber<float>(value);
                *(float *)dest = f;
            });
        } break;
        case PrimitiveKind::Float64: {
            PUSH_ARRAY(value.IsNumber() || value.IsBigInt(), "number", {
                double d = GetNumber<double>(value);
                *(double *)dest = d;
            });
        } break;
        case PrimitiveKind::Callback: {
            for (Size i = 0; i < len; i++) {
                Napi::Value value = array[(uint32_t)i];

                offset = AlignLen(offset, ref->align);

                uint8_t *dest = origin + offset;

                void *ptr;

                if (value.IsFunction()) {
                    Napi::Function func = value.As<Napi::Function>();

                    ptr = ReserveTrampoline(ref->ref.proto, func);
                    if (!ptr) [[unlikely]]
                        return false;
                } else if (CheckValueTag(instance, value, ref->ref.marker)) {
                    Napi::External<void> external = value.As<Napi::External<void>>();
                    ptr = external.Data();
                } else if (IsNullOrUndefined(value)) {
                    ptr = nullptr;
                } else {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), ref->name);
                    return false;
                }

                *(void **)dest = ptr;

                offset += ref->size;
            }
        } break;

        case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
    }

#undef PUSH_ARRAY

    return true;
}

void CallData::PushBuffer(Span<const uint8_t> buffer, Size size, const TypeInfo *type, uint8_t *origin)
{
    buffer.len = std::min(buffer.len, size);

    // Go fast brrrrrrr :)
    memcpy_safe(origin, buffer.ptr, (size_t)buffer.len);
    memset_safe(origin + buffer.len, 0, (size_t)(size - buffer.len));

#define SWAP(CType) \
        do { \
            CType *data = (CType *)origin; \
            Size len = buffer.len / RG_SIZE(CType); \
             \
            for (Size i = 0; i < len; i++) { \
                data[i] = ReverseBytes(data[i]); \
            } \
        } while (false)

    if (type->primitive == PrimitiveKind::Array || type->primitive == PrimitiveKind::Pointer) {
        const TypeInfo *ref = type->ref.type;

        if (ref->primitive == PrimitiveKind::Int16S || ref->primitive == PrimitiveKind::UInt16S) {
            SWAP(uint16_t);
        } else if (ref->primitive == PrimitiveKind::Int32S || ref->primitive == PrimitiveKind::UInt32S) {
            SWAP(uint32_t);
        } else if (ref->primitive == PrimitiveKind::Int64S || ref->primitive == PrimitiveKind::UInt64S) {
            SWAP(uint64_t);
        }
    }

#undef SWAP
}

bool CallData::PushStringArray(Napi::Value obj, const TypeInfo *type, uint8_t *origin)
{
    RG_ASSERT(obj.IsString());
    RG_ASSERT(type->primitive == PrimitiveKind::Array);

    size_t encoded = 0;

    switch (type->ref.type->primitive) {
        case PrimitiveKind::Int8: {
            napi_status status = napi_get_value_string_utf8(env, obj, (char *)origin, type->size, &encoded);
            RG_ASSERT(status == napi_ok);
        } break;
        case PrimitiveKind::Int16: {
            napi_status status = napi_get_value_string_utf16(env, obj, (char16_t *)origin, type->size / 2, &encoded);
            RG_ASSERT(status == napi_ok);

            encoded *= 2;
        } break;

        default: {
            ThrowError<Napi::TypeError>(env, "Strings cannot be converted to %1 array", type->ref.type->name);
            return false;
        } break;
    }

    memset_safe(origin + encoded, 0, type->size - encoded);

    return true;
}

bool CallData::PushPointer(Napi::Value value, const TypeInfo *type, int directions, void **out_ptr)
{
    if (CheckValueTag(instance, value, &CastMarker)) {
        Napi::External<ValueCast> external = value.As<Napi::External<ValueCast>>();
        ValueCast *cast = external.Data();

        value = cast->ref.Value();
        type = cast->type;
    }

    switch (value.Type()) {
        case napi_undefined:
        case napi_null: {
            *out_ptr = nullptr;
            return true;
        } break;

        case napi_external: {
            RG_ASSERT(type->primitive == PrimitiveKind::Pointer ||
                      type->primitive == PrimitiveKind::String ||
                      type->primitive == PrimitiveKind::String16 ||
                      type->primitive == PrimitiveKind::String32);

            if (!CheckValueTag(instance, value, type->ref.marker) &&
                    !CheckValueTag(instance, value, instance->void_type) &&
                    type->ref.type != instance->void_type) [[unlikely]]
                goto unexpected;

            *out_ptr = value.As<Napi::External<uint8_t>>().Data();
            return true;
        } break;

        case napi_object: {
            uint8_t *ptr = nullptr;

            OutArgument::Kind out_kind;
            Size out_max_len = -1;

            if (value.IsArray()) {
                Napi::Array array = value.As<Napi::Array>();
                Size len = PushIndirectString(array, type->ref.type, &ptr);

                if (len >= 0) {
                    if (!type->ref.type->size && type->ref.type != instance->void_type) [[unlikely]] {
                        ThrowError<Napi::TypeError>(env, "Cannot pass [string] value to %1", type->name);
                        return false;
                    }

                    switch (type->ref.type->size) {
                        default: { out_kind = OutArgument::Kind::String; } break;
                        case 2: { out_kind = OutArgument::Kind::String16; } break;
                        case 4: { out_kind = OutArgument::Kind::String32; } break;
                    }
                    out_max_len = len;
                } else {
                    if (!type->ref.type->size) [[unlikely]] {
                        ThrowError<Napi::TypeError>(env, "Cannot pass %1 value to %2, use koffi.as()",
                                                    type->ref.type != instance->void_type ? "opaque" : "ambiguous", type->name);
                        return false;
                    }

                    Size len = (Size)array.Length();
                    Size size = len * type->ref.type->size;

                    ptr = AllocHeap(size, 16);

                    if (directions & 1) {
                        if (!PushNormalArray(array, len, type, ptr))
                            return false;
                    } else {
                        memset_safe(ptr, 0, size);
                    }

                    out_kind = OutArgument::Kind::Array;
                }
            } else if (IsRawBuffer(value)) {
                Span<uint8_t> buffer = GetRawBuffer(value);

                // We can fast path
                ptr = buffer.ptr;
                directions = 1;

                out_kind = OutArgument::Kind::Buffer;
            } else if (type->ref.type->primitive == PrimitiveKind::Record ||
                       type->ref.type->primitive == PrimitiveKind::Union) [[likely]] {
                Napi::Object obj = value.As<Napi::Object>();
                RG_ASSERT(IsObject(value));

                ptr = AllocHeap(type->ref.type->size, 16);

                if (type->ref.type->primitive == PrimitiveKind::Union &&
                        (directions & 2) && !CheckValueTag(instance, obj, &MagicUnionMarker)) [[unlikely]] {
                    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected union value", GetValueType(instance, obj));
                    return false;
                }

                if (directions & 1) {
                    if (!PushObject(obj, type->ref.type, ptr))
                        return false;
                } else {
                    memset_safe(ptr, 0, type->size);
                }

                out_kind = OutArgument::Kind::Object;
            } else {
                goto unexpected;
            }

            if (directions & 2) {
                OutArgument *out = out_arguments.AppendDefault();

                napi_status status = napi_create_reference(env, value, 1, &out->ref);
                RG_ASSERT(status == napi_ok);

                out->kind = out_kind;
                out->ptr = ptr;
                out->type = type->ref.type;
                out->max_len = out_max_len;
            }

            *out_ptr = ptr;
            return true;
        } break;

        case napi_string: {
            RG_ASSERT(type->primitive == PrimitiveKind::Pointer);

            if (directions & 2) [[unlikely]]
                goto unexpected;

            if (type->ref.type == instance->void_type) {
                PushStringValue(value, (const char **)out_ptr);
                return true;
            } else if (type->ref.type->primitive == PrimitiveKind::Int8) {
                PushStringValue(value, (const char **)out_ptr);
                return true;
            } else if (type->ref.type->primitive == PrimitiveKind::Int16) {
                PushString16Value(value, (const char16_t **)out_ptr);
                return true;
            } else if (type->ref.type->primitive == PrimitiveKind::Int32) {
                PushString32Value(value, (const char32_t **)out_ptr);
                return true;
            } else {
                goto unexpected;
            }
        } break;

        case napi_function: {
            if (type->primitive != PrimitiveKind::Callback) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Cannot pass function to type %1", type->name);
                return false;
            }

            Napi::Function func = value.As<Napi::Function>();

            void *ptr = ReserveTrampoline(type->ref.proto, func);
            if (!ptr) [[unlikely]]
                return false;

            *out_ptr = (void *)ptr;
            return true;
        } break;

        case napi_number: {
            Napi::Number number = value.As<Napi::Number>();
            intptr_t ptr = (intptr_t)number.Int32Value();

            *out_ptr = (void *)ptr;
            return true;
        } break;

        case napi_bigint: {
            Napi::BigInt bigint = value.As<Napi::BigInt>();

            bool lossless;
            intptr_t ptr = (intptr_t)bigint.Int64Value(&lossless);

            *out_ptr = (void *)ptr;
            return true;
        } break;

        default: {} break;
    }

unexpected:
    ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), type->name);
    return false;
}

Size CallData::PushIndirectString(Napi::Array array, const TypeInfo *ref, uint8_t **out_ptr)
{
    if (array.Length() != 1)
        return -1;

    Napi::Value value = array[0u];

    if (!value.IsString())
        return -1;

    if (ref == instance->void_type) {
        return PushStringValue(value, (const char **)out_ptr);
    } else if (ref->primitive == PrimitiveKind::Int8) {
        return PushStringValue(value, (const char **)out_ptr);
    } else if (ref->primitive == PrimitiveKind::Int16) {
        return PushString16Value(value, (const char16_t **)out_ptr);
    } else if (ref->primitive == PrimitiveKind::Int32) {
        return PushString32Value(value, (const char32_t **)out_ptr);
    } else {
        return -1;
    }
}

void *CallData::ReserveTrampoline(const FunctionInfo *proto, Napi::Function func)
{
    if (!InitAsyncBroker(env, instance)) [[unlikely]]
        return nullptr;

    int16_t idx;
    {
        std::lock_guard<std::mutex> lock(shared.mutex);

        if (!shared.available.len) [[unlikely]] {
            ThrowError<Napi::Error>(env, "Too many callbacks are in use (max = %1)", MaxTrampolines);
            return env.Null();
        }
        if (!used_trampolines.Available()) [[unlikely]] {
            ThrowError<Napi::Error>(env, "This call uses too many temporary callbacks (max = %1)", RG_LEN(used_trampolines.data));
            return env.Null();
        }

        idx = shared.available.data[--shared.available.len];
        used_trampolines.Append(idx);
    }

    TrampolineInfo *trampoline = &shared.trampolines[idx];

    trampoline->instance = instance;
    trampoline->proto = proto;
    trampoline->func.Reset(func, 1);
    trampoline->recv.Reset();
    trampoline->generation = (int32_t)mem->generation;

    void *ptr = GetTrampoline(idx, proto);

    return ptr;
}

void CallData::DumpForward(const FunctionInfo *func) const
{
    PrintLn(stderr, "%!..+---- %1 (%2) ----%!0", func->name, CallConventionNames[(int)func->convention]);

    if (func->parameters.len) {
        PrintLn(stderr, "Parameters:");
        for (Size i = 0; i < func->parameters.len; i++) {
            const ParameterInfo &param = func->parameters[i];
            PrintLn(stderr, "  %1 = %2 (%3)", i, param.type->name, FmtMemSize(param.type->size));
        }
    }
    PrintLn(stderr, "Return: %1 (%2)", func->ret.type->name, FmtMemSize(func->ret.type->size));

    Span<const uint8_t> stack = MakeSpan(mem->stack.end(), old_stack_mem.end() - mem->stack.end());
    Span<const uint8_t> heap = MakeSpan(old_heap_mem.ptr, mem->heap.ptr - old_heap_mem.ptr);

    DumpMemory("Stack", stack);
    DumpMemory("Heap", heap);
}

static inline Napi::Value GetReferenceValue(Napi::Env env, napi_ref ref)
{
    napi_value value;

    napi_status status = napi_get_reference_value(env, ref, &value);
    RG_ASSERT(status == napi_ok);

    return Napi::Value(env, value);
}

void CallData::PopOutArguments()
{
    for (const OutArgument &out: out_arguments) {
        Napi::Value value = GetReferenceValue(env, out.ref);
        RG_ASSERT(!value.IsEmpty());

        switch (out.kind) {
            case OutArgument::Kind::Array: {
                RG_ASSERT(value.IsArray());

                Napi::Array array(env, value);
                DecodeNormalArray(array, out.ptr, out.type);
            } break;

            case OutArgument::Kind::Buffer: {
                RG_ASSERT(IsRawBuffer(value));

                Span<uint8_t> buffer = GetRawBuffer(value);
                DecodeBuffer(buffer, out.ptr, out.type);
            } break;

            case OutArgument::Kind::String: {
                Napi::Array array(env, value);

                RG_ASSERT(array.IsArray());
                RG_ASSERT(array.Length() == 1);

                Size len = strnlen((const char *)out.ptr, out.max_len);
                Napi::String str = Napi::String::New(env, (const char *)out.ptr, len);

                array.Set(0u, str);
            } break;

            case OutArgument::Kind::String16: {
                Napi::Array array(env, value);

                RG_ASSERT(array.IsArray());
                RG_ASSERT(array.Length() == 1);

                Size len = NullTerminatedLength((const char16_t *)out.ptr, out.max_len);
                Napi::String str = Napi::String::New(env, (const char16_t *)out.ptr, len);

                array.Set(0u, str);
            } break;

            case OutArgument::Kind::String32: {
                Napi::Array array(env, value);

                RG_ASSERT(array.IsArray());
                RG_ASSERT(array.Length() == 1);

                Size len = NullTerminatedLength((const char32_t *)out.ptr, out.max_len);
                Napi::String str = MakeStringFromUTF32(env, (const char32_t *)out.ptr, len);

                array.Set(0u, str);
            } break;

            case OutArgument::Kind::Object: {
                Napi::Object obj = value.As<Napi::Object>();

                if (CheckValueTag(instance, value, &MagicUnionMarker)) {
                    MagicUnion *u = MagicUnion::Unwrap(obj);
                    u->SetRaw(out.ptr);
                } else {
                    DecodeObject(obj, out.ptr, out.type);
                }
            } break;
        }
    }
}

}
