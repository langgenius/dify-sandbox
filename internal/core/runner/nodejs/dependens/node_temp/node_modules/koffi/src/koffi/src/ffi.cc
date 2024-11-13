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
#include "ffi.hh"
#include "call.hh"
#include "parser.hh"
#include "util.hh"
#ifdef _WIN32
    #include "win32.hh"
#endif
#include "errno.inc"

#ifdef _WIN32
    #ifndef NOMINMAX
        #define NOMINMAX
    #endif
    #ifndef WIN32_LEAN_AND_MEAN
        #define WIN32_LEAN_AND_MEAN
    #endif
    #include <windows.h>
    #include <ntsecapi.h>
#else
    #include <dlfcn.h>
    #include <unistd.h>
    #include <sys/mman.h>
    #ifndef MAP_STACK
        #define MAP_STACK 0
    #endif
#endif

#include <napi.h>

namespace RG {

SharedData shared;

static RG_THREAD_LOCAL CallData *exec_call;

static bool ChangeSize(const char *name, Napi::Value value, Size min_size, Size max_size, Size *out_size)
{
    Napi::Env env = value.Env();

    if (!value.IsNumber()) {
        InstanceData *instance = env.GetInstanceData<InstanceData>();

        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for '%2', expected number", GetValueType(instance, value), name);
        return false;
    }

    int64_t size = value.As<Napi::Number>().Int64Value();

    if (size < min_size || size > max_size) {
        ThrowError<Napi::Error>(env, "Setting '%1' must be between %2 and %3", name, FmtMemSize(min_size), FmtMemSize(max_size));
        return false;
    }

    *out_size = (Size)size;
    return true;
}

static bool ChangeMemorySize(const char *name, Napi::Value value, Size *out_size)
{
    const Size MinSize = Kibibytes(1);
    const Size MaxSize = Mebibytes(16);

    return ChangeSize(name, value, MinSize, MaxSize, out_size);
}

static bool ChangeAsyncLimit(const char *name, Napi::Value value, int max, int *out_limit)
{
    Napi::Env env = value.Env();

    if (!value.IsNumber()) {
        InstanceData *instance = env.GetInstanceData<InstanceData>();

        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for '%2', expected number", GetValueType(instance, value), name);
        return false;
    }

    int64_t n = value.As<Napi::Number>().Int64Value();

    if (n < 0 || n > max) {
        ThrowError<Napi::Error>(env, "Setting '%1' must be between 0 and %2", name, max);
        return false;
    }

    *out_limit = (int)n;
    return true;
}

static Napi::Value GetSetConfig(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length()) {
        if (instance->memories.len) {
            ThrowError<Napi::Error>(env, "Cannot change Koffi configuration once a library has been loaded");
            return env.Null();
        }

        if (!info[0].IsObject()) {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value for config, expected object", GetValueType(instance, info[0]));
            return env.Null();
        }

        decltype(instance->config) new_config = instance->config;
        int max_async_calls = new_config.resident_async_pools + new_config.max_temporaries;

        Napi::Object obj = info[0].As<Napi::Object>();
        Napi::Array keys = GetOwnPropertyNames(obj);

        for (uint32_t i = 0; i < keys.Length(); i++) {
            std::string key = keys.Get(i).As<Napi::String>();
            Napi::Value value = obj[key];

            if (key == "sync_stack_size") {
                if (!ChangeMemorySize(key.c_str(), value, &new_config.sync_stack_size))
                    return env.Null();
            } else if (key == "sync_heap_size") {
                if (!ChangeMemorySize(key.c_str(), value, &new_config.sync_heap_size))
                    return env.Null();
            } else if (key == "async_stack_size") {
                if (!ChangeMemorySize(key.c_str(), value, &new_config.async_stack_size))
                    return env.Null();
            } else if (key == "async_heap_size") {
                if (!ChangeMemorySize(key.c_str(), value, &new_config.async_heap_size))
                    return env.Null();
            } else if (key == "resident_async_pools") {
                if (!ChangeAsyncLimit(key.c_str(), value, RG_LEN(instance->memories.data) - 1, &new_config.resident_async_pools))
                    return env.Null();
            } else if (key == "max_async_calls") {
                if (!ChangeAsyncLimit(key.c_str(), value, MaxAsyncCalls, &max_async_calls))
                    return env.Null();
            } else if (key == "max_type_size") {
                if (!ChangeSize(key.c_str(), value, 32, Mebibytes(512), &new_config.max_type_size))
                    return env.Null();
            } else {
                ThrowError<Napi::Error>(env, "Unexpected config member '%1'", key.c_str());
                return env.Null();
            }
        }

        if (max_async_calls < new_config.resident_async_pools) {
            ThrowError<Napi::Error>(env, "Setting max_async_calls must be >= to resident_async_pools");
            return env.Null();
        }

        new_config.max_temporaries =  max_async_calls - new_config.resident_async_pools;
        instance->config = new_config;
    }

    Napi::Object obj = Napi::Object::New(env);

    obj.Set("sync_stack_size", instance->config.sync_stack_size);
    obj.Set("sync_heap_size", instance->config.sync_heap_size);
    obj.Set("async_stack_size", instance->config.async_stack_size);
    obj.Set("async_heap_size", instance->config.async_heap_size);
    obj.Set("resident_async_pools", instance->config.resident_async_pools);
    obj.Set("max_async_calls", instance->config.resident_async_pools + instance->config.max_temporaries);
    obj.Set("max_type_size", instance->config.max_type_size);

    return obj;
}

static Napi::Value GetStats(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    Napi::Object obj = Napi::Object::New(env);

    obj.Set("disposed", instance->stats.disposed);

    return obj;
}

static inline bool CheckAlignment(int64_t align)
{
    bool valid = (align > 0) && (align <= 8 && !(align & (align - 1)));
    return valid;
}

static Napi::Value CreateStructType(const Napi::CallbackInfo &info, bool pad)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 or 2 arguments, got %1", info.Length());
        return env.Null();
    }

    bool named = info.Length() > 1;

    if (named && !info[0].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string", GetValueType(instance, info[0]));
        return env.Null();
    }
    if (!IsObject(info[named])) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for members, expected object", GetValueType(instance, info[1]));
        return env.Null();
    }

    Napi::String name = info[0].As<Napi::String>();
    Napi::Object obj = info[named].As<Napi::Object>();
    Napi::Array keys = GetOwnPropertyNames(obj);

    RG_DEFER_NC(err_guard, len = instance->types.len) {
        Size start = len + !named;

        for (Size i = start; i < instance->types.len; i++) {
            const TypeInfo *it = &instance->types[i];
            const TypeInfo **ptr = instance->types_map.Find(it->name);

            if (ptr && *ptr == it) {
                instance->types_map.Remove(ptr);
            }
        }

        instance->types.RemoveFrom(len);
    };

    TypeInfo *type = instance->types.AppendDefault();

    if (named) {
        type->name = DuplicateString(name.Utf8Value().c_str(), &instance->str_alloc).ptr;

        bool inserted;
        instance->types_map.TrySet(type->name, type, &inserted);

        if (!inserted) {
            ThrowError<Napi::Error>(env, "Duplicate type name '%1'", type->name);
            return env.Null();
        }
    } else {
        type->name = Fmt(&instance->str_alloc, "<anonymous_%1>", instance->types.len).ptr;
    }

    type->primitive = PrimitiveKind::Record;
    type->align = 1;
    type->flags = (int)TypeFlag::IsIncomplete;

    HashSet<const char *> members;
    int64_t size = 0;

    for (uint32_t i = 0; i < keys.Length(); i++) {
        RecordMember member = {};

        std::string key = keys.Get(i).As<Napi::String>();
        Napi::Value value = obj[key];
        int16_t align = 0;

        member.name = DuplicateString(key.c_str(), &instance->str_alloc).ptr;

        if (value.IsArray()) {
            Napi::Array array = value.As<Napi::Array>();

            if (array.Length() != 2 || !array.Get(0u).IsNumber()) {
                ThrowError<Napi::Error>(env, "Member specifier array must contain alignement value and type");
                return env.Null();
            }

            int64_t align64 = array.Get(0u).As<Napi::Number>().Int64Value();

            if (!CheckAlignment(align64)) {
                ThrowError<Napi::Error>(env, "Alignment of member '%1' must be 1, 2, 4 or 8", member.name);
                return env.Null();
            }

            value = array[1u];
            align = (int16_t)align64;
        }

        member.type = ResolveType(value);
        if (!member.type)
            return env.Null();
        if (!CanStoreType(member.type)) {
            ThrowError<Napi::TypeError>(env, "Type %1 cannot be used as a member (maybe try %1 *)", member.type->name);
            return env.Null();
        }

        if (!align) {
            align = pad ? member.type->align : 1;
        }
        member.offset = (int32_t)AlignLen(size, align);

        size = member.offset + member.type->size;
        type->align = std::max(type->align, align);

        if (size > instance->config.max_type_size) {
            ThrowError<Napi::Error>(env, "Struct '%1' size is too high (max = %2)", type->name, FmtMemSize(size));
            return env.Null();
        }

        bool inserted;
        members.TrySet(member.name, &inserted);

        if (!inserted) {
            ThrowError<Napi::Error>(env, "Duplicate member '%1' in struct '%2'", member.name, type->name);
            return env.Null();
        }

        type->members.Append(member);
    }

    size = (int32_t)AlignLen(size, type->align);
    if (!size) {
        ThrowError<Napi::Error>(env, "Empty struct '%1' is not allowed in C", type->name);
        return env.Null();
    }
    type->size = (int32_t)size;

    type->flags &= ~(int)TypeFlag::IsIncomplete;
    err_guard.Disable();

    return WrapType(env, instance, type);
}

static Napi::Value CreatePaddedStructType(const Napi::CallbackInfo &info)
{
    return CreateStructType(info, true);
}

static Napi::Value CreatePackedStructType(const Napi::CallbackInfo &info)
{
    return CreateStructType(info, false);
}

static Napi::Value CreateUnionType(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 or 2 arguments, got %1", info.Length());
        return env.Null();
    }

    bool named = info.Length() > 1;

    if (named && !info[0].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string", GetValueType(instance, info[0]));
        return env.Null();
    }
    if (!IsObject(info[named])) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for members, expected object", GetValueType(instance, info[1]));
        return env.Null();
    }

    Napi::String name = info[0].As<Napi::String>();
    Napi::Object obj = info[named].As<Napi::Object>();
    Napi::Array keys = GetOwnPropertyNames(obj);

    RG_DEFER_NC(err_guard, len = instance->types.len) {
        Size start = len + !named;

        for (Size i = start; i < instance->types.len; i++) {
            const TypeInfo *it = &instance->types[i];
            const TypeInfo **ptr = instance->types_map.Find(it->name);

            if (ptr && *ptr == it) {
                instance->types_map.Remove(ptr);
            }
        }

        instance->types.RemoveFrom(len);
    };

    TypeInfo *type = instance->types.AppendDefault();

    if (named) {
        type->name = DuplicateString(name.Utf8Value().c_str(), &instance->str_alloc).ptr;

        bool inserted;
        instance->types_map.TrySet(type->name, type, &inserted);

        if (!inserted) {
            ThrowError<Napi::Error>(env, "Duplicate type name '%1'", type->name);
            return env.Null();
        }
    } else {
        type->name = Fmt(&instance->str_alloc, "<anonymous_%1>", instance->types.len).ptr;
    }

    type->primitive = PrimitiveKind::Union;
    type->align = 1;
    type->flags = (int)TypeFlag::IsIncomplete;

    HashSet<const char *> members;
    int32_t size = 0;

    for (uint32_t i = 0; i < keys.Length(); i++) {
        RecordMember member = {};

        std::string key = keys.Get(i).As<Napi::String>();
        Napi::Value value = obj[key];
        int16_t align = 0;

        member.name = DuplicateString(key.c_str(), &instance->str_alloc).ptr;

        if (value.IsArray()) {
            Napi::Array array = value.As<Napi::Array>();

            if (array.Length() != 2 || !array.Get(0u).IsNumber()) {
                ThrowError<Napi::Error>(env, "Member specifier array must contain alignement value and type");
                return env.Null();
            }

            int64_t align64 = array.Get(0u).As<Napi::Number>().Int64Value();

            if (!CheckAlignment(align64)) {
                ThrowError<Napi::Error>(env, "Alignment of member '%1' must be 1, 2, 4 or 8", member.name);
                return env.Null();
            }

            value = array[1u];
            align = (int16_t)align64;
        }

        member.type = ResolveType(value);
        if (!member.type)
            return env.Null();
        if (!CanStoreType(member.type)) {
            ThrowError<Napi::TypeError>(env, "Type %1 cannot be used as a member (maybe try %1 *)", member.type->name);
            return env.Null();
        }

        align = align ? align : member.type->align;
        size = std::max(size, member.type->size);
        type->align = std::max(type->align, align);

        bool inserted;
        members.TrySet(member.name, &inserted);

        if (!inserted) {
            ThrowError<Napi::Error>(env, "Duplicate member '%1' in union '%2'", member.name, type->name);
            return env.Null();
        }

        type->members.Append(member);
    }

    size = (int32_t)AlignLen(size, type->align);
    if (!size) {
        ThrowError<Napi::Error>(env, "Empty union '%1' is not allowed in C", type->name);
        return env.Null();
    }
    type->size = (int32_t)size;

    type->flags &= ~(int)TypeFlag::IsIncomplete;
    err_guard.Disable();

    // Union constructor
    Napi::Function constructor = MagicUnion::InitClass(env, type);
    type->construct.Reset(constructor, 1);

    return WrapType(env, instance, type);
}

Napi::Value InstantiateUnion(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (!info.IsConstructCall()) {
        ThrowError<Napi::TypeError>(env, "This function is a constructor and must be called with new");
        return env.Null();
    }
    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();
    if (type->primitive != PrimitiveKind::Union) {
        ThrowError<Napi::TypeError>(env, "Expected union type, got %1", PrimitiveKindNames[(int)type->primitive]);
        return env.Null();
    }

    Napi::Object wrapper = type->construct.New({}).As<Napi::Object>();
    SetValueTag(instance, wrapper, &MagicUnionMarker);

    return wrapper;
}

static Napi::Value CreateOpaqueType(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    bool named = (info.Length() >= 1);

    if (named && !info[0].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string", GetValueType(instance, info[0]));
        return env.Null();
    }

    Napi::String name = info[0].As<Napi::String>();    

    TypeInfo *type = instance->types.AppendDefault();
    RG_DEFER_N(err_guard) { instance->types.RemoveLast(1); };

    type->name = named ? DuplicateString(name.Utf8Value().c_str(), &instance->str_alloc).ptr
                       : Fmt(&instance->str_alloc, "<anonymous_%1>", instance->types.len).ptr;

    type->primitive = PrimitiveKind::Void;
    type->size = 0;
    type->align = 0;

    // If the insert succeeds, we cannot fail anymore
    if (named) {
        bool inserted;
        instance->types_map.TrySet(type->name, type, &inserted);

        if (!inserted) {
            ThrowError<Napi::Error>(env, "Duplicate type name '%1'", type->name);
            return env.Null();
        }
    }
    err_guard.Disable();

    return WrapType(env, instance, type);
}

static Napi::Value CreatePointerType(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 to 3 arguments, got %1", info.Length());
        return env.Null();
    }

    bool named = (info.Length() >= 2 && !info[1].IsNumber());

    if (named && !info[0].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string", GetValueType(instance, info[0]));
        return env.Null();
    }

    std::string name = named ? info[0].As<Napi::String>() : std::string();

    const TypeInfo *type = ResolveType(info[named]);
    if (!type)
        return env.Null();

    int count = 0;
    if (info.Length() >= 2u + named) {
        if (!info[1 + named].IsNumber()) {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value for count, expected number", GetValueType(instance, info[1 + named]));
            return env.Null();
        }

        count = info[1 + named].As<Napi::Number>();

        if (count < 1 || count > 4) {
            ThrowError<Napi::TypeError>(env, "Value of count must be between 1 and 4");
            return env.Null();
        }
    } else {
        count = 1;
    }

    type = MakePointerType(instance, type, count);
    RG_ASSERT(type);

    if (named) {
        TypeInfo *copy = instance->types.AppendDefault();
        RG_DEFER_N(err_guard) { instance->types.RemoveLast(1); };

        memcpy((void *)copy, type, RG_SIZE(*type));
        copy->name = DuplicateString(name.c_str(), &instance->str_alloc).ptr;

        bool inserted;
        instance->types_map.TrySet(copy->name, copy, &inserted);

        // If the insert succeeds, we cannot fail anymore
        if (!inserted) {
            ThrowError<Napi::Error>(env, "Duplicate type name '%1'", copy->name);
            return env.Null();
        }
        err_guard.Disable();

        type = copy;
    }

    return WrapType(env, instance, type);
}

static Napi::Value EncodePointerDirection(const Napi::CallbackInfo &info, int directions)
{
    RG_ASSERT(directions >= 1 && directions <= 3);

    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();

    if (type->primitive != PrimitiveKind::Pointer &&
            type->primitive != PrimitiveKind::String &&
            type->primitive != PrimitiveKind::String16) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 type, expected pointer or string type", type->name);
        return env.Null();
    }

    // Embed direction in unused pointer bits
    const TypeInfo *marked = (const TypeInfo *)((uint8_t *)type + directions - 1);

    return WrapType(env, instance, marked);
}

static Napi::Value MarkIn(const Napi::CallbackInfo &info)
{
    return EncodePointerDirection(info, 1);
}

static Napi::Value MarkOut(const Napi::CallbackInfo &info)
{
    return EncodePointerDirection(info, 2);
}

static Napi::Value MarkInOut(const Napi::CallbackInfo &info)
{
    return EncodePointerDirection(info, 3);
}

static Napi::Value CreateDisposableType(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 or 2 arguments, got %1", info.Length());
        return env.Null();
    }

    bool named = (info.Length() >= 2 && !info[1].IsFunction());

    if (named && !info[0].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string", GetValueType(instance, info[0]));
        return env.Null();
    }

    Napi::String name = info[0].As<Napi::String>();

    const TypeInfo *src = ResolveType(info[named]);
    if (!src)
        return env.Null();
    if (src->primitive != PrimitiveKind::Pointer &&
            src->primitive != PrimitiveKind::String &&
            src->primitive != PrimitiveKind::String16) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 type, expected pointer or string type", src->name);
        return env.Null();
    }
    if (src->dispose) {
        ThrowError<Napi::TypeError>(env, "Cannot use disposable type '%1' to create new disposable", src->name);
        return env.Null();
    }

    DisposeFunc *dispose;
    Napi::Function dispose_func;
    if (info.Length() >= 2u + named && !IsNullOrUndefined(info[1 + named])) {
        Napi::Function func = info[1 + named].As<Napi::Function>();

        if (!func.IsFunction()) {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value for func, expected function", GetValueType(instance, func));
            return env.Null();
        }

        dispose = [](Napi::Env env, const TypeInfo *type, const void *ptr) {
            InstanceData *instance = env.GetInstanceData<InstanceData>();
            const Napi::FunctionReference &ref = type->dispose_ref;

            Napi::External<void> external = Napi::External<void>::New(env, (void *)ptr);
            SetValueTag(instance, external, type->ref.marker);

            Napi::Value self = env.Null();
            napi_value args[] = {
                external
            };

            ref.Call(self, RG_LEN(args), args);
            instance->stats.disposed++;
        };
        dispose_func = func;
    } else {
        dispose = [](Napi::Env env, const TypeInfo *, const void *ptr) {
            InstanceData *instance = env.GetInstanceData<InstanceData>();

            free((void *)ptr);
            instance->stats.disposed++;
        };
    }

    TypeInfo *type = instance->types.AppendDefault();
    RG_DEFER_N(err_guard) { instance->types.RemoveLast(1); };

    memcpy((void *)type, (const void *)src, RG_SIZE(*src));
    type->members.allocator = GetNullAllocator();

    type->name = named ? DuplicateString(name.Utf8Value().c_str(), &instance->str_alloc).ptr
                       : Fmt(&instance->str_alloc, "<anonymous_%1>", instance->types.len).ptr;

    type->dispose = dispose;
    type->dispose_ref = Napi::Persistent(dispose_func);

    // If the insert succeeds, we cannot fail anymore
    if (named) {
        bool inserted;
        instance->types_map.TrySet(type->name, type, &inserted);

        if (!inserted) {
            ThrowError<Napi::Error>(env, "Duplicate type name '%1'", type->name);
            return env.Null();
        }
    }
    err_guard.Disable();

    return WrapType(env, instance, type);
}

static inline bool GetExternalPointer(Napi::Env env, Napi::Value value, void **out_ptr)
{
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (IsNullOrUndefined(value)) {
        *out_ptr = 0;
        return true;
    } else if (value.IsExternal() && !CheckValueTag(instance, value, &TypeInfoMarker) &&
                                     !CheckValueTag(instance, value, &CastMarker) &&
                                     !CheckValueTag(instance, value, &MagicUnionMarker)) {
        Napi::External<void> external = value.As<Napi::External<void>>();
        void *ptr = external.Data();

        *out_ptr = ptr;
        return true;
    } else {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for ptr, expected external pointer", GetValueType(instance, value));
        return false;
    }
}

static Napi::Value CallAlloc(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 2) {
        ThrowError<Napi::TypeError>(env, "Expected 2 arguments, got %1", info.Length());
        return env.Null();
    }
    if (!info[1].IsNumber()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for length, expected number", GetValueType(instance, info[1]));
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();

    if (!type->size) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Cannot allocate memory for zero-sized type %1", type->name);
        return env.Null();
    }

    int32_t len = info[1].As<Napi::Number>();

    if (len <= 0) [[unlikely]] {
        ThrowError<Napi::Error>(env, "Size must be greater than 0");
        return env.Null();
    }
    if (len > INT32_MAX / type->size) [[unlikely]] {
        ThrowError<Napi::Error>(env, "Cannot allocate more than %1 objects of type %2", INT32_MAX / type->size, type->name);
        return env.Null();
    }

    void *ptr = calloc((size_t)len, (size_t)type->size);

    if (!ptr) [[unlikely]] {
        Size size = (Size)(len * type->size);

        ThrowError<Napi::Error>(env, "Failed to allocate %1 of memory", FmtMemSize((Size)size));
        return env.Null();
    }

    Napi::External<void> external = Napi::External<void>::New(env, ptr);
    SetValueTag(instance, external, type);

    return external;
}

static Napi::Value CallFree(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    void *ptr = nullptr;
    if (!GetExternalPointer(env, info[0], &ptr))
        return env.Null();

    free(ptr);

    return env.Undefined();
}

static Napi::Value GetOrSetErrNo(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() >= 1) {
        Napi::Number value = info[0].As<Napi::Number>();

        if (!value.IsNumber()) {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value for errno, expected integer", GetValueType(instance, value));
            return env.Null();
        }

        errno = value;
    }

    Napi::Number ret = Napi::Number::New(env, errno);
    return ret;
}

static Napi::Value CreateArrayType(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 2) {
        ThrowError<Napi::TypeError>(env, "Expected 2 arguments, got %1", info.Length());
        return env.Null();
    }
    if (!info[1].IsNumber()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for length, expected integer", GetValueType(instance, info[1]));
        return env.Null();
    }

    const TypeInfo *ref = ResolveType(info[0]);
    int64_t len = info[1].As<Napi::Number>().Int64Value();

    if (!ref)
        return env.Null();
    if (len <= 0) {
        ThrowError<Napi::TypeError>(env, "Array length must be positive and non-zero");
        return env.Null();
    }
    if (len > instance->config.max_type_size / ref->size) {
        ThrowError<Napi::TypeError>(env, "Array length is too high (max = %1)", instance->config.max_type_size / ref->size);
        return env.Null();
    }

    const TypeInfo *type = nullptr;

    if (info.Length() >= 3 && !IsNullOrUndefined(info[2])) {
        if (!info[2].IsString()) {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value for hint, expected string", GetValueType(instance, info[2]));
            return env.Null();
        }

        std::string to = info[2].As<Napi::String>();
        ArrayHint hint = {};

        if (to == "Typed" || to == "typed") {
            if (!(ref->flags & (int)TypeFlag::HasTypedArray)) {
                ThrowError<Napi::Error>(env, "Array hint 'Typed' cannot be used with type %1", ref->name);
                return env.Null();
            }

            hint = ArrayHint::Typed;
        } else if (to == "Array" || to == "array") {
            hint = ArrayHint::Array;
        } else if (to == "String" || to == "string") {
            if (ref->primitive != PrimitiveKind::Int8 && ref->primitive != PrimitiveKind::Int16) {
                ThrowError<Napi::Error>(env, "Array hint 'String' can only be used with 8 and 16-bit signed integer types");
                return env.Null();
            }

            hint = ArrayHint::String;
        } else {
            ThrowError<Napi::Error>(env, "Array conversion hint must be 'Typed', 'Array' or 'String'");
            return env.Null();
        }

        type = MakeArrayType(instance, ref, len, hint);
    } else {
        type = MakeArrayType(instance, ref, len);
    }

    return WrapType(env, instance, type);
}

static bool ParseClassicFunction(const Napi::CallbackInfo &info, FunctionInfo *out_func)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    Napi::String name = info[0u].As<Napi::String>();
    Napi::Value ret = info[1u];
    Napi::Array parameters = info[2u].As<Napi::Array>();

    // Detect optional call convention
    if (name.IsString() && DetectCallConvention(name.Utf8Value().c_str(), &out_func->convention)) {
        if (info.Length() < 4) {
            ThrowError<Napi::TypeError>(env, "Expected 4 arguments, got %1", info.Length());
            return false;
        }

        name = info[1u].As<Napi::String>();
        ret = info[2u];
        parameters = info[3u].As<Napi::Array>();
    }

#ifdef _WIN32
    if (name.IsNumber()) {
        out_func->ordinal_name = name.As<Napi::Number>().Int32Value();
        name = name.ToString();
    }
#endif
    if (!name.IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string or integer", GetValueType(instance, name));
        return false;
    }

    out_func->name = DuplicateString(name.ToString().Utf8Value().c_str(), &instance->str_alloc).ptr;

    out_func->ret.type = ResolveType(ret);
    if (!out_func->ret.type)
        return false;
    if (!CanReturnType(out_func->ret.type)) {
        ThrowError<Napi::TypeError>(env, "You are not allowed to directly return %1 values (maybe try %1 *)", out_func->ret.type->name);
        return false;
    }

    if (!parameters.IsArray()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for parameters of '%2', expected an array", GetValueType(instance, parameters), out_func->name);
        return false;
    }

    uint32_t parameters_len = parameters.Length();

    if (parameters_len) {
        Napi::String str = parameters.Get(parameters_len - 1).As<Napi::String>();

        if (str.IsString() && str.Utf8Value() == "...") {
            out_func->variadic = true;
            parameters_len--;
        }
    }

    for (uint32_t j = 0; j < parameters_len; j++) {
        ParameterInfo param = {};

        param.type = ResolveType(parameters[j], &param.directions);

        if (!param.type)
            return false;
        if (!CanPassType(param.type, param.directions)) {
            ThrowError<Napi::TypeError>(env, "Type %1 cannot be used as a parameter", param.type->name);
            return false;
        }
        if (out_func->parameters.len >= MaxParameters) {
            ThrowError<Napi::TypeError>(env, "Functions cannot have more than %1 parameters", MaxParameters);
            return false;
        }
        if ((param.directions & 2) && ++out_func->out_parameters >= MaxParameters) {
            ThrowError<Napi::TypeError>(env, "Functions cannot have more than %1 output parameters", MaxParameters);
            return false;
        }

        param.offset = (int8_t)j;

        out_func->parameters.Append(param);
    }

    out_func->required_parameters = (int8_t)out_func->parameters.len;

    return true;
}

static Napi::Value CreateFunctionType(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    FunctionInfo *func = instance->callbacks.AppendDefault();
    RG_DEFER_N(err_guard) { instance->callbacks.RemoveLast(1); };

    if (info.Length() >= 3) {
        if (!ParseClassicFunction(info, func))
            return env.Null();
    } else if (info.Length() >= 1) {
        if (!info[0].IsString()) {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value for prototype, expected string", GetValueType(instance, info[0]));
            return env.Null();
        }

        std::string proto = info[0u].As<Napi::String>();
        if (!ParsePrototype(env, proto.c_str(), func))
            return env.Null();
    } else {
        ThrowError<Napi::TypeError>(env, "Expected 1 or 3 arguments, got %1", info.Length());
        return env.Null();
    }

    if (!AnalyseFunction(env, instance, func))
        return env.Null();

    // Adjust parameter offsets for koffi.call()
    for (ParameterInfo &param: func->parameters) {
        param.offset += 2;
    }
    func->required_parameters += 2;

    // We cannot fail after this check
    if (instance->types_map.Find(func->name)) {
        ThrowError<Napi::Error>(env, "Duplicate type name '%1'", func->name);
        return env.Null();
    }
    err_guard.Disable();

    TypeInfo *type = instance->types.AppendDefault();

    type->name = func->name;

    type->primitive = PrimitiveKind::Prototype;
    type->align = alignof(void *);
    type->size = RG_SIZE(void *);
    type->ref.proto = func;

    instance->types_map.Set(type->name, type);

    return WrapType(env, instance, type);
}

static Napi::Value CreateTypeAlias(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 2) {
        ThrowError<Napi::TypeError>(env, "Expected 2 arguments, got %1", info.Length());
        return env.Null();
    }
    if (!info[0].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string", GetValueType(instance, info[0]));
        return env.Null();
    }

    std::string name = info[0].As<Napi::String>();
    const char *alias = DuplicateString(name.c_str(), &instance->str_alloc).ptr;

    const TypeInfo *type = ResolveType(info[1]);
    if (!type)
        return env.Null();

    // Alias the type
    {
        bool inserted;
        instance->types_map.TrySet(alias, type, &inserted);

        if (!inserted) {
            ThrowError<Napi::Error>(env, "Type name '%1' already exists", alias);
            return env.Null();
        }
    }

    return WrapType(env, instance, type);
}

static Napi::Value GetTypeSize(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();

    return Napi::Number::New(env, type->size);
}

static Napi::Value GetTypeAlign(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();

    return Napi::Number::New(env, type->align);
}

static Napi::Value GetMemberOffset(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 2) {
        ThrowError<Napi::TypeError>(env, "Expected 2 arguments, got %1", info.Length());
        return env.Null();
    }
    if (!info[1].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for member, expected string", GetValueType(instance, info[1]));
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();
    if (type->primitive != PrimitiveKind::Record) {
        ThrowError<Napi::TypeError>(env, "The offsetof() function can only be used with record types");
        return env.Null();
    }

    std::string name = info[1].As<Napi::String>();

    const RecordMember *member = std::find_if(type->members.begin(), type->members.end(),
        [&](const RecordMember &member) { return TestStr(member.name, name.c_str()); });
    if (member == type->members.end()) {
        ThrowError<Napi::Error>(env, "Record type %1 does not have member '%2'", type->name, name.c_str());
        return env.Null();
    }

    return Napi::Number::New(env, member->offset);
}

static Napi::Value GetResolvedType(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();

    return WrapType(env, instance, type);
}

static Napi::Value GetTypeDefinition(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[0]);
    if (!type)
        return env.Null();

    if (type->defn.IsEmpty()) {
        Napi::Object defn = Napi::Object::New(env);

        defn.Set("name", Napi::String::New(env, type->name));
        defn.Set("primitive", PrimitiveKindNames[(int)type->primitive]);
        defn.Set("size", Napi::Number::New(env, (double)type->size));
        defn.Set("alignment", Napi::Number::New(env, (double)type->align));
        defn.Set("disposable", Napi::Boolean::New(env, !!type->dispose));

        switch (type->primitive) {
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
            case PrimitiveKind::Float32:
            case PrimitiveKind::Float64:
            case PrimitiveKind::Prototype:
            case PrimitiveKind::Callback: {} break;

            case PrimitiveKind::Array: {
                uint32_t len = type->size / type->ref.type->size;
                defn.Set("length", Napi::Number::New(env, (double)len));
                defn.Set("hint", ArrayHintNames[(int)type->hint]);
            } [[fallthrough]];
            case PrimitiveKind::Pointer: {
                Napi::Value value = WrapType(env, instance, type->ref.type);
                defn.Set("ref", value);
            } break;
            case PrimitiveKind::Record:
            case PrimitiveKind::Union: {
                Napi::Object members = Napi::Object::New(env);

                for (const RecordMember &member: type->members) {
                    Napi::Object obj = Napi::Object::New(env);

                    obj.Set("name", member.name);
                    obj.Set("type", WrapType(env, instance, member.type));
                    obj.Set("offset", member.offset);

                    members.Set(member.name, obj);
                }

                defn.Set("members", members);
            } break;
        }

        defn.Freeze();
        type->defn.Reset(defn, 1);
    }

    return type->defn.Value();
}

static InstanceMemory *AllocateMemory(InstanceData *instance, Size stack_size, Size heap_size)
{
    std::lock_guard<std::mutex> lock(instance->memories_mutex);

    for (Size i = 1; i < instance->memories.len; i++) {
        InstanceMemory *mem = instance->memories[i];

        if (!mem->busy) {
            mem->busy = true;
            return mem;
        }
    }

    bool temporary = (instance->memories.len > instance->config.resident_async_pools);

    if (temporary && instance->temporaries >= instance->config.max_temporaries) [[unlikely]]
        return nullptr;

    InstanceMemory *mem = new InstanceMemory();
    RG_DEFER_N(mem_guard) { delete mem; };

    stack_size = AlignLen(stack_size, Kibibytes(64));

#if defined(_WIN32)
    // Allocate stack memory
    mem->stack.len = stack_size;
    mem->stack.ptr = (uint8_t *)VirtualAlloc(nullptr, mem->stack.len, MEM_RESERVE | MEM_COMMIT, PAGE_READWRITE);

    RG_CRITICAL(mem->stack.ptr, "Failed to allocate %1 of memory", mem->stack.len);
#else
    mem->stack.len = stack_size;
    mem->stack.ptr = (uint8_t *)mmap(nullptr, mem->stack.len, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANON | MAP_STACK, -1, 0);

    RG_CRITICAL(mem->stack.ptr, "Failed to allocate %1 of memory", mem->stack.len);
#endif

#ifdef __OpenBSD__
    // Make sure the SP points inside the MAP_STACK area, or (void) functions may crash on OpenBSD i386
    mem->stack.len -= 16;
#endif

    // Keep real stack limits intact, in case we need them
    mem->stack0 = mem->stack;

    mem->heap.len = heap_size;
#ifdef _WIN32
    mem->heap.ptr = (uint8_t *)VirtualAlloc(nullptr, mem->heap.len, MEM_COMMIT | MEM_RESERVE, PAGE_READWRITE);
#else
    mem->heap.ptr = (uint8_t *)mmap(nullptr, mem->heap.len, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANON, -1, 0);
#endif
    RG_CRITICAL(mem->heap.ptr, "Failed to allocate %1 of memory", mem->heap.len);

    if (temporary) {
        instance->temporaries++;
        mem->temporary = true;
    } else {
        instance->memories.Append(mem);
        mem->temporary = false;
    }

    mem->busy = true;

    mem_guard.Disable();
    return mem;
}

static Napi::Value TranslateNormalCall(const FunctionInfo *func, void *native,
                                       const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < (uint32_t)func->required_parameters) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Expected %1 arguments, got %2", func->parameters.len, info.Length());
        return env.Null();
    }

    InstanceMemory *mem = instance->memories[0];
    CallData call(env, instance, mem);

    if (!call.Prepare(func, info)) [[unlikely]]
        return env.Null();

    if (instance->debug) {
        call.DumpForward(func);
    }

    // Execute call
    {
        RG_DEFER_C(prev_call = exec_call) { exec_call = prev_call; };
        exec_call = &call;

        call.Execute(func, native);
    }

    return call.Complete(func);
}

Napi::Value TranslateNormalCall(const Napi::CallbackInfo &info)
{
    FunctionInfo *func = (FunctionInfo *)info.Data();
    return TranslateNormalCall(func, func->native, info);
}

static Napi::Value TranslateVariadicCall(const FunctionInfo *func, void *native,
                                         const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    FunctionInfo copy;
    memcpy((void *)&copy, func, RG_SIZE(*func));
    copy.lib = nullptr;

    // This makes variadic calls non-reentrant
    RG_DEFER_C(len = copy.parameters.len) {
        copy.parameters.RemoveFrom(len);
        copy.parameters.Leak();
    };

    if (info.Length() < (uint32_t)copy.required_parameters) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Expected %1 arguments or more, got %2", copy.parameters.len, info.Length());
        return env.Null();
    }
    if ((info.Length() - copy.required_parameters) % 2) [[unlikely]] {
        ThrowError<Napi::Error>(env, "Missing value argument for variadic call");
        return env.Null();
    }

    for (Size i = copy.required_parameters; i < (Size)info.Length(); i += 2) {
        ParameterInfo param = {};

        param.type = ResolveType(info[(uint32_t)i], &param.directions);

        if (!param.type) [[unlikely]]
            return env.Null();
        if (!CanPassType(param.type, param.directions)) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Type %1 cannot be used as a parameter", param.type->name);
            return env.Null();
        }
        if (copy.parameters.len >= MaxParameters) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Functions cannot have more than %1 parameters", MaxParameters);
            return env.Null();
        }
        if ((param.directions & 2) && ++copy.out_parameters >= MaxParameters) [[unlikely]] {
            ThrowError<Napi::TypeError>(env, "Functions cannot have more than %1 output parameters", MaxParameters);
            return env.Null();
        }

        param.variadic = true;
        param.offset = (int8_t)(i + 1);

        copy.parameters.Append(param);
    }

    if (!AnalyseFunction(env, instance, &copy)) [[unlikely]]
        return env.Null();

    InstanceMemory *mem = instance->memories[0];
    CallData call(env, instance, mem);

    if (!call.Prepare(&copy, info)) [[unlikely]]
        return env.Null();

    if (instance->debug) {
        call.DumpForward(&copy);
    }

    // Execute call
    {
        RG_DEFER_C(prev_call = exec_call) { exec_call = prev_call; };
        exec_call = &call;

        call.Execute(&copy, native);
    }

    return call.Complete(&copy);
}

Napi::Value TranslateVariadicCall(const Napi::CallbackInfo &info)
{
    FunctionInfo *func = (FunctionInfo *)info.Data();
    return TranslateVariadicCall(func, func->native, info);
}

class AsyncCall: public Napi::AsyncWorker {
    Napi::Env env;

    const FunctionInfo *func;
    void *native;

    CallData call;
    bool prepared = false;

public:
    AsyncCall(Napi::Env env, InstanceData *instance, const FunctionInfo *func,
              void *native, InstanceMemory *mem, Napi::Function &callback)
        : Napi::AsyncWorker(callback), env(env), func(func->Ref()), native(native),
          call(env, instance, mem) {}
    ~AsyncCall() { func->Unref(); }

    bool Prepare(const Napi::CallbackInfo &info) {
        prepared = call.Prepare(func, info);

        if (!prepared) {
            Napi::Error err = env.GetAndClearPendingException();
            SetError(err.Message());
        }

        return prepared;
    }
    void DumpForward() { call.DumpForward(func); }

    void Execute() override;
    void OnOK() override;
};

void AsyncCall::Execute()
{
    if (prepared) {
        RG_DEFER_C(prev_call = exec_call) { exec_call = prev_call; };
        exec_call = &call;

        call.Execute(func, native);
    }
}

void AsyncCall::OnOK()
{
    RG_ASSERT(prepared);

    Napi::FunctionReference &callback = Callback();

    Napi::Value self = env.Null();
    napi_value args[] = {
        env.Null(),
        call.Complete(func)
    };

    callback.Call(self, RG_LEN(args), args);
}

static Napi::Value TranslateAsyncCall(const FunctionInfo *func, void *native,
                                      const Napi::CallbackInfo &info)
{
    RG_ASSERT(!func->variadic);

    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() <= (uint32_t)func->required_parameters) {
        ThrowError<Napi::TypeError>(env, "Expected %1 arguments, got %2", func->parameters.len + 1, info.Length());
        return env.Null();
    }

    Napi::Function callback = info[(uint32_t)func->required_parameters].As<Napi::Function>();

    if (!callback.IsFunction()) {
        ThrowError<Napi::TypeError>(env, "Expected callback function as last argument, got %1", GetValueType(instance, callback));
        return env.Null();
    }

    InstanceMemory *mem = AllocateMemory(instance, instance->config.async_stack_size, instance->config.async_heap_size);
    if (!mem) [[unlikely]] {
        ThrowError<Napi::Error>(env, "Too many asynchronous calls are running");
        return env.Null();
    }
    AsyncCall *async = new AsyncCall(env, instance, func, native, mem, callback);

    if (async->Prepare(info) && instance->debug) {
        async->DumpForward();
    }
    async->Queue();

    return env.Undefined();
}

Napi::Value TranslateAsyncCall(const Napi::CallbackInfo &info)
{
    FunctionInfo *func = (FunctionInfo *)info.Data();
    return TranslateAsyncCall(func, func->native, info);
}

extern "C" void RelayCallback(Size idx, uint8_t *own_sp, uint8_t *caller_sp, BackRegisters *out_reg)
{
    if (exec_call) [[likely]] {
        exec_call->RelaySafe(idx, own_sp, caller_sp, false, out_reg);
    } else {
        // This happens if the callback pointer is called from a different thread
        // than the one that runs the FFI call (sync or async).

        TrampolineInfo *trampoline = &shared.trampolines[idx];

        Napi::Env env = trampoline->func.Env();
        InstanceData *instance = env.GetInstanceData<InstanceData>();

        InstanceMemory *mem = AllocateMemory(instance, instance->config.async_stack_size, instance->config.async_heap_size);
        if (!mem) [[unlikely]] {
            ThrowError<Napi::Error>(env, "Too many asynchronous calls are running");
            return;
        }

        // Avoid triggering the "use callback beyond FFI" check
        RG_DEFER_C(generation = trampoline->generation) { trampoline->generation = generation; };
        trampoline->generation = -1;

        // We set dispose_call to true so that the main thread will dispose of CallData itself
        CallData call(env, instance, mem);
        call.RelaySafe(idx, own_sp, caller_sp, true, out_reg);
    }
}

static Napi::Value FindLibraryFunction(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();
    LibraryHolder *lib = (LibraryHolder *)info.Data();

    FunctionInfo *func = new FunctionInfo();
    RG_DEFER { func->Unref(); };

    func->lib = lib->Ref();

    if (info.Length() >= 3) {
        if (!ParseClassicFunction(info, func))
            return env.Null();
    } else if (info.Length() >= 1) {
        if (!info[0].IsString()) {
            ThrowError<Napi::TypeError>(env, "Unexpected %1 value for prototype, expected string", GetValueType(instance, info[0]));
            return env.Null();
        }

        std::string proto = info[0u].As<Napi::String>();
        if (!ParsePrototype(env, proto.c_str(), func))
            return env.Null();
    } else {
        ThrowError<Napi::TypeError>(env, "Expected 1 or 3 arguments, got %1", info.Length());
        return env.Null();
    }

    if (func->convention != CallConvention::Cdecl && func->variadic) {
        LogError("Call convention '%1' does not support variadic functions, ignoring",
                 CallConventionNames[(int)func->convention]);
        func->convention = CallConvention::Cdecl;
    }

    if (!AnalyseFunction(env, instance, func))
        return env.Null();
    if (func->variadic) {
        // Minimize reallocations
        func->parameters.Grow(32);
    }

#ifdef _WIN32
    if (func->ordinal_name < 0) {
        if (func->decorated_name) {
            func->native = (void *)GetProcAddress((HMODULE)lib->module, func->decorated_name);
        }
        if (!func->native) {
            func->native = (void *)GetProcAddress((HMODULE)lib->module, func->name);
        }
    } else {
        uint16_t ordinal = (uint16_t)func->ordinal_name;

        func->decorated_name = nullptr;
        func->native = (void *)GetProcAddress((HMODULE)lib->module, (LPCSTR)(size_t)ordinal);
    }
#else
    if (func->decorated_name) {
        func->native = dlsym(lib->module, func->decorated_name);
    }
    if (!func->native) {
        func->native = dlsym(lib->module, func->name);
    }
#endif
    if (!func->native) {
        ThrowError<Napi::Error>(env, "Cannot find function '%1' in shared library", func->name);
        return env.Null();
    }

    Napi::Function wrapper = WrapFunction(env, func);
    return wrapper;
}

static Napi::Value FindSymbol(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();
    LibraryHolder *lib = (LibraryHolder *)info.Data();

    if (info.Length() < 2) {
        ThrowError<Napi::TypeError>(env, "Expected 2, got %1", info.Length());
        return env.Null();
    }
     if (!info[0].IsString()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for name, expected string", GetValueType(instance, info[0]));
        return env.Null();
    }

    std::string name = info[0].As<Napi::String>();

    const TypeInfo *type = ResolveType(info[1]);
    if (!type)
        return env.Null();

#ifdef _WIN32
    void *ptr = (void *)GetProcAddress((HMODULE)lib->module, name.c_str());
#else
    void *ptr = (void *)dlsym(lib->module, name.c_str());
#endif
    if (!ptr) {
        ThrowError<Napi::Error>(env, "Cannot find symbol '%1' in shared library", name.c_str());
        return env.Null();
    }

    Napi::External<void> external = Napi::External<void>::New(env, ptr);
    SetValueTag(instance, external, &type);

    return external;
}

static Napi::Value UnloadLibrary(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    LibraryHolder *lib = (LibraryHolder *)info.Data();

    lib->Unload();

    return env.Undefined();
}

static Napi::Value LoadSharedLibrary(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 or 2 arguments, got %1", info.Length());
        return env.Null();
    }
    if (!info[0].IsString() && !IsNullOrUndefined(info[0])) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for filename, expected string or null", GetValueType(instance, info[0]));
        return env.Null();
    }
    if (info.Length() >= 2 && !IsObject(info[1])) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for options, expected object", GetValueType(instance, info[1]));
        return env.Null();
    }

#ifndef _WIN32
    bool lazy = false;
    if (info.Length() >= 2) {
        Napi::Object options = info[1].As<Napi::Object>();
        lazy = options.Get("lazy").ToBoolean();
    }
#endif

    if (!instance->memories.len) {
        AllocateMemory(instance, instance->config.sync_stack_size, instance->config.sync_heap_size);
        RG_ASSERT(instance->memories.len);
    }

    // Load shared library
    void *module = nullptr;
#ifdef _WIN32
    if (info[0].IsString()) {
        std::string filename = info[0].As<Napi::String>();
        module = LoadWindowsLibrary(env, filename.c_str());

        if (!module)
            return env.Null();
    } else {
        module = GetModuleHandle(nullptr);
        RG_ASSERT(module);
    }
#else
    if (info[0].IsString()) {
        int flags = lazy ? RTLD_LAZY : RTLD_NOW;

        std::string filename = info[0].As<Napi::String>();
        module = dlopen(filename.c_str(), flags);

        if (!module) {
            const char *msg = dlerror();

            if (StartsWith(msg, filename.c_str())) {
                msg += filename.length();
            }
            while (strchr(": ", msg[0])) {
                msg++;
            }

            ThrowError<Napi::Error>(env, "Failed to load shared library: %1", msg);
            return env.Null();
        }
    } else {
        module = RTLD_DEFAULT;
    }
#endif

    LibraryHolder *lib = new LibraryHolder(module);
    RG_DEFER { lib->Unref(); };

    Napi::Object obj = Napi::Object::New(env);

#define ADD_METHOD(Name, Call) \
        do { \
            const auto wrapper = [](const Napi::CallbackInfo &info) { return Call; }; \
            Napi::Function func = Napi::Function::New(env, wrapper, (Name), (void *)lib->Ref()); \
            func.AddFinalizer([](Napi::Env, LibraryHolder *lib) { lib->Unref(); }, lib); \
            obj.Set((Name), func); \
        } while (false)

    ADD_METHOD("func", FindLibraryFunction(info));
    ADD_METHOD("symbol", FindSymbol(info));

    // We can't unref the library after unload, obviously
    obj.Set("unload", Napi::Function::New(env, UnloadLibrary, "unload", (void *)lib->Ref()));

#undef ADD_METHOD

    return obj;
}

static Napi::Value RegisterCallback(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (!InitAsyncBroker(env, instance)) [[unlikely]]
        return env.Null();

    bool has_recv = (info.Length() >= 3 && info[1].IsFunction());

    if (info.Length() < 2u + has_recv) {
        ThrowError<Napi::TypeError>(env, "Expected 2 or 3 arguments, got %1", info.Length());
        return env.Null();
    }
    if (!info[0u + has_recv].IsFunction()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for func, expected function", GetValueType(instance, info[0 + has_recv]));
        return env.Null();
    }

    Napi::Value recv = has_recv ? info[0] : env.Undefined();
    Napi::Function func = info[0u + has_recv].As<Napi::Function>();

    const TypeInfo *type = ResolveType(info[1u + has_recv]);
    if (!type)
        return env.Null();
    if (type->primitive != PrimitiveKind::Callback) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 type, expected <callback> * type", type->name);
        return env.Null();
    }

    int16_t idx;
    {
        std::lock_guard<std::mutex> lock(shared.mutex);

        if (!shared.available.len) [[unlikely]] {
            ThrowError<Napi::Error>(env, "Too many callbacks are in use (max = %1)", MaxTrampolines);
            return env.Null();
        }

        idx = shared.available.data[--shared.available.len];
    }

    TrampolineInfo *trampoline = &shared.trampolines[idx];

    trampoline->instance = instance;
    trampoline->proto = type->ref.proto;
    trampoline->func.Reset(func, 1);
    if (!IsNullOrUndefined(recv)) {
        trampoline->recv.Reset(recv, 1);
    } else {
        trampoline->recv.Reset();
    }
    trampoline->generation = -1;

    void *ptr = GetTrampoline(idx, type->ref.proto);

    Napi::External<void> external = Napi::External<void>::New(env, ptr);
    SetValueTag(instance, external, type->ref.marker);

    // Cache index for fast unregistration
    instance->trampolines_map.Set(ptr, idx);

    return external;
}

static Napi::Value UnregisterCallback(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }
    if (!info[0].IsExternal()) {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for id, expected registered callback", GetValueType(instance, info[0]));
        return env.Null();
    }

    Napi::External<void> external = info[0].As<Napi::External<void>>();
    void *ptr = external.Data();

    int16_t idx;
    {
        int16_t *it = instance->trampolines_map.Find(ptr);

        if (!it) [[unlikely]] {
            ThrowError<Napi::Error>(env, "Could not find matching registered callback");
            return env.Null();
        }

        idx = *it;
        instance->trampolines_map.Remove(it);
    }

    // Release shared trampoline safely
    {
        std::lock_guard<std::mutex> lock(shared.mutex);

        TrampolineInfo *trampoline = &shared.trampolines[idx];
        RG_ASSERT(!trampoline->func.IsEmpty());

        trampoline->func.Reset();
        trampoline->recv.Reset();

        shared.available.Append(idx);
    }

    return env.Undefined();
}

static Napi::Value CastValue(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 2) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Expected 2 arguments, got %1", info.Length());
        return env.Null();
    }

    Napi::Value value = info[0];

    const TypeInfo *type = ResolveType(info[1]);
    if (!type) [[unlikely]]
        return env.Null();
    if (type->primitive != PrimitiveKind::Pointer &&
            type->primitive != PrimitiveKind::Callback &&
            type->primitive != PrimitiveKind::String &&
            type->primitive != PrimitiveKind::String16) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Only pointer or string types can be used for casting");
        return env.Null();
    }

    ValueCast *cast = new ValueCast;

    cast->ref.Reset(value, 1);
    cast->type = type;

    Napi::External<ValueCast> external = Napi::External<ValueCast>::New(env, cast, [](Napi::Env, ValueCast *cast) { delete cast; });
    SetValueTag(instance, external, &CastMarker);

    return external;
}

static Napi::Value DecodeValue(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();

    bool has_offset = (info.Length() >= 2 && info[1].IsNumber());
    bool has_len = (info.Length() >= 3u + has_offset && info[2u + has_offset].IsNumber());

    if (info.Length() < 2u + has_offset) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Expected %1 to 4 arguments, got %2", 2 + has_offset, info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[1u + has_offset]);
    if (!type) [[unlikely]]
        return env.Null();

    Napi::Value value = info[0];
    int64_t offset = has_offset ? info[1].As<Napi::Number>().Int64Value() : 0;

    if (has_len) {
        Size len = info[2u + has_offset].As<Napi::Number>();

        Napi::Value ret = Decode(value, offset, type, &len);
        return ret;
    } else {
        Napi::Value ret = Decode(value, offset, type);
        return ret;
    }
}

static Napi::Value GetPointerAddress(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();

    if (info.Length() < 1) {
        ThrowError<Napi::TypeError>(env, "Expected 1 argument, got %1", info.Length());
        return env.Null();
    }

    void *ptr = nullptr;
    if (!GetExternalPointer(env, info[0], &ptr))
        return env.Null();

    uint64_t ptr64 = (uint64_t)(uintptr_t)ptr;
    Napi::BigInt bigint = Napi::BigInt::New(env, ptr64);

    return bigint;
}

static Napi::Value CallPointerSync(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (info.Length() < 2) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Expected 2 or more arguments, got %1", info.Length());
        return env.Null();
    }

    void *ptr = nullptr;
    if (!GetExternalPointer(env, info[0], &ptr)) [[unlikely]]
        return env.Null();

    const TypeInfo *type = ResolveType(info[1]);
    if (!type) [[unlikely]]
        return env.Null();
    if (type->primitive != PrimitiveKind::Prototype) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Unexpected %1 value for type, expected function type", GetValueType(instance, info[1]));
        return env.Null();
    }

    const FunctionInfo *proto = type->ref.proto;

    return proto->variadic ? TranslateVariadicCall(proto, ptr, info)
                           : TranslateNormalCall(proto, ptr, info);
}

static Napi::Value EncodeValue(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();

    bool has_offset = (info.Length() >= 2 && info[1].IsNumber());
    bool has_len = (info.Length() >= 4u + has_offset && info[3u + has_offset].IsNumber());

    if (info.Length() < 3u + has_offset) [[unlikely]] {
        ThrowError<Napi::TypeError>(env, "Expected %1 to 5 arguments, got %2", 3 + has_offset, info.Length());
        return env.Null();
    }

    const TypeInfo *type = ResolveType(info[1u + has_offset]);
    if (!type) [[unlikely]]
        return env.Null();

    Napi::Value ref = info[0];
    int64_t offset = has_offset ? info[1].As<Napi::Number>().Int64Value() : 0;
    Napi::Value value = info[2u + has_offset];

    if (has_len) {
        Size len = info[3u + has_offset].As<Napi::Number>();

        if (!Encode(ref, offset, value, type, &len))
            return env.Null();
    } else {
        if (!Encode(ref, offset, value, type))
            return env.Null();
    }

    return env.Undefined();
}

static Napi::Value ResetKoffi(const Napi::CallbackInfo &info)
{
    Napi::Env env = info.Env();
    InstanceData *instance = env.GetInstanceData<InstanceData>();

    if (instance->broker) {
        napi_release_threadsafe_function(instance->broker, napi_tsfn_abort);
        instance->broker = nullptr;
    }

    instance->types.RemoveFrom(instance->base_types_len);

    {
        HashSet<const void *> base_types;
        HashMap<const char *, const TypeInfo *> new_map;

        for (const TypeInfo &type: instance->types) {
            base_types.Set(&type);
        }

        for (const auto &bucket: instance->types_map.table) {
            if (base_types.Find(bucket.value)) {
                new_map.Set(bucket.key, bucket.value);
            }
        }

        std::swap(instance->types_map, new_map);
    }

    instance->callbacks.Clear();

    return env.Undefined();
}

void LibraryHolder::Unload()
{
#ifdef _WIN32
    if (module && module != GetModuleHandle(nullptr)) {
        FreeLibrary((HMODULE)module);
    }
#else
    if (module && module != RTLD_DEFAULT) {
        dlclose(module);
    }
#endif

    module = nullptr;
}

const LibraryHolder *LibraryHolder::Ref() const
{
    refcount++;
    return this;
}

void LibraryHolder::Unref() const
{
    if (!--refcount) {
        delete this;
    }
}

FunctionInfo::~FunctionInfo()
{
    if (lib) {
        lib->Unref();
    }
}

const FunctionInfo *FunctionInfo::Ref() const
{
    refcount++;
    return this;
}

void FunctionInfo::Unref() const
{
    if (!--refcount) {
        delete this;
    }
}

InstanceMemory::~InstanceMemory()
{
#ifdef _WIN32
    if (stack.ptr) {
        VirtualFree(stack.ptr, 0, MEM_RELEASE);
    }
    if (heap.ptr) {
        VirtualFree(heap.ptr, 0, MEM_RELEASE);
    }
#else
    if (stack.ptr) {
        munmap(stack.ptr, stack.len);
    }
    if (heap.ptr) {
        munmap(heap.ptr, heap.len);
    }
#endif
}

bool InitAsyncBroker(Napi::Env env, InstanceData *instance)
{
    if (!instance->broker) {
        if (napi_create_threadsafe_function(env, nullptr, nullptr,
                                            Napi::String::New(env, "Koffi Async Callback Broker"),
                                            0, 1, nullptr, nullptr, nullptr,
                                            CallData::RelayAsync, &instance->broker) != napi_ok) {
            LogError("Failed to create async callback broker");
            return false;
        }
        napi_unref_threadsafe_function(env, instance->broker);
    }

    return true;
}

static void RegisterPrimitiveType(Napi::Env env, Napi::Object map, std::initializer_list<const char *> names,
                                  PrimitiveKind primitive, int32_t size, int16_t align, const char *ref = nullptr)
{
    RG_ASSERT(names.size() > 0);
    RG_ASSERT(align <= size);

    InstanceData *instance = env.GetInstanceData<InstanceData>();

    TypeInfo *type = instance->types.AppendDefault();

    type->name = *names.begin();

    type->primitive = primitive;
    type->size = size;
    type->align = align;

    if (IsInteger(type) || IsFloat(type)) {
        type->flags |= (int)TypeFlag::HasTypedArray;
    }
    if (TestStr(type->name, "char") || TestStr(type->name, "char16") || TestStr(type->name, "char16_t")) {
        type->flags |= (int)TypeFlag::IsCharLike;
    }

    if (ref) {
        const TypeInfo *marker = instance->types_map.FindValue(ref, nullptr);
        RG_ASSERT(marker);

        type->ref.marker = marker;
    }

    Napi::Value wrapper = WrapType(env, instance, type);

    for (const char *name: names) {
        bool inserted;
        instance->types_map.TrySet(name, type, &inserted);
        RG_ASSERT(inserted);

        if (!EndsWith(name, "*")) {
            map.Set(name, wrapper);
        }
    }
}

static inline PrimitiveKind GetSignPrimitive(Size len, bool sign)
{
    switch (len) {
        case 1: return sign ? PrimitiveKind::Int8 : PrimitiveKind::UInt8;
        case 2: return sign ? PrimitiveKind::Int16 : PrimitiveKind::UInt16;
        case 4: return sign ? PrimitiveKind::Int32 : PrimitiveKind::UInt32;
        case 8: return sign ? PrimitiveKind::Int64 : PrimitiveKind::UInt64;
    }

    RG_UNREACHABLE();
}

static inline PrimitiveKind GetLittleEndianPrimitive(PrimitiveKind kind)
{
#ifdef RG_BIG_ENDIAN
    return (PrimitiveKind)((int)kind + 1);
#else
    return kind;
#endif
}

static inline PrimitiveKind GetBigEndianPrimitive(PrimitiveKind kind)
{
#ifdef RG_BIG_ENDIAN
    return kind;
#else
    return (PrimitiveKind)((int)kind + 1);
#endif
}

static InstanceData *CreateInstance()
{
    InstanceData *instance = new InstanceData();
    RG_DEFER_N(err_guard) { delete instance; };

    instance->main_thread_id = std::this_thread::get_id();

    instance->debug = GetDebugFlag("DUMP_CALLS");
    FillRandomSafe(&instance->tag_lower, RG_SIZE(instance->tag_lower));

#ifdef _WIN32
    TEB *teb = GetTEB();

    instance->main_stack_max = teb->StackBase;
    instance->main_stack_min = teb->DeallocationStack;
#endif

    err_guard.Disable();
    return instance;
}

static Napi::Object InitModule(Napi::Env env, Napi::Object exports)
{
    InstanceData *instance = CreateInstance();
    RG_CRITICAL(instance, "Failed to initialize Koffi");

    env.SetInstanceData(instance);

    exports.Set("config", Napi::Function::New(env, GetSetConfig, "config"));
    exports.Set("stats", Napi::Function::New(env, GetStats, "stats"));

    exports.Set("struct", Napi::Function::New(env, CreatePaddedStructType, "struct"));
    exports.Set("pack", Napi::Function::New(env, CreatePackedStructType, "pack"));
    exports.Set("union", Napi::Function::New(env, CreateUnionType, "union"));
    exports.Set("Union", Napi::Function::New(env, InstantiateUnion, "Union"));
    exports.Set("opaque", Napi::Function::New(env, CreateOpaqueType, "opaque"));
    exports.Set("pointer", Napi::Function::New(env, CreatePointerType, "pointer"));
    exports.Set("array", Napi::Function::New(env, CreateArrayType, "array"));
    exports.Set("proto", Napi::Function::New(env, CreateFunctionType, "proto"));
    exports.Set("alias", Napi::Function::New(env, CreateTypeAlias, "alias"));

    exports.Set("sizeof", Napi::Function::New(env, GetTypeSize, "sizeof"));
    exports.Set("alignof", Napi::Function::New(env, GetTypeAlign, "alignof"));
    exports.Set("offsetof", Napi::Function::New(env, GetMemberOffset, "offsetof"));
    exports.Set("resolve", Napi::Function::New(env, GetResolvedType, "resolve"));
    exports.Set("introspect", Napi::Function::New(env, GetTypeDefinition, "introspect"));

    exports.Set("load", Napi::Function::New(env, LoadSharedLibrary, "load"));

    exports.Set("in", Napi::Function::New(env, MarkIn, "in"));
    exports.Set("out", Napi::Function::New(env, MarkOut, "out"));
    exports.Set("inout", Napi::Function::New(env, MarkInOut, "inout"));

    exports.Set("disposable", Napi::Function::New(env, CreateDisposableType, "disposable"));
    exports.Set("alloc", Napi::Function::New(env, CallAlloc, "alloc"));
    exports.Set("free", Napi::Function::New(env, CallFree, "free"));

    exports.Set("register", Napi::Function::New(env, RegisterCallback, "register"));
    exports.Set("unregister", Napi::Function::New(env, UnregisterCallback, "unregister"));

    exports.Set("as", Napi::Function::New(env, CastValue, "as"));
    exports.Set("decode", Napi::Function::New(env, DecodeValue, "decode"));
    exports.Set("address", Napi::Function::New(env, GetPointerAddress, "address"));
    exports.Set("call", Napi::Function::New(env, CallPointerSync, "call"));
    exports.Set("encode", Napi::Function::New(env, EncodeValue, "encode"));

    exports.Set("reset", Napi::Function::New(env, ResetKoffi, "reset"));

    exports.Set("errno", Napi::Function::New(env, GetOrSetErrNo, "errno"));

    // Export useful OS info
    {
        Napi::Object os = Napi::Object::New(env);
        exports.Set("os", os);

        Napi::Object codes = Napi::Object::New(env);

        for (const ErrnoCodeInfo &info: ErrnoCodes) {
            codes.Set(info.name, Napi::Number::New(env, info.value));
        }

        os.Set("errno", codes);
    }

#if defined(_WIN32)
    exports.Set("extension", Napi::String::New(env, ".dll"));
#elif defined(__APPLE__)
    exports.Set("extension", Napi::String::New(env, ".dylib"));
#else
    exports.Set("extension", Napi::String::New(env, ".so"));
#endif

    // Init base types
    {
        Napi::Object types = Napi::Object::New(env);
        exports.Set("types", types);

        RegisterPrimitiveType(env, types, {"void"}, PrimitiveKind::Void, 0, 0);
        RegisterPrimitiveType(env, types, {"bool"}, PrimitiveKind::Bool, RG_SIZE(bool), alignof(bool));
        RegisterPrimitiveType(env, types, {"int8_t", "int8"}, PrimitiveKind::Int8, 1, 1);
        RegisterPrimitiveType(env, types, {"uint8_t", "uint8"}, PrimitiveKind::UInt8, 1, 1);
        RegisterPrimitiveType(env, types, {"char"}, PrimitiveKind::Int8, 1, 1);
        RegisterPrimitiveType(env, types, {"unsigned char", "uchar"}, PrimitiveKind::UInt8, 1, 1);
        RegisterPrimitiveType(env, types, {"char16_t", "char16"}, PrimitiveKind::Int16, 2, 2);
        RegisterPrimitiveType(env, types, {"int16_t", "int16"}, PrimitiveKind::Int16, 2, 2);
        RegisterPrimitiveType(env, types, {"int16_le_t", "int16_le"}, GetLittleEndianPrimitive(PrimitiveKind::Int16), 2, 2);
        RegisterPrimitiveType(env, types, {"int16_be_t", "int16_be"}, GetBigEndianPrimitive(PrimitiveKind::Int16), 2, 2);
        RegisterPrimitiveType(env, types, {"uint16_t", "uint16"}, PrimitiveKind::UInt16, 2, 2);
        RegisterPrimitiveType(env, types, {"uint16_le_t", "uint16_le"}, GetLittleEndianPrimitive(PrimitiveKind::UInt16), 2, 2);
        RegisterPrimitiveType(env, types, {"uint16_be_t", "uint16_be"}, GetBigEndianPrimitive(PrimitiveKind::UInt16), 2, 2);
        RegisterPrimitiveType(env, types, {"short"}, PrimitiveKind::Int16, 2, 2);
        RegisterPrimitiveType(env, types, {"unsigned short", "ushort"}, PrimitiveKind::UInt16, 2, 2);
        RegisterPrimitiveType(env, types, {"int32_t", "int32"}, PrimitiveKind::Int32, 4, 4);
        RegisterPrimitiveType(env, types, {"int32_le_t", "int32_le"}, GetLittleEndianPrimitive(PrimitiveKind::Int32), 4, 4);
        RegisterPrimitiveType(env, types, {"int32_be_t", "int32_be"}, GetBigEndianPrimitive(PrimitiveKind::Int32), 4, 4);
        RegisterPrimitiveType(env, types, {"uint32_t", "uint32"}, PrimitiveKind::UInt32, 4, 4);
        RegisterPrimitiveType(env, types, {"uint32_le_t", "uint32_le"}, GetLittleEndianPrimitive(PrimitiveKind::UInt32), 4, 4);
        RegisterPrimitiveType(env, types, {"uint32_be_t", "uint32_be"}, GetBigEndianPrimitive(PrimitiveKind::UInt32), 4, 4);
        RegisterPrimitiveType(env, types, {"int"}, PrimitiveKind::Int32, 4, 4);
        RegisterPrimitiveType(env, types, {"unsigned int", "uint"}, PrimitiveKind::UInt32, 4, 4);
        RegisterPrimitiveType(env, types, {"int64_t", "int64"}, PrimitiveKind::Int64, 8, alignof(int64_t));
        RegisterPrimitiveType(env, types, {"int64_le_t", "int64_le"}, GetLittleEndianPrimitive(PrimitiveKind::Int64), 8, alignof(int64_t));
        RegisterPrimitiveType(env, types, {"int64_be_t", "int64_be"}, GetBigEndianPrimitive(PrimitiveKind::Int64), 8, alignof(int64_t));
        RegisterPrimitiveType(env, types, {"uint64_t", "uint64"}, PrimitiveKind::UInt64, 8, alignof(int64_t));
        RegisterPrimitiveType(env, types, {"uint64_le_t", "uint64_le"}, GetLittleEndianPrimitive(PrimitiveKind::UInt64), 8, alignof(int64_t));
        RegisterPrimitiveType(env, types, {"uint64_be_t", "uint64_be"}, GetBigEndianPrimitive(PrimitiveKind::UInt64), 8, alignof(int64_t));
        RegisterPrimitiveType(env, types, {"intptr_t", "intptr"}, GetSignPrimitive(RG_SIZE(intptr_t), true), RG_SIZE(intptr_t), alignof(intptr_t));
        RegisterPrimitiveType(env, types, {"uintptr_t", "uintptr"}, GetSignPrimitive(RG_SIZE(intptr_t), false), RG_SIZE(intptr_t), alignof(intptr_t));
        RegisterPrimitiveType(env, types, {"size_t"}, GetSignPrimitive(RG_SIZE(size_t), false), RG_SIZE(size_t), alignof(size_t));
        RegisterPrimitiveType(env, types, {"long"}, GetSignPrimitive(RG_SIZE(long), true), RG_SIZE(long), alignof(long));
        RegisterPrimitiveType(env, types, {"unsigned long", "ulong"}, GetSignPrimitive(RG_SIZE(long), false), RG_SIZE(long), alignof(long));
        RegisterPrimitiveType(env, types, {"long long", "longlong"}, PrimitiveKind::Int64, RG_SIZE(int64_t), alignof(int64_t));
        RegisterPrimitiveType(env, types, {"unsigned long long", "ulonglong"}, PrimitiveKind::UInt64, RG_SIZE(uint64_t), alignof(uint64_t));
        RegisterPrimitiveType(env, types, {"float", "float32"}, PrimitiveKind::Float32, 4, alignof(float));
        RegisterPrimitiveType(env, types, {"double", "float64"}, PrimitiveKind::Float64, 8, alignof(double));
        RegisterPrimitiveType(env, types, {"char *", "str", "string"}, PrimitiveKind::String, RG_SIZE(void *), alignof(void *), "char");
        RegisterPrimitiveType(env, types, {"char16_t *", "char16 *", "str16", "string16"}, PrimitiveKind::String16, RG_SIZE(void *), alignof(void *), "char16_t");

        instance->void_type = instance->types_map.FindValue("void", nullptr);
        instance->char_type = instance->types_map.FindValue("char", nullptr);
        instance->char16_type = instance->types_map.FindValue("char16", nullptr);
        instance->str_type = instance->types_map.FindValue("char *", nullptr);
        instance->str16_type = instance->types_map.FindValue("char16_t *", nullptr);

        instance->active_symbol = Napi::Symbol::New(env, "active");

        instance->base_types_len = instance->types.len;
    }

    // Expose internal Node stuff
    {
        Napi::Object node = Napi::Object::New(env);
        exports.Set("node", node);

        Napi::External<void> external = Napi::External<void>::New(env, (napi_env)env);
        SetValueTag(instance, external, instance->void_type);

        node.Set("env", external);
    }

    exports.Set("version", Napi::String::New(env, RG_STRINGIFY(VERSION)));

    return exports;
}

InstanceData::~InstanceData()
{
    for (InstanceMemory *mem: memories) {
        delete mem;
    }

    // Clean-up leftover trampoline references
    {
        std::lock_guard<std::mutex> lock(shared.mutex);

        for (int16_t idx = 0; idx < MaxTrampolines; idx++) {
            TrampolineInfo *trampoline = &shared.trampolines[idx];

            if (trampoline->instance == this) {
                trampoline->instance = nullptr;
                trampoline->func.Reset();
                trampoline->recv.Reset();
            }
        }
    }

    if (broker) {
        napi_release_threadsafe_function(broker, napi_tsfn_abort);
    }
}

NODE_API_MODULE(koffi, InitModule);

}
