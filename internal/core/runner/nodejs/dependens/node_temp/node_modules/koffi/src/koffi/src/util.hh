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

#include <napi.h>

namespace RG {

extern const napi_type_tag TypeInfoMarker;
extern const napi_type_tag CastMarker;
extern const napi_type_tag MagicUnionMarker;

class MagicUnion: public Napi::ObjectWrap<MagicUnion> {
    const TypeInfo *type;

    napi_value active_symbol;
    Size active_idx = -1;

    HeapArray<uint8_t> raw;

public:
    static Napi::Function InitClass(Napi::Env env, const TypeInfo *type);

    MagicUnion(const Napi::CallbackInfo &info);

    const TypeInfo *GetType() { return type; }
    const RecordMember *GetMember() const { return (active_idx >= 0) ? &type->members[active_idx] : nullptr; }

    void SetRaw(const uint8_t *ptr);
    const uint8_t *GetRaw() const { return raw.ptr; }

private:
    Napi::Value Getter(const Napi::CallbackInfo &info);
    void Setter(const Napi::CallbackInfo &info, const Napi::Value &value);
};

template <typename T, typename... Args>
void ThrowError(Napi::Env env, const char *msg, Args... args)
{
    char buf[1024];
    Fmt(buf, msg, args...);

    auto err = T::New(env, buf);
    err.ThrowAsJavaScriptException();
}

static inline bool IsInteger(const TypeInfo *type)
{
    bool integer = ((int)type->primitive >= (int)PrimitiveKind::Int8 &&
                    (int)type->primitive <= (int)PrimitiveKind::UInt64);
    return integer;
}

static inline bool IsFloat(const TypeInfo *type)
{
    bool fp = (type->primitive == PrimitiveKind::Float32 ||
               type->primitive == PrimitiveKind::Float64);
    return fp;
}

static inline bool IsRegularSize(Size size, Size max)
{
    bool regular = (size <= max && !(size & (size - 1)));
    return regular;
}

const TypeInfo *ResolveType(Napi::Value value, int *out_directions = nullptr);
const TypeInfo *ResolveType(Napi::Env env, Span<const char> str, int *out_directions = nullptr);

const TypeInfo *MakePointerType(InstanceData *instance, const TypeInfo *ref, int count = 1);
const TypeInfo *MakeArrayType(InstanceData *instance, const TypeInfo *ref, Size len);
const TypeInfo *MakeArrayType(InstanceData *instance, const TypeInfo *ref, Size len, ArrayHint hint);

Napi::External<TypeInfo> WrapType(Napi::Env env, InstanceData *instance, const TypeInfo *type);

bool CanPassType(const TypeInfo *type, int directions);
bool CanReturnType(const TypeInfo *type);
bool CanStoreType(const TypeInfo *type);

// Can be slow, only use for error messages
const char *GetValueType(const InstanceData *instance, Napi::Value value);

void SetValueTag(const InstanceData *instance, Napi::Value value, const void *marker);
bool CheckValueTag(const InstanceData *instance, Napi::Value value, const void *marker);

static inline bool IsNullOrUndefined(Napi::Value value)
{
    return value.IsNull() || value.IsUndefined();
}

static inline bool IsObject(Napi::Value value)
{
    return value.IsObject() && !IsNullOrUndefined(value) && !value.IsArray();
}

static inline bool IsRawBuffer(Napi::Value value)
{
    return value.IsTypedArray() || value.IsArrayBuffer();
}

static inline Span<uint8_t> GetRawBuffer(Napi::Value value)
{
    if (value.IsTypedArray()) {
        Napi::TypedArray array = value.As<Napi::TypedArray>();
        Napi::ArrayBuffer buffer = array.ArrayBuffer();
        Size offset = array.ByteOffset();

        return MakeSpan((uint8_t *)buffer.Data() + offset, (Size)array.ByteLength());
    } else if (value.IsArrayBuffer()) {
        Napi::ArrayBuffer buffer = value.As<Napi::ArrayBuffer>();

        return MakeSpan((uint8_t *)buffer.Data(), (Size)buffer.ByteLength());
    }

    RG_UNREACHABLE();
}

int GetTypedArrayType(const TypeInfo *type);

template <typename T>
T GetNumber(Napi::Value value)
{
    RG_ASSERT(value.IsNumber() || value.IsBigInt());

    if (value.IsNumber()) [[likely]] {
        return (T)value.As<Napi::Number>().DoubleValue();
    } else if (value.IsBigInt()) {
        Napi::BigInt bigint = value.As<Napi::BigInt>();

        bool lossless;
        return (T)bigint.Uint64Value(&lossless);
    }

    RG_UNREACHABLE();
}

template <typename T>
Size NullTerminatedLength(const T *ptr)
{
    Size len = 0;
    while (ptr[len]) {
        len++;
    }
    return len;
}
template <typename T>
Size NullTerminatedLength(const T *ptr, Size max)
{
    Size len = 0;
    while (len < max && ptr[len]) {
        len++;
    }
    return len;
}

Napi::String MakeStringFromUTF32(Napi::Env env, const char32_t *ptr, Size len);
static inline Napi::String MakeStringFromUTF32(Napi::Env env, const char32_t *ptr)
    { return MakeStringFromUTF32(env, ptr, NullTerminatedLength(ptr)); }

Napi::Object DecodeObject(Napi::Env env, const uint8_t *origin, const TypeInfo *type);
void DecodeObject(Napi::Object obj, const uint8_t *origin, const TypeInfo *type);
Napi::Value DecodeArray(Napi::Env env, const uint8_t *origin, const TypeInfo *type);
void DecodeNormalArray(Napi::Array array, const uint8_t *origin, const TypeInfo *ref);
void DecodeBuffer(Span<uint8_t> buffer, const uint8_t *origin, const TypeInfo *ref);

Napi::Value Decode(Napi::Value value, Size offset, const TypeInfo *type, const Size *len = nullptr);
Napi::Value Decode(Napi::Env env, const uint8_t *ptr, const TypeInfo *type, const Size *len = nullptr);

bool Encode(Napi::Value ref, Size offset, Napi::Value value, const TypeInfo *type, const Size *len = nullptr);
bool Encode(Napi::Env env, uint8_t *ptr, Napi::Value value, const TypeInfo *type, const Size *len = nullptr);

static inline Napi::Value NewBigInt(Napi::Env env, int64_t value)
{
    if (value <= 9007199254740992ll && value >= -9007199254740992ll) {
        double d = (double)value;
        return Napi::Number::New(env, d);
    } else {
        return Napi::BigInt::New(env, value);
    }
}

static inline Napi::Value NewBigInt(Napi::Env env, uint64_t value)
{
    if (value <= 9007199254740992ull) {
        double d = (double)value;
        return Napi::Number::New(env, d);
    } else {
        return Napi::BigInt::New(env, value);
    }
}

static inline Napi::Array GetOwnPropertyNames(Napi::Object obj)
{
    Napi::Env env = obj.Env();

    napi_value result;
    napi_status status = napi_get_all_property_names(env, obj, napi_key_own_only,
                                                     (napi_key_filter)(napi_key_enumerable | napi_key_skip_symbols),
                                                     napi_key_numbers_to_strings, &result);
    RG_ASSERT(status == napi_ok);

    return Napi::Array(env, result);
}

Napi::Function WrapFunction(Napi::Env env, const FunctionInfo *func);

bool DetectCallConvention(Span<const char> name, CallConvention *out_convention);

int AnalyseFlat(const TypeInfo *type, FunctionRef<void(const TypeInfo *type, int offset, int count)> func);

void DumpMemory(const char *type, Span<const uint8_t> bytes);

}
