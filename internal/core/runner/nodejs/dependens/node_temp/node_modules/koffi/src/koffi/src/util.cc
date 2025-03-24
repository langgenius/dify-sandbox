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

// Value does not matter, the tag system uses memory addresses
const napi_type_tag TypeInfoMarker = { 0x1cc449675b294374, 0xbb13a50e97dcb017 };
const napi_type_tag CastMarker = { 0x77f459614a0a412f, 0x80b3dda1341dc8df };
const napi_type_tag MagicUnionMarker = { 0x5eaf2245526a4c7d, 0x8c86c9ee2b96ffc8 };

Napi::Function MagicUnion::InitClass(Napi::Env env, const TypeInfo *type)
{
    RG_ASSERT(type->primitive == PrimitiveKind::Union);

    // node-addon-api wants std::vector
    std::vector<Napi::ClassPropertyDescriptor<MagicUnion>> properties; 
    properties.reserve(type->members.len);

    for (Size i = 0; i < type->members.len; i++) {
        const RecordMember &member = type->members[i];

        napi_property_attributes attr = (napi_property_attributes)(napi_writable | napi_enumerable);
        Napi::ClassPropertyDescriptor<MagicUnion> prop = InstanceAccessor(member.name, &MagicUnion::Getter,
                                                                          &MagicUnion::Setter, attr, (void *)i);

        properties.push_back(prop);
    }

    Napi::Function constructor = DefineClass(env, type->name, properties, (void *)type);
    return constructor;
}

MagicUnion::MagicUnion(const Napi::CallbackInfo &info)
    : Napi::ObjectWrap<MagicUnion>(info), type((const TypeInfo *)info.Data())
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    active_symbol = instance->active_symbol;
}

void MagicUnion::SetRaw(const uint8_t *ptr)
{
    raw.RemoveFrom(0);
    raw.Append(MakeSpan(ptr, type->size));

    Value().Set(active_symbol, Env().Undefined());
    active_idx = -1;
}

Napi::Value MagicUnion::Getter(const Napi::CallbackInfo &info)
{
    Size idx = (Size)info.Data();
    const RecordMember &member = type->members[idx];

    Napi::Value value;

    if (idx == active_idx) {
        value = Value().Get(active_symbol);
    } else {
        Napi::Env env = info.Env();

        if (!raw.len) [[unlikely]] {
            ThrowError<Napi::Error>(env, "Cannont convert %1 union value", active_idx < 0 ? "empty" : "assigned");
            return env.Null();
        }

        value = Decode(env, raw.ptr, member.type);

        Value().Set(active_symbol, value);
        active_idx = idx;
    }

    RG_ASSERT(!value.IsEmpty());
    return value;
}

void MagicUnion::Setter(const Napi::CallbackInfo &info, const Napi::Value &value)
{
    Size idx = (Size)info.Data();

    Value().Set(active_symbol, value);
    active_idx = idx;

    raw.Clear();
}

const TypeInfo *ResolveType(Napi::Value value, int *out_directions)
{
    Napi::Env env = value.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (value.IsString()) {
        std::string str = value.As<Napi::String>();

        // Quick path for known types (int, float *, etc.)
        const TypeInfo *type = instance->types_map.FindValue(str.c_str(), nullptr);

        if (!type || (type->flags & (int)TypeFlag::IsIncomplete)) {
            type = ResolveType(env, str.c_str(), out_directions);

            if (!type) {
                if (!env.IsExceptionPending()) {
                    ThrowError<Napi::TypeError>(env, "Unknown or invalid type name '%1'", str.c_str());
                }
                return nullptr;
            }

            // Cache for quick future access
            bool inserted;
            auto bucket = instance->types_map.TrySetDefault(str.c_str(), &inserted);

            if (inserted) {
                bucket->key = DuplicateString(str.c_str(), &instance->str_alloc).ptr;
                bucket->value = type;
            }
        } else if (out_directions) {
            *out_directions = 1;
        }

        return type;
    } else if (CheckValueTag(instance, value, &TypeInfoMarker)) {
        Napi::External<TypeInfo> external = value.As<Napi::External<TypeInfo>>();
        const TypeInfo *raw = external.Data();

        const TypeInfo *type = AlignDown(raw, 4);
        RG_ASSERT(type);

        if (out_directions) {
            Size delta = (uint8_t *)raw - (uint8_t *)type;
            *out_directions = 1 + (int)delta;
        }
        return type;
    } else {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value as type specifier, expected string or type", GetValueType(instance, value));
        return nullptr;
    }
}

static inline bool IsIdentifierStart(char c)
{
    return IsAsciiAlpha(c) || c == '_';
}

static inline bool IsIdentifierChar(char c)
{
    return IsAsciiAlphaOrDigit(c) || c == '_';
}

static inline Span<const char> SplitIdentifier(Span<const char> str)
{
    Size offset = 0;

    if (str.len && IsIdentifierStart(str[0])) {
        offset++;

        while (offset < str.len && IsIdentifierChar(str[offset])) {
            offset++;
        }
    }

    Span<const char> token = str.Take(0, offset);
    return token;
}

const TypeInfo *ResolveType(Napi::Env env, Span<const char> str, int *out_directions)
{
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    // Each item can be > 0 for array or 0 for a pointer
    LocalArray<Size, 8> arrays;
    uint8_t disposables = 0;

    // Consume parameter direction qualifier
    if (out_directions) {
        if (str.len && str[0] == '_') {
            Span<const char> qualifier = SplitIdentifier(str);

            if (qualifier == "_In_") {
                *out_directions = 1;
                str = str.Take(5, str.len - 5);
            } else if (qualifier == "_Out_") {
                *out_directions = 2;
                str = str.Take(6, str.len - 6);
            } else if (qualifier == "_Inout_") {
                *out_directions = 3;
                str = str.Take(8, str.len - 8);
            } else {
                *out_directions = 1;
            }
        } else {
            *out_directions = 1;
        }
    }

    Span<const char> name;
    Span<const char> after;
    {
        Span<const char> remain = str;

        // Skip initial const qualifiers
        remain = TrimStrLeft(remain);
        while (SplitIdentifier(remain) == "const") {
            remain = remain.Take(6, remain.len - 6);
            remain = TrimStrLeft(remain);
        }
        remain = TrimStrLeft(remain);

        after = remain;

        // Consume one or more identifiers (e.g. unsigned int)
        for (;;) {
            after = TrimStrLeft(after);

            Span<const char> token = SplitIdentifier(after);
            if (!token.len)
                break;
            after = after.Take(token.len, after.len - token.len);
        }

        name = TrimStr(MakeSpan(remain.ptr, after.ptr - remain.ptr));
    }

    // Consume type indirections (pointer, array, etc.)
    while (after.len) {
        if (after[0] == '*') {
            after = after.Take(1, after.len - 1);

            if (!arrays.Available()) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Too many type indirections");
                return nullptr;
            }

            arrays.Append(0);
        } else if (after[0] == '!') {
            after = after.Take(1, after.len - 1);
            disposables |= (1u << arrays.len);
        } else if (after[0] == '[') {
            after = after.Take(1, after.len - 1);

            Size len = 0;

            after = TrimStrLeft(after);
            if (!ParseInt(after, &len, 0, &after) || len < 0) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Invalid array length");
                return nullptr;
            }
            after = TrimStrLeft(after);
            if (!after.len || after[0] != ']') [[unlikely]] {
                ThrowError<Napi::Error>(env, "Expected ']' after array length");
                return nullptr;
            }
            after = after.Take(1, after.len - 1);

            if (!arrays.Available()) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Too many type indirections");
                return nullptr;
            }

            arrays.Append(len);
        } else if (SplitIdentifier(after) == "const") {
            after = after.Take(6, after.len - 6);
        } else {
            after = TrimStrRight(after);

            if (after.len) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Unexpected character '%1' in type specifier", after[0]);
                return nullptr;
            }

            break;
        }

        after = TrimStrLeft(after);
    }

    const TypeInfo *type = instance->types_map.FindValue(name, nullptr);

    if (!type) {
        // Try with cleaned up spaces
        if (name.len < 256) {
            LocalArray<char, 256> buf;
            for (Size i = 0; i < name.len; i++) {
                char c = name[i];

                if (IsAsciiWhite(c)) {
                    buf.Append(' ');
                    while (++i < name.len && IsAsciiWhite(name[i]));
                    i--;
                } else {
                    buf.Append(c);
                }
            }

            type = instance->types_map.FindValue(buf, nullptr);
        }

        if (!type)
            return nullptr;
    }

    for (int i = 0;; i++) {
        if (disposables & (1u << i)) {
            if (type->primitive != PrimitiveKind::Pointer &&
                    type->primitive != PrimitiveKind::String &&
                    type->primitive != PrimitiveKind::String16 &&
                    type->primitive != PrimitiveKind::String32) [[unlikely]] {
                ThrowError<Napi::Error>(env, "Cannot create disposable type for non-pointer");
                return nullptr;
            }

            TypeInfo *copy = instance->types.AppendDefault();

            memcpy((void *)copy, (const void *)type, RG_SIZE(*type));
            copy->name = Fmt(&instance->str_alloc, "<anonymous_%1>", instance->types.len).ptr;
            copy->members.allocator = GetNullAllocator();

            copy->dispose = [](Napi::Env env, const TypeInfo *, const void *ptr) {
                InstanceData *instance = env.GetInstanceData<InstanceData>();

                free((void *)ptr);
                instance->stats.disposed++;
            };

            type = copy;
        }

        if (i >= arrays.len)
            break;
        Size len = arrays[i];

        if (len > 0) {
            if (type->flags & (int)TypeFlag::IsIncomplete) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Cannot make array of incomplete type");
                return nullptr;
            }

            if (len > instance->config.max_type_size / type->size) {
                ThrowError<Napi::TypeError>(env, "Array length is too high (max = %1)", instance->config.max_type_size / type->size);
                return nullptr;
            }

            type = MakeArrayType(instance, type, len);
            RG_ASSERT(type);
        } else {
            RG_ASSERT(!len);

            type = MakePointerType(instance, type);
            RG_ASSERT(type);
        }
    }

    if (type->flags & (int)TypeFlag::IsIncomplete) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Cannot directly use incomplete type");
        return nullptr;
    }

    return type;
}

const TypeInfo *MakePointerType(InstanceData *instance, const TypeInfo *ref, int count)
{
    RG_ASSERT(count >= 1);

    for (int i = 0; i < count; i++) {
        char name_buf[256];
        Fmt(name_buf, "%1%2*", ref->name, EndsWith(ref->name, "*") ? "" : " ");

        bool inserted;
        auto bucket = instance->types_map.TrySetDefault(name_buf, &inserted);

        if (inserted) {
            TypeInfo *type = instance->types.AppendDefault();

            type->name = DuplicateString(name_buf, &instance->str_alloc).ptr;

            if (ref->primitive != PrimitiveKind::Prototype) {
                type->primitive = PrimitiveKind::Pointer;
                type->size = RG_SIZE(void *);
                type->align = RG_SIZE(void *);
                type->ref.type = ref;
            } else {
                type->primitive = PrimitiveKind::Callback;
                type->size = RG_SIZE(void *);
                type->align = RG_SIZE(void *);
                type->ref.proto = ref->ref.proto;
            }

            bucket->key = type->name;
            bucket->value = type;
        }

        ref = bucket->value;
    }

    return ref;
}

static const TypeInfo *MakeArrayType(InstanceData *instance, const TypeInfo *ref, Size len,
                                     ArrayHint hint, bool insert)
{
    RG_ASSERT(len > 0);
    RG_ASSERT(len <= instance->config.max_type_size / ref->size);

    TypeInfo *type = instance->types.AppendDefault();

    type->name = Fmt(&instance->str_alloc, "%1[%2]", ref->name, len).ptr;

    type->primitive = PrimitiveKind::Array;
    type->align = ref->align;
    type->size = (int32_t)(len * ref->size);
    type->ref.type = ref;
    type->hint = hint;

    if (insert) {
        bool inserted;
        type = (TypeInfo *)*instance->types_map.TrySet(type->name, type, &inserted);
        instance->types.RemoveLast(!inserted);
    }

    return type;
}

const TypeInfo *MakeArrayType(InstanceData *instance, const TypeInfo *ref, Size len)
{
    ArrayHint hint = {};

    if (ref->flags & (int)TypeFlag::IsCharLike) {
        hint = ArrayHint::String;
    } else if (ref->flags & (int)TypeFlag::HasTypedArray) {
        hint = ArrayHint::Typed;
    } else {
        hint = ArrayHint::Array;
    }

    return MakeArrayType(instance, ref, len, hint, true);
}

const TypeInfo *MakeArrayType(InstanceData *instance, const TypeInfo *ref, Size len, ArrayHint hint)
{
    return MakeArrayType(instance, ref, len, hint, false);
}

Napi::External<TypeInfo> WrapType(Napi::Env env, InstanceData *instance, const TypeInfo *type)
{
    Napi::External<TypeInfo> external = Napi::External<TypeInfo>::New(env, (TypeInfo *)type);
    SetValueTag(instance, external, &TypeInfoMarker);

    return external;
}

bool CanPassType(const TypeInfo *type, int directions)
{
    if (directions & 2) {
        if (type->primitive == PrimitiveKind::Pointer)
            return true;
        if (type->primitive == PrimitiveKind::String)
            return true;
        if (type->primitive == PrimitiveKind::String16)
            return true;
        if (type->primitive == PrimitiveKind::String32)
            return true;

        return false;
    } else {
        if (type->primitive == PrimitiveKind::Void)
            return false;
        if (type->primitive == PrimitiveKind::Array)
            return false;
        if (type->primitive == PrimitiveKind::Prototype)
            return false;
        if (type->primitive == PrimitiveKind::Callback && type->ref.proto->variadic)
            return false;

        return true;
    }
}

bool CanReturnType(const TypeInfo *type)
{
    if (type->primitive == PrimitiveKind::Void && !TestStr(type->name, "void"))
        return false;
    if (type->primitive == PrimitiveKind::Array)
        return false;
    if (type->primitive == PrimitiveKind::Prototype)
        return false;

    return true;
}

bool CanStoreType(const TypeInfo *type)
{
    if (type->primitive == PrimitiveKind::Void)
        return false;
    if (type->primitive == PrimitiveKind::Prototype)
        return false;
    if (type->primitive == PrimitiveKind::Callback && type->ref.proto->variadic)
        return false;

    return true;
}

const char *GetValueType(const InstanceData *instance, Napi::Value value)
{
    if (CheckValueTag(instance, value, &CastMarker)) {
        Napi::External<ValueCast> external = value.As<Napi::External<ValueCast>>();
        ValueCast *cast = external.Data();

        return cast->type->name;
    }

    if (CheckValueTag(instance, value, &TypeInfoMarker))
        return "Type";
    for (const TypeInfo &type: instance->types) {
        if (type.ref.marker && CheckValueTag(instance, value, type.ref.marker))
            return type.name;
    }

    if (value.IsArray()) {
        return "Array";
    } else if (value.IsTypedArray()) {
        Napi::TypedArray array = value.As<Napi::TypedArray>();

        switch (array.TypedArrayType()) {
            case napi_int8_array: return "Int8Array";
            case napi_uint8_array: return "Uint8Array";
            case napi_uint8_clamped_array: return "Uint8ClampedArray";
            case napi_int16_array: return "Int16Array";
            case napi_uint16_array: return "Uint16Array";
            case napi_int32_array: return "Int32Array";
            case napi_uint32_array: return "Uint32Array";
            case napi_float32_array: return "Float32Array";
            case napi_float64_array: return "Float64Array";
            case napi_bigint64_array: return "BigInt64Array";
            case napi_biguint64_array: return "BigUint64Array";
        }
    } else if (value.IsArrayBuffer()) {
        return "ArrayBuffer";
    } else if (value.IsBuffer()) {
        return "Buffer";
    }

    switch (value.Type()) {
        case napi_undefined: return "Undefined";
        case napi_null: return "Null";
        case napi_boolean: return "Boolean";
        case napi_number: return "Number";
        case napi_string: return "String";
        case napi_symbol: return "Symbol";
        case napi_object: return "Object";
        case napi_function: return "Function";
        case napi_external: return "External";
        case napi_bigint: return "BigInt";
    }

    // This should not be possible, but who knows...
    return "Unknown";
}

void SetValueTag(const InstanceData *instance, Napi::Value value, const void *marker)
{
    static_assert(RG_SIZE(TypeInfo) >= 16);

    // We used to make a temporary tag on the stack with lower set to a constant value and
    // upper to the pointer address, but this broke in Node 20.12 and Node 21.6 due to ExternalWrapper
    // storing a pointer to the tag and not the tag value itself (which seems wrong, but anyway).
    //
    // Since this is no longer an option, we just type alias whatever marker points to to napi_type_tag.
    // This may seem gross, but we disable strict aliasing anyway, so as long as what marker points
    // to is bigger than 16 bytes and does not change, it works!
    // Which holds true for us: the main thing pointed to is TypeInfo, which is constant and big enough,
    // and the few other markers we use, such as CastMarker, are actual const napi_type_tag structs.
    const napi_type_tag *tag = (const napi_type_tag *)marker;

    napi_status status = napi_type_tag_object(value.Env(), value, tag);
    RG_ASSERT(status == napi_ok);
}

bool CheckValueTag(const InstanceData *instance, Napi::Value value, const void *marker)
{
    bool match = false;

    if (!IsNullOrUndefined(value)) {
        const napi_type_tag *tag = (const napi_type_tag *)marker;
        napi_check_object_type_tag(value.Env(), value, tag, &match);
    }

    return match;
}

int GetTypedArrayType(const TypeInfo *type)
{
    switch (type->primitive) {
        case PrimitiveKind::Int8: return napi_int8_array;
        case PrimitiveKind::UInt8: return napi_uint8_array;
        case PrimitiveKind::Int16: return napi_int16_array;
        case PrimitiveKind::UInt16: return napi_uint16_array;
        case PrimitiveKind::Int32: return napi_int32_array;
        case PrimitiveKind::UInt32: return napi_uint32_array;
        case PrimitiveKind::Float32: return napi_float32_array;
        case PrimitiveKind::Float64: return napi_float64_array;

        default: return -1;
    }

    RG_UNREACHABLE();
}

Napi::String MakeStringFromUTF32(Napi::Env env, const char32_t *ptr, Size len)
{
    HeapArray<char16_t> buf;
    buf.Reserve(len * 2);

    for (Size i = 0; i < len; i++) {
        char32_t uc = ptr[i];

        if (uc < 0xFFFF) {
            if (uc < 0xD800 || uc > 0xDFFF) {
                buf.Append((char16_t)uc);
            } else {
                buf.Append('?');
            }
        } else if (uc < 0x10FFFF) {
            uc -= 0x0010000UL;

            buf.Append((char16_t)((uc >> 10) + 0xD800));
            buf.Append((char16_t)((uc & 0x3FFul) + 0xDC00));
        } else {
            buf.Append('?');
        }
    }

    Napi::String str = Napi::String::New(env, buf.ptr, buf.len);
    return str;
}

Napi::Object DecodeObject(Napi::Env env, const uint8_t *origin, const TypeInfo *type)
{
    // We can't decode unions because we don't know which member is valid
    if (type->primitive == PrimitiveKind::Union) {
        InstanceData *instance = env.GetInstanceData<InstanceData>();

        Napi::Object wrapper = type->construct.New({}).As<Napi::Object>();
        SetValueTag(instance, wrapper, &MagicUnionMarker);

        MagicUnion *u = MagicUnion::Unwrap(wrapper);
        u->SetRaw(origin);

        return wrapper;
    }

    Napi::Object obj = Napi::Object::New(env);
    DecodeObject(obj, origin, type);
    return obj;
}

void DecodeObject(Napi::Object obj, const uint8_t *origin, const TypeInfo *type)
{
    Napi::Env env = obj.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    RG_ASSERT(type->primitive == PrimitiveKind::Record);

    for (Size i = 0; i < type->members.len; i++) {
        const RecordMember &member = type->members[i];

        const uint8_t *src = origin + member.offset;

        switch (member.type->primitive) {
            case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

            case PrimitiveKind::Bool: {
                bool b = *(bool *)src;
                obj.Set(member.name, Napi::Boolean::New(env, b));
            } break;
            case PrimitiveKind::Int8: {
                double d = (double)*(int8_t *)src;
                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::UInt8: {
                double d = (double)*(uint8_t *)src;
                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::Int16: {
                double d = (double)*(int16_t *)src;
                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::Int16S: {
                int16_t v = *(int16_t *)src;
                double d = (double)ReverseBytes(v);

                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::UInt16: {
                double d = (double)*(uint16_t *)src;
                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::UInt16S: {
                uint16_t v = *(uint16_t *)src;
                double d = (double)ReverseBytes(v);

                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::Int32: {
                double d = (double)*(int32_t *)src;
                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::Int32S: {
                int32_t v = *(int32_t *)src;
                double d = (double)ReverseBytes(v);

                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::UInt32: {
                double d = (double)*(uint32_t *)src;
                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::UInt32S: {
                uint32_t v = *(uint32_t *)src;
                double d = (double)ReverseBytes(v);

                obj.Set(member.name, Napi::Number::New(env, d));
            } break;
            case PrimitiveKind::Int64: {
                int64_t v = *(int64_t *)src;
                obj.Set(member.name, NewBigInt(env, v));
            } break;
            case PrimitiveKind::Int64S: {
                int64_t v = ReverseBytes(*(int64_t *)src);
                obj.Set(member.name, NewBigInt(env, v));
            } break;
            case PrimitiveKind::UInt64: {
                uint64_t v = *(uint64_t *)src;
                obj.Set(member.name, NewBigInt(env, v));
            } break;
            case PrimitiveKind::UInt64S: {
                uint64_t v = ReverseBytes(*(uint64_t *)src);
                obj.Set(member.name, NewBigInt(env, v));
            } break;
            case PrimitiveKind::String: {
                const char *str = *(const char **)src;
                obj.Set(member.name, str ? Napi::String::New(env, str) : env.Null());

                if (member.type->dispose) {
                    member.type->dispose(env, member.type, str);
                }
            } break;
            case PrimitiveKind::String16: {
                const char16_t *str16 = *(const char16_t **)src;
                obj.Set(member.name, str16 ? Napi::String::New(env, str16) : env.Null());

                if (member.type->dispose) {
                    member.type->dispose(env, member.type, str16);
                }
            } break;
            case PrimitiveKind::String32: {
                const char32_t *str32 = *(const char32_t **)src;
                obj.Set(member.name, str32 ? MakeStringFromUTF32(env, str32) : env.Null());
            } break;
            case PrimitiveKind::Pointer:
            case PrimitiveKind::Callback: {
                void *ptr2 = *(void **)src;

                if (ptr2) {
                    Napi::External<void> external = Napi::External<void>::New(env, ptr2);
                    SetValueTag(instance, external, member.type->ref.marker);

                    obj.Set(member.name, external);
                } else {
                    obj.Set(member.name, env.Null());
                }

                if (member.type->dispose) {
                    member.type->dispose(env, member.type, ptr2);
                }
            } break;
            case PrimitiveKind::Record:
            case PrimitiveKind::Union: {
                Napi::Object obj2 = DecodeObject(env, src, member.type);
                obj.Set(member.name, obj2);
            } break;
            case PrimitiveKind::Array: {
                Napi::Value value = DecodeArray(env, src, member.type);
                obj.Set(member.name, value);
            } break;
            case PrimitiveKind::Float32: {
                float f = *(float *)src;
                obj.Set(member.name, Napi::Number::New(env, (double)f));
            } break;
            case PrimitiveKind::Float64: {
                double d = *(double *)src;
                obj.Set(member.name, Napi::Number::New(env, d));
            } break;

            case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
        }
    }
}

Napi::Value DecodeArray(Napi::Env env, const uint8_t *origin, const TypeInfo *type)
{
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    RG_ASSERT(type->primitive == PrimitiveKind::Array);

    uint32_t len = type->size / type->ref.type->size;
    Size offset = 0;

#define POP_ARRAY(SetCode) \
        do { \
            Napi::Array array = Napi::Array::New(env); \
             \
            for (uint32_t i = 0; i < len; i++) { \
                offset = AlignLen(offset, type->ref.type->align); \
                 \
                const uint8_t *src = origin + offset; \
                 \
                SetCode \
                 \
                offset += type->ref.type->size; \
            } \
             \
            return array; \
        } while (false)
#define POP_NUMBER_ARRAY(TypedArrayType, CType) \
        do { \
            if (type->hint == ArrayHint::Array) { \
                POP_ARRAY({ \
                    double d = (double)*(CType *)src; \
                    array.Set(i, Napi::Number::New(env, d)); \
                }); \
            } else { \
                Napi::TypedArrayType array = Napi::TypedArrayType::New(env, len); \
                Span<uint8_t> buffer = MakeSpan((uint8_t *)array.ArrayBuffer().Data(), (Size)len * RG_SIZE(CType)); \
                 \
                DecodeBuffer(buffer, origin, type->ref.type); \
                 \
                return array; \
            } \
        } while (false)
#define POP_NUMBER_ARRAY_SWAP(TypedArrayType, CType) \
        do { \
            if (type->hint == ArrayHint::Array) { \
                POP_ARRAY({ \
                    CType v = *(CType *)src; \
                    double d = (double)ReverseBytes(v); \
                    array.Set(i, Napi::Number::New(env, d)); \
                }); \
            } else { \
                Napi::TypedArrayType array = Napi::TypedArrayType::New(env, len); \
                Span<uint8_t> buffer = MakeSpan((uint8_t *)array.ArrayBuffer().Data(), (Size)len * RG_SIZE(CType)); \
                 \
                DecodeBuffer(buffer, origin, type->ref.type); \
                 \
                return array; \
            } \
        } while (false)

    switch (type->ref.type->primitive) {
        case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

        case PrimitiveKind::Bool: {
            POP_ARRAY({
                bool b = *(bool *)src;
                array.Set(i, Napi::Boolean::New(env, b));
            });
        } break;
        case PrimitiveKind::Int8: {
            if (type->hint == ArrayHint::String) {
                const char *ptr = (const char *)origin;
                size_t count = strnlen(ptr, (size_t)len);

                Napi::String str = Napi::String::New(env, ptr, count);
                return str;
            }

            POP_NUMBER_ARRAY(Int8Array, int8_t);
        } break;
        case PrimitiveKind::UInt8: { POP_NUMBER_ARRAY(Uint8Array, uint8_t); } break;
        case PrimitiveKind::Int16: {
            if (type->hint == ArrayHint::String) {
                const char16_t *ptr = (const char16_t *)origin;
                Size count = NullTerminatedLength(ptr, len);

                Napi::String str = Napi::String::New(env, ptr, count);
                return str;
            }

            POP_NUMBER_ARRAY(Int16Array, int16_t);
        } break;
        case PrimitiveKind::Int16S: { POP_NUMBER_ARRAY_SWAP(Int16Array, int16_t); } break;
        case PrimitiveKind::UInt16: { POP_NUMBER_ARRAY(Uint16Array, uint16_t); } break;
        case PrimitiveKind::UInt16S: { POP_NUMBER_ARRAY_SWAP(Uint16Array, uint16_t); } break;
        case PrimitiveKind::Int32: {
            if (type->hint == ArrayHint::String) {
                const char32_t *ptr = (const char32_t *)origin;
                Size count = NullTerminatedLength(ptr, len);

                Napi::String str = MakeStringFromUTF32(env, ptr, count);
                return str;
            }

            POP_NUMBER_ARRAY(Int32Array, int32_t);
        } break;
        case PrimitiveKind::Int32S: { POP_NUMBER_ARRAY_SWAP(Int32Array, int32_t); } break;
        case PrimitiveKind::UInt32: { POP_NUMBER_ARRAY(Uint32Array, uint32_t); } break;
        case PrimitiveKind::UInt32S: { POP_NUMBER_ARRAY_SWAP(Uint32Array, uint32_t); } break;
        case PrimitiveKind::Int64: {
            POP_ARRAY({
                int64_t v = *(int64_t *)src;
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::Int64S: {
            POP_ARRAY({
                int64_t v = ReverseBytes(*(int64_t *)src);
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::UInt64: {
            POP_ARRAY({
                uint64_t v = *(uint64_t *)src;
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::UInt64S: {
            POP_ARRAY({
                uint64_t v = ReverseBytes(*(uint64_t *)src);
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::String: {
            POP_ARRAY({
                const char *str = *(const char **)src;
                array.Set(i, str ? Napi::String::New(env, str) : env.Null());
            });
        } break;
        case PrimitiveKind::String16: {
            POP_ARRAY({
                const char16_t *str16 = *(const char16_t **)src;
                array.Set(i, str16 ? Napi::String::New(env, str16) : env.Null());
            });
        } break;
        case PrimitiveKind::String32: {
            POP_ARRAY({
                const char32_t *str32 = *(const char32_t **)src;
                array.Set(i, str32 ? MakeStringFromUTF32(env, str32) : env.Null());
            });
        } break;
        case PrimitiveKind::Pointer:
        case PrimitiveKind::Callback: {
            POP_ARRAY({
                void *ptr2 = *(void **)src;

                if (ptr2) {
                    Napi::External<void> external = Napi::External<void>::New(env, ptr2);
                    SetValueTag(instance, external, type->ref.type->ref.marker);

                    array.Set(i, external);
                } else {
                    array.Set(i, env.Null());
                }
            });
        } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            POP_ARRAY({
                Napi::Object obj = DecodeObject(env, src, type->ref.type);
                array.Set(i, obj);
            });
        } break;
        case PrimitiveKind::Array: {
            POP_ARRAY({
                Napi::Value value = DecodeArray(env, src, type->ref.type);
                array.Set(i, value);
            });
        } break;
        case PrimitiveKind::Float32: { POP_NUMBER_ARRAY(Float32Array, float); } break;
        case PrimitiveKind::Float64: { POP_NUMBER_ARRAY(Float64Array, double); } break;

        case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
    }

#undef POP_NUMBER_ARRAY_SWAP
#undef POP_NUMBER_ARRAY
#undef POP_ARRAY

    RG_UNREACHABLE();
}

void DecodeNormalArray(Napi::Array array, const uint8_t *origin, const TypeInfo *ref)
{
    Napi::Env env = array.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    RG_ASSERT(array.IsArray());

    Size offset = 0;
    uint32_t len = array.Length();

#define POP_ARRAY(SetCode) \
        do { \
            for (uint32_t i = 0; i < len; i++) { \
                offset = AlignLen(offset, ref->align); \
                 \
                const uint8_t *src = origin + offset; \
                 \
                SetCode \
                 \
                offset += ref->size; \
            } \
        } while (false)
#define POP_NUMBER_ARRAY(CType) \
        do { \
            POP_ARRAY({ \
                double d = (double)*(CType *)src; \
                array.Set(i, Napi::Number::New(env, d)); \
            }); \
        } while (false)
#define POP_NUMBER_ARRAY_SWAP(CType) \
        do { \
            POP_ARRAY({ \
                CType v = *(CType *)src; \
                double d = (double)ReverseBytes(v); \
                array.Set(i, Napi::Number::New(env, d)); \
            }); \
        } while (false)

    switch (ref->primitive) {
        case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

        case PrimitiveKind::Bool: {
            POP_ARRAY({
                bool b = *(bool *)src;
                array.Set(i, Napi::Boolean::New(env, b));
            });
        } break;
        case PrimitiveKind::Int8: { POP_NUMBER_ARRAY(int8_t); } break;
        case PrimitiveKind::UInt8: { POP_NUMBER_ARRAY(uint8_t); } break;
        case PrimitiveKind::Int16: { POP_NUMBER_ARRAY(int16_t); } break;
        case PrimitiveKind::Int16S: { POP_NUMBER_ARRAY_SWAP(int16_t); } break;
        case PrimitiveKind::UInt16: { POP_NUMBER_ARRAY(uint16_t); } break;
        case PrimitiveKind::UInt16S: { POP_NUMBER_ARRAY_SWAP(uint16_t); } break;
        case PrimitiveKind::Int32: { POP_NUMBER_ARRAY(int32_t); } break;
        case PrimitiveKind::Int32S: { POP_NUMBER_ARRAY_SWAP(int32_t); } break;
        case PrimitiveKind::UInt32: { POP_NUMBER_ARRAY(uint32_t); } break;
        case PrimitiveKind::UInt32S: { POP_NUMBER_ARRAY_SWAP(uint32_t); } break;
        case PrimitiveKind::Int64: {
            POP_ARRAY({
                int64_t v = *(int64_t *)src;
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::Int64S: {
            POP_ARRAY({
                int64_t v = ReverseBytes(*(int64_t *)src);
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::UInt64: {
            POP_ARRAY({
                uint64_t v = *(uint64_t *)src;
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::UInt64S: {
            POP_ARRAY({
                uint64_t v = ReverseBytes(*(uint64_t *)src);
                array.Set(i, NewBigInt(env, v));
            });
        } break;
        case PrimitiveKind::String: {
            POP_ARRAY({
                const char *str = *(const char **)src;
                array.Set(i, str ? Napi::String::New(env, str) : env.Null());

                if (ref->dispose) {
                    ref->dispose(env, ref, str);
                }
            });
        } break;
        case PrimitiveKind::String16: {
            POP_ARRAY({
                const char16_t *str16 = *(const char16_t **)src;
                array.Set(i, str16 ? Napi::String::New(env, str16) : env.Null());

                if (ref->dispose) {
                    ref->dispose(env, ref, str16);
                }
            });
        } break;
        case PrimitiveKind::String32: {
            POP_ARRAY({
                const char32_t *str32 = *(const char32_t **)src;
                array.Set(i, str32 ? MakeStringFromUTF32(env, str32) : env.Null());

                if (ref->dispose) {
                    ref->dispose(env, ref, str32);
                }
            });
        } break;
        case PrimitiveKind::Pointer:
        case PrimitiveKind::Callback: {
            POP_ARRAY({
                void *ptr2 = *(void **)src;

                if (ptr2) {
                    Napi::External<void> external = Napi::External<void>::New(env, ptr2);
                    SetValueTag(instance, external, ref->ref.marker);

                    array.Set(i, external);
                } else {
                    array.Set(i, env.Null());
                }

                if (ref->dispose) {
                    ref->dispose(env, ref, ptr2);
                }
            });
        } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            POP_ARRAY({
                Napi::Object obj = DecodeObject(env, src, ref);
                array.Set(i, obj);
            });
        } break;
        case PrimitiveKind::Array: {
            POP_ARRAY({
                Napi::Value value = DecodeArray(env, src, ref);
                array.Set(i, value);
            });
        } break;
        case PrimitiveKind::Float32: { POP_NUMBER_ARRAY(float); } break;
        case PrimitiveKind::Float64: { POP_NUMBER_ARRAY(double); } break;

        case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
    }

#undef POP_NUMBER_ARRAY_SWAP
#undef POP_NUMBER_ARRAY
#undef POP_ARRAY
}

void DecodeBuffer(Span<uint8_t> buffer, const uint8_t *origin, const TypeInfo *ref)
{
    // Go fast brrrrr!
    memcpy_safe(buffer.ptr, origin, (size_t)buffer.len);

#define SWAP(CType) \
        do { \
            CType *data = (CType *)buffer.ptr; \
            Size len = buffer.len / RG_SIZE(CType); \
             \
            for (Size i = 0; i < len; i++) { \
                data[i] = ReverseBytes(data[i]); \
            } \
        } while (false)

    if (ref->primitive == PrimitiveKind::Int16S || ref->primitive == PrimitiveKind::UInt16S) {
        SWAP(uint16_t);
    } else if (ref->primitive == PrimitiveKind::Int32S || ref->primitive == PrimitiveKind::UInt32S) {
        SWAP(uint32_t);
    } else if (ref->primitive == PrimitiveKind::Int64S || ref->primitive == PrimitiveKind::UInt64S) {
        SWAP(uint64_t);
    }

#undef SWAP
}

Napi::Value Decode(Napi::Value value, Size offset, const TypeInfo *type, const Size *len)
{
    Napi::Env env = value.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    const uint8_t *ptr = nullptr;

    if (value.IsExternal()) {
        Napi::External<void> external = value.As<Napi::External<void>>();
        ptr = (const uint8_t *)external.Data();
    } else if (IsRawBuffer(value)) {
        Span<uint8_t> buffer = GetRawBuffer(value);

        if (buffer.len - offset < type->size) [[unlikely]] {
            ThrowError<Napi::Error>(env, "Expected buffer with size superior or equal to type %1 (%2 bytes)",
                                    type->name, type->size + offset);
            return env.Null();
        }

        ptr = (const uint8_t *)buffer.ptr;
    } else {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for variable, expected external or TypedArray", GetValueType(instance, value));
        return env.Null();
    }

    if (!ptr)
        return env.Null();
    ptr += offset;

    Napi::Value ret = Decode(env, ptr, type, len);
    return ret;
}

Napi::Value Decode(Napi::Env env, const uint8_t *ptr, const TypeInfo *type, const Size *len)
{
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (len && type->primitive != PrimitiveKind::String &&
               type->primitive != PrimitiveKind::String16 &&
               type->primitive != PrimitiveKind::String32 &&
               type->primitive != PrimitiveKind::Prototype) {
        if (*len >= 0) {
            type = MakeArrayType(instance, type, *len);
        } else {
            switch (type->primitive) {
                case PrimitiveKind::Int8: 
                case PrimitiveKind::UInt8: {
                    Size count = strlen((const char *)ptr);
                    type = MakeArrayType(instance, type, count);
                } break;
                case PrimitiveKind::Int16: 
                case PrimitiveKind::UInt16: {
                    Size count = NullTerminatedLength((const char16_t *)ptr, RG_SIZE_MAX);
                    type = MakeArrayType(instance, type, count);
                } break;
                case PrimitiveKind::Int32:
                case PrimitiveKind::UInt32: {
                    Size count = NullTerminatedLength((const char32_t *)ptr, RG_SIZE_MAX);
                    type = MakeArrayType(instance, type, count);
                } break;

                case PrimitiveKind::Pointer: {
                    Size count = NullTerminatedLength((const void **)ptr, RG_SIZE_MAX);
                    type = MakeArrayType(instance, type, count);
                } break;

                default: {
                    ThrowError<Napi::TypeError>(env, "Cannot determine null-terminated length for type %1", type->name);
                    return env.Null();
                } break;
            }
        }
    }

#define RETURN_INT(Type, NewCall) \
        do { \
            Type v = *(Type *)ptr; \
            return NewCall(env, v); \
        } while (false)
#define RETURN_INT_SWAP(Type, NewCall) \
        do { \
            Type v = ReverseBytes(*(Type *)ptr); \
            return NewCall(env, v); \
        } while (false)

    switch (type->primitive) {
        case PrimitiveKind::Void: {
            ThrowError<Napi::TypeError>(env, "Cannot decode value of type %1", type->name);
            return env.Null();
        } break;

        case PrimitiveKind::Bool: {
            bool v = *(bool *)ptr;
            return Napi::Boolean::New(env, v);
        } break;
        case PrimitiveKind::Int8: { RETURN_INT(int8_t, Napi::Number::New); } break;
        case PrimitiveKind::UInt8: { RETURN_INT(uint8_t, Napi::Number::New); } break;
        case PrimitiveKind::Int16: { RETURN_INT(int16_t, Napi::Number::New); } break;
        case PrimitiveKind::Int16S: { RETURN_INT_SWAP(int16_t, Napi::Number::New); } break;
        case PrimitiveKind::UInt16: { RETURN_INT(uint16_t, Napi::Number::New); } break;
        case PrimitiveKind::UInt16S: { RETURN_INT_SWAP(uint16_t, Napi::Number::New); } break;
        case PrimitiveKind::Int32: { RETURN_INT(int32_t, Napi::Number::New); } break;
        case PrimitiveKind::Int32S: { RETURN_INT_SWAP(int32_t, Napi::Number::New); } break;
        case PrimitiveKind::UInt32: { RETURN_INT(uint32_t, Napi::Number::New); } break;
        case PrimitiveKind::UInt32S: { RETURN_INT_SWAP(uint32_t, Napi::Number::New); } break;
        case PrimitiveKind::Int64: { RETURN_INT(int64_t, NewBigInt); } break;
        case PrimitiveKind::Int64S: { RETURN_INT_SWAP(int64_t, NewBigInt); } break;
        case PrimitiveKind::UInt64: { RETURN_INT(uint64_t, NewBigInt); } break;
        case PrimitiveKind::UInt64S: { RETURN_INT_SWAP(uint64_t, NewBigInt); } break;
        case PrimitiveKind::String: {
            if (len) {
                const char *str = *(const char **)ptr;
                return str ? Napi::String::New(env, str, *len) : env.Null();
            } else {
                const char *str = *(const char **)ptr;
                return str ? Napi::String::New(env, str) : env.Null();
            }
        } break;
        case PrimitiveKind::String16: {
            if (len) {
                const char16_t *str16 = *(const char16_t **)ptr;
                return str16 ? Napi::String::New(env, str16, *len) : env.Null();
            } else {
                const char16_t *str16 = *(const char16_t **)ptr;
                return str16 ? Napi::String::New(env, str16) : env.Null();
            }
        } break;
        case PrimitiveKind::String32: {
            if (len) {
                const char32_t *str32 = *(const char32_t **)ptr;
                return str32 ? MakeStringFromUTF32(env, str32, *len) : env.Null();
            } else {
                const char32_t *str32 = *(const char32_t **)ptr;
                return str32 ? MakeStringFromUTF32(env, str32) : env.Null();
            }
        } break;
        case PrimitiveKind::Pointer:
        case PrimitiveKind::Callback: {
            void *ptr2 = *(void **)ptr;
            return ptr2 ? Napi::External<void>::New(env, ptr2, [](Napi::Env, void *) {}) : env.Null();
        } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            Napi::Object obj = DecodeObject(env, ptr, type);
            return obj;
        } break;
        case PrimitiveKind::Array: {
            Napi::Value array = DecodeArray(env, ptr, type);
            return array;
        } break;
        case PrimitiveKind::Float32: {
            float f = *(float *)ptr;
            return Napi::Number::New(env, f);
        } break;
        case PrimitiveKind::Float64: {
            double d = *(double *)ptr;
            return Napi::Number::New(env, d);
        } break;

        case PrimitiveKind::Prototype: {
            const FunctionInfo *proto = type->ref.proto;
            RG_ASSERT(!proto->variadic);
            RG_ASSERT(!proto->lib);

            FunctionInfo *func = new FunctionInfo();
            RG_DEFER { func->Unref(); };

            memcpy((void *)func, proto, RG_SIZE(*proto));
            memset((void *)&func->parameters, 0, RG_SIZE(func->parameters));
            func->parameters = proto->parameters;

            func->name = "<anonymous>";
            func->native = (void *)ptr;

            // Fix back parameter offset
            for (ParameterInfo &param: func->parameters) {
                param.offset -= 2;
            }
            func->required_parameters -= 2;

            Napi::Function wrapper = WrapFunction(env, func);
            return wrapper;
        } break;
    }

#undef RETURN_BIGINT
#undef RETURN_INT

    return env.Null();
}

bool Encode(Napi::Value ref, Size offset, Napi::Value value, const TypeInfo *type, const Size *len)
{
    Napi::Env env = ref.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    uint8_t *ptr = nullptr;

    if (ref.IsExternal()) {
        Napi::External<void> external = ref.As<Napi::External<void>>();
        ptr = (uint8_t *)external.Data();
    } else if (IsRawBuffer(ref)) {
        Span<uint8_t> buffer = GetRawBuffer(ref);

        if (buffer.len - offset < type->size) [[unlikely]] {
            ThrowError<Napi::Error>(env, "Expected buffer with size superior or equal to type %1 (%2 bytes)",
                                    type->name, type->size + offset);
            return env.Null();
        }

        ptr = (uint8_t *)buffer.ptr;
    } else {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for reference, expected external or TypedArray", GetValueType(instance, value));
        return env.Null();
    }

    if (!ptr) [[unlikely]] {
        ThrowError<Napi::Error>(env, "Cannot encode data in NULL pointer");
        return env.Null();
    }
    ptr += offset;

    return Encode(env, ptr, value, type, len);
}

bool Encode(Napi::Env env, uint8_t *origin, Napi::Value value, const TypeInfo *type, const Size *len)
{
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (len && type->primitive != PrimitiveKind::String &&
               type->primitive != PrimitiveKind::String16 &&
               type->primitive != PrimitiveKind::String32 &&
               type->primitive != PrimitiveKind::Prototype) {
        if (*len < 0) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Automatic (negative) length is only supported when decoding");
            return env.Null();
        }

        type = MakeArrayType(instance, type, *len);
    }

    InstanceMemory mem = {};
    CallData call(env, instance, &mem);

#define PUSH_INTEGER(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            *(CType *)origin = v; \
        } while (false)
#define PUSH_INTEGER_SWAP(CType) \
        do { \
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] { \
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value)); \
                return false; \
            } \
             \
            CType v = GetNumber<CType>(value); \
            *(CType *)origin = ReverseBytes(v); \
        } while (false)

    switch (type->primitive) {
        case PrimitiveKind::Void: { RG_UNREACHABLE(); } break;

        case PrimitiveKind::Bool: {
            if (!value.IsBoolean()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected boolean", GetValueType(instance, value));
                return false;
            }

            bool b = value.As<Napi::Boolean>();
            *(bool *)origin = b;
        } break;
        case PrimitiveKind::Int8: { PUSH_INTEGER(int8_t); } break;
        case PrimitiveKind::UInt8: { PUSH_INTEGER(uint8_t); } break;
        case PrimitiveKind::Int16: { PUSH_INTEGER(int16_t); } break;
        case PrimitiveKind::Int16S: { PUSH_INTEGER_SWAP(int16_t); } break;
        case PrimitiveKind::UInt16: { PUSH_INTEGER(uint16_t); } break;
        case PrimitiveKind::UInt16S: { PUSH_INTEGER_SWAP(uint16_t); } break;
        case PrimitiveKind::Int32: { PUSH_INTEGER(int32_t); } break;
        case PrimitiveKind::Int32S: { PUSH_INTEGER_SWAP(int32_t); } break;
        case PrimitiveKind::UInt32: { PUSH_INTEGER(uint32_t); } break;
        case PrimitiveKind::UInt32S: { PUSH_INTEGER_SWAP(uint32_t); } break;
        case PrimitiveKind::Int64: { PUSH_INTEGER(int64_t); } break;
        case PrimitiveKind::Int64S: { PUSH_INTEGER_SWAP(int64_t); } break;
        case PrimitiveKind::UInt64: { PUSH_INTEGER(uint64_t); } break;
        case PrimitiveKind::UInt64S: { PUSH_INTEGER_SWAP(uint64_t); } break;
        case PrimitiveKind::String: {
            const char *str;
            if (!call.PushString(value, 1, &str)) [[unlikely]]
                return false;
            *(const char **)origin = str;
        } break;
        case PrimitiveKind::String16: {
            const char16_t *str16;
            if (!call.PushString16(value, 1, &str16)) [[unlikely]]
                return false;
            *(const char16_t **)origin = str16;
        } break;
        case PrimitiveKind::String32: {
            const char32_t *str32;
            if (!call.PushString32(value, 1, &str32)) [[unlikely]]
                return false;
            *(const char32_t **)origin = str32;
        } break;
        case PrimitiveKind::Pointer: {
            void *ptr;
            if (!call.PushPointer(value, type, 1, &ptr)) [[unlikely]]
                return false;
            *(void **)origin = ptr;
        } break;
        case PrimitiveKind::Record:
        case PrimitiveKind::Union: {
            if (!IsObject(value)) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected object", GetValueType(instance, value));
                return false;
            }

            Napi::Object obj = value.As<Napi::Object>();

            if (!call.PushObject(obj, type, origin))
                return false;
        } break;
        case PrimitiveKind::Array: {
            if (value.IsArray()) {
                Napi::Array array = value.As<Napi::Array>();
                Size len = (Size)type->size / type->ref.type->size;

                if (!call.PushNormalArray(array, len, type, origin))
                    return false;
            } else if (IsRawBuffer(value)) {
                Span<const uint8_t> buffer = GetRawBuffer(value);
                call.PushBuffer(buffer, type->size, type, origin);
            } else if (value.IsString()) {
                if (!call.PushStringArray(value, type, origin))
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
            *(float *)origin = f;
        } break;
        case PrimitiveKind::Float64: {
            if (!value.IsNumber() && !value.IsBigInt()) [[unlikely]] {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected number", GetValueType(instance, value));
                return false;
            }

            double d = GetNumber<double>(value);
            *(double *)origin = d;
        } break;
        case PrimitiveKind::Callback: {
            void *ptr;

            if (value.IsFunction()) {
                ThrowError<Napi::Error>(env, "Cannot encode non-registered callback");
                return false;
            } else if (CheckValueTag(instance, value, type->ref.marker)) {
                ptr = value.As<Napi::External<void>>().Data();
            } else if (IsNullOrUndefined(value)) {
                ptr = nullptr;
            } else {
                ThrowError<Napi::TypeError>(env, "Unexpected %1 value, expected %2", GetValueType(instance, value), type->name);
                return false;
            }

            *(void **)origin = ptr;
        } break;

        case PrimitiveKind::Prototype: { RG_UNREACHABLE(); } break;
    }

#undef PUSH_INTEGER_SWAP
#undef PUSH_INTEGER

    // Keep memory around if any was allocated
    {
        BlockAllocator *alloc = call.GetAllocator();

        if (alloc->IsUsed()) {
            BlockAllocator *copy = instance->encode_map.FindValue(origin, nullptr);

            if (!copy) {
                copy = instance->encode_allocators.AppendDefault();
                instance->encode_map.Set(origin, copy);
            }

            std::swap(*alloc, *copy);
        }
    }

    return true;
}

Napi::Function WrapFunction(Napi::Env env, const FunctionInfo *func)
{
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    Napi::Function wrapper;
    if (func->variadic) {
        Napi::Function::Callback call = TranslateVariadicCall;
        wrapper = Napi::Function::New(env, call, func->name, (void *)func->Ref());
    } else {
        Napi::Function::Callback call = TranslateNormalCall;
        wrapper = Napi::Function::New(env, call, func->name, (void *)func->Ref());
    }
    wrapper.AddFinalizer([](Napi::Env, FunctionInfo *func) { func->Unref(); }, (FunctionInfo *)func);

    if (!func->variadic) {
        Napi::Function::Callback call = TranslateAsyncCall;
        Napi::Function async = Napi::Function::New(env, call, func->name, (void *)func->Ref());

        async.AddFinalizer([](Napi::Env, FunctionInfo *func) { func->Unref(); }, (FunctionInfo *)func);
        wrapper.Set("async", async);
    }

    // Create info object
    {
        Napi::Object meta = Napi::Object::New(env);
        Napi::Array arguments = Napi::Array::New(env, func->parameters.len);

        meta.Set("name", Napi::String::New(env, func->name));
        meta.Set("arguments", arguments);
        meta.Set("result", WrapType(env, instance, func->ret.type));

        for (Size i = 0; i < func->parameters.len; i++) {
            const ParameterInfo &param = func->parameters[i];
            arguments.Set((uint32_t)i, WrapType(env, instance, param.type));
        }

        wrapper.Set("info", meta);
    }

    return wrapper;
}

bool DetectCallConvention(Span<const char> name, CallConvention *out_convention)
{
    if (name == "__cdecl") {
        *out_convention = CallConvention::Cdecl;
        return true;
    } else if (name == "__stdcall") {
        *out_convention = CallConvention::Stdcall;
        return true;
    } else if (name == "__fastcall") {
        *out_convention = CallConvention::Fastcall;
        return true;
    } else if (name == "__thiscall") {
        *out_convention = CallConvention::Thiscall;
        return true;
    } else {
        return false;
    }
}

static int AnalyseFlatRec(const TypeInfo *type, int offset, int count, FunctionRef<void(const TypeInfo *type, int offset, int count)> func)
{
    if (type->primitive == PrimitiveKind::Record) {
        for (int i = 0; i < count; i++) {
            for (const RecordMember &member: type->members) {
                offset = AnalyseFlatRec(member.type, offset, 1, func);
            }
        }
    } else if (type->primitive == PrimitiveKind::Union) {
        for (int i = 0; i < count; i++) {
            for (const RecordMember &member: type->members) {
                AnalyseFlatRec(member.type, offset, 1, func);
            }
        }
        offset += count;
    } else if (type->primitive == PrimitiveKind::Array) {
        count *= type->size / type->ref.type->size;
        offset = AnalyseFlatRec(type->ref.type, offset, count, func);
    } else {
        func(type, offset, count);
        offset += count;
    }

    return offset;
}

int AnalyseFlat(const TypeInfo *type, FunctionRef<void(const TypeInfo *type, int offset, int count)> func)
{
    return AnalyseFlatRec(type, 0, 1, func);
}

void DumpMemory(const char *type, Span<const uint8_t> bytes)
{
    if (bytes.len) {
        PrintLn(stderr, "%1 at 0x%2 (%3):", type, bytes.ptr, FmtMemSize(bytes.len));

        for (const uint8_t *ptr = bytes.begin(); ptr < bytes.end();) {
            Print(stderr, "  [0x%1 %2 %3]  ", FmtArg(ptr).Pad0(-16),
                                              FmtArg((ptr - bytes.begin()) / sizeof(void *)).Pad(-4),
                                              FmtArg(ptr - bytes.begin()).Pad(-4));
            for (int i = 0; ptr < bytes.end() && i < (int)sizeof(void *); i++, ptr++) {
                Print(stderr, " %1", FmtHex(*ptr).Pad0(-2));
            }
            PrintLn(stderr);
        }
    }
}

}
