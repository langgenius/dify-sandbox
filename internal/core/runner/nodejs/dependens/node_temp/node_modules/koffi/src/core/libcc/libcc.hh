// Copyright 2023 Niels Martignène <niels.martignene@protonmail.com>

// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the “Software”), to deal in 
// the Software without restriction, including without limitation the rights to use,
// copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the
// Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
// OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.

#pragma once

#include <algorithm>
#include <atomic>
#include <cmath>
#include <condition_variable>
#include <errno.h>
#include <float.h>
#include <functional>
#include <inttypes.h>
#include <limits.h>
#include <limits>
#include <memory>
#include <mutex>
#include <shared_mutex>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <thread>
#include <type_traits>
#include <utility>
#if defined(_WIN32)
    #include <intrin.h>
#elif !defined(__APPLE__) && (!defined(__linux__) || defined(__GLIBC__)) && __has_include(<ucontext.h>)
    #define RG_FIBER_USE_UCONTEXT
    #include <ucontext.h>
#endif
#ifdef __EMSCRIPTEN__
    #include <emscripten.h>
#endif
#ifdef _MSC_VER
    #define ENABLE_INTSAFE_SIGNED_FUNCTIONS
    #include <intsafe.h>
    #pragma intrinsic(_BitScanReverse)
    #ifdef _WIN64
        #pragma intrinsic(_BitScanReverse64)
    #endif
    #if defined(_M_IX86) || defined(_M_X64)
        #pragma intrinsic(__rdtsc)
    #endif
#endif

struct sigaction;
struct BrotliEncoderStateStruct;

namespace RG {

// ------------------------------------------------------------------------
// Config
// ------------------------------------------------------------------------

#ifndef NDEBUG
    #define RG_DEBUG
#endif

#define RG_DEFAULT_ALLOCATOR MallocAllocator
#define RG_BLOCK_ALLOCATOR_DEFAULT_SIZE Kibibytes(4)

#define RG_HEAPARRAY_BASE_CAPACITY 8
#define RG_HEAPARRAY_GROWTH_FACTOR 2.0

// Must be a power-of-two
#define RG_HASHTABLE_BASE_CAPACITY 8
#define RG_HASHTABLE_MAX_LOAD_FACTOR 0.5

#define RG_FMT_STRING_BASE_CAPACITY 256
#define RG_FMT_STRING_PRINT_BUFFER_SIZE 1024

#define RG_LINE_READER_STEP_SIZE 65536

#define RG_ASYNC_MAX_THREADS 2048
#define RG_ASYNC_MAX_IDLE_TIME 10000
#define RG_ASYNC_MAX_PENDING_TASKS 1024

#define RG_FIBER_DEFAULT_STACK_SIZE Kibibytes(128)

// ------------------------------------------------------------------------
// Utility
// ------------------------------------------------------------------------

extern "C" const char *FelixTarget;
extern "C" const char *FelixVersion;
extern "C" const char *FelixCompiler;

#if defined(__x86_64__) || defined(_M_X64) || defined(__aarch64__) || defined(_M_ARM64) || __riscv_xlen == 64
    typedef int64_t Size;
    #define RG_SIZE_MAX INT64_MAX
#elif defined(_WIN32) || defined(__APPLE__) || defined(__unix__)
    typedef int32_t Size;
    #define RG_SIZE_MAX INT32_MAX
#elif defined(__thumb__) || defined(__arm__) || defined(__EMSCRIPTEN__)
    typedef int32_t Size;
    #define RG_SIZE_MAX INT32_MAX
#else
    #error Machine architecture not supported
#endif

#if defined(_MSC_VER) || __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
    // Sane platform
#elif __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__
    #define RG_BIG_ENDIAN
#else
    #error This code base is not designed to support platforms with crazy endianness
#endif

#if UINT_MAX != 0xFFFFFFFFu
    #error This code base is not designed to support non-32-bits int types
#endif
#if ULLONG_MAX != 0xFFFFFFFFFFFFFFFFull
    #error This code base is not designed to support non-64-bits long long types
#endif
static_assert(sizeof(double) == 8, "This code base is not designed to support single-precision double floats");

#define RG_STRINGIFY_(a) #a
#define RG_STRINGIFY(a) RG_STRINGIFY_(a)
#define RG_CONCAT_(a, b) a ## b
#define RG_CONCAT(a, b) RG_CONCAT_(a, b)
#define RG_UNIQUE_NAME(prefix) RG_CONCAT(prefix, __LINE__)
#define RG_FORCE_EXPAND(x) x
#define RG_IGNORE (void)!

#if defined(__GNUC__)
    #define RG_PUSH_NO_WARNINGS \
        _Pragma("GCC diagnostic push") \
        _Pragma("GCC diagnostic ignored \"-Wall\"") \
        _Pragma("GCC diagnostic ignored \"-Wextra\"") \
        _Pragma("GCC diagnostic ignored \"-Wconversion\"") \
        _Pragma("GCC diagnostic ignored \"-Wsign-conversion\"") \
        _Pragma("GCC diagnostic ignored \"-Wunused-function\"") \
        _Pragma("GCC diagnostic ignored \"-Wunused-variable\"") \
        _Pragma("GCC diagnostic ignored \"-Wunused-but-set-variable\"") \
        _Pragma("GCC diagnostic ignored \"-Wunused-parameter\"")
    #define RG_POP_NO_WARNINGS \
        _Pragma("GCC diagnostic pop")

    // thread_local has many bugs with MinGW (Windows):
    // - Destructors are run after the memory is freed
    // - It crashes when used in R packages
    #ifdef __EMSCRIPTEN__
        #define RG_THREAD_LOCAL
    #else
        #define RG_THREAD_LOCAL __thread
    #endif

    #ifndef SCNd8
        #define SCNd8 "hhd"
    #endif
    #ifndef SCNi8
        #define SCNi8 "hhd"
    #endif
    #ifndef SCNu8
        #define SCNu8 "hhu"
    #endif
#elif defined(_MSC_VER)
    #define RG_PUSH_NO_WARNINGS __pragma(warning(push, 0))
    #define RG_POP_NO_WARNINGS __pragma(warning(pop))

    #define RG_THREAD_LOCAL thread_local
#else
    #error Compiler not supported
#endif

#ifdef __clang__
    #if __has_feature(address_sanitizer)
        #define __SANITIZE_ADDRESS__
    #endif
    #if __has_feature(thread_sanitizer)
        #define __SANITIZE_THREAD__
    #endif
    #if __has_feature(undefined_sanitizer)
        #define __SANITIZE_UNDEFINED__
    #endif
#endif

extern "C" void AssertMessage(const char *filename, int line, const char *cond);

#if defined(_MSC_VER)
    #define RG_DEBUG_BREAK() __debugbreak()
#elif defined(__clang__)
    #define RG_DEBUG_BREAK() __builtin_debugtrap()
#elif defined(__i386__) || defined(__x86_64__)
    #define RG_DEBUG_BREAK() __asm__ __volatile__("int $0x03")
#elif defined(__thumb__)
    #define RG_DEBUG_BREAK() __asm__ __volatile__(".inst 0xde01")
#elif defined(__aarch64__)
    #define RG_DEBUG_BREAK() __asm__ __volatile__(".inst 0xd4200000")
#elif defined(__arm__)
    #define RG_DEBUG_BREAK() __asm__ __volatile__(".inst 0xe7f001f0")
#elif defined(__riscv)
    #define RG_DEBUG_BREAK() __asm__ __volatile__("ebreak")
#endif

#define RG_CRITICAL(Cond, ...) \
    do { \
        if (!(Cond)) [[unlikely]] { \
            PrintLn(stderr, __VA_ARGS__); \
            abort(); \
        } \
    } while (false)
#ifdef RG_DEBUG
    #define RG_ASSERT(Cond) \
        do { \
            if (!(Cond)) [[unlikely]] { \
                RG::AssertMessage(__FILE__, __LINE__, RG_STRINGIFY(Cond)); \
                RG_DEBUG_BREAK(); \
                abort(); \
            } \
        } while (false)
#else
    #define RG_ASSERT(Cond) \
        do { \
            (void)sizeof(Cond); \
        } while (false)
#endif

#if defined(RG_DEBUG)
    #define RG_UNREACHABLE() \
        do { \
            RG::AssertMessage(__FILE__, __LINE__, "Reached code marked as UNREACHABLE"); \
            RG_DEBUG_BREAK(); \
            abort(); \
        } while (false)
#elif defined(__GNUC__) || defined(__clang__)
    #define RG_UNREACHABLE() __builtin_unreachable()
#else
    #define RG_UNREACHABLE() __assume(0)
#endif

#define RG_DELETE_COPY(Cls) \
    Cls(const Cls&) = delete; \
    Cls &operator=(const Cls&) = delete;

constexpr uint16_t MakeUInt16(uint8_t high, uint8_t low)
    { return (uint16_t)(((uint16_t)high << 8) | low); }
constexpr uint32_t MakeUInt32(uint16_t high, uint16_t low) { return ((uint32_t)high << 16) | low; }
constexpr uint64_t MakeUInt64(uint32_t high, uint32_t low) { return ((uint64_t)high << 32) | low; }

constexpr Size Mebibytes(Size len) { return len * 1024 * 1024; }
constexpr Size Kibibytes(Size len) { return len * 1024; }
constexpr Size Megabytes(Size len) { return len * 1000 * 1000; }
constexpr Size Kilobytes(Size len) { return len * 1000; }

#define RG_SIZE(Type) ((RG::Size)sizeof(Type))
template <typename T, unsigned N>
char (&ComputeArraySize(T const (&)[N]))[N];
#define RG_BITS(Type) (8 * RG_SIZE(Type))
#define RG_LEN(Array) RG_SIZE(RG::ComputeArraySize(Array))
#define RG_OFFSET_OF(Type, Member) ((Size)__builtin_offsetof(Type, Member))

static inline constexpr uint16_t ReverseBytes(uint16_t u)
{
    return (uint16_t)(((u & 0x00FF) << 8) |
                      ((u & 0xFF00) >> 8));
}

static inline constexpr uint32_t ReverseBytes(uint32_t u)
{
    return ((u & 0x000000FF) << 24) |
           ((u & 0x0000FF00) << 8)  |
           ((u & 0x00FF0000) >> 8)  |
           ((u & 0xFF000000) >> 24);
}

static inline constexpr uint64_t ReverseBytes(uint64_t u)
{
    return ((u & 0x00000000000000FF) << 56) |
           ((u & 0x000000000000FF00) << 40) |
           ((u & 0x0000000000FF0000) << 24) |
           ((u & 0x00000000FF000000) << 8)  |
           ((u & 0x000000FF00000000) >> 8)  |
           ((u & 0x0000FF0000000000) >> 24) |
           ((u & 0x00FF000000000000) >> 40) |
           ((u & 0xFF00000000000000) >> 56);
}

static inline constexpr int16_t ReverseBytes(int16_t i)
    { return (int16_t)ReverseBytes((uint16_t)i); }
static inline constexpr int32_t ReverseBytes(int32_t i)
    { return (int32_t)ReverseBytes((uint32_t)i); }
static inline constexpr int64_t ReverseBytes(int64_t i)
    { return (int64_t)ReverseBytes((uint64_t)i); }

#ifdef RG_BIG_ENDIAN
template <typename T>
constexpr T LittleEndian(T v) { return ReverseBytes(v); }
template <typename T>
constexpr T BigEndian(T v) { return v; }
#else
template <typename T>
constexpr T LittleEndian(T v) { return v; }
template <typename T>
constexpr T BigEndian(T v) { return ReverseBytes(v); }
#endif

#if defined(__GNUC__)
    static inline int CountLeadingZeros(uint32_t u)
    {
        if (!u)
            return 32;

        return __builtin_clz(u);
    }
    static inline int CountLeadingZeros(uint64_t u)
    {
        if (!u)
            return 64;

    #if UINT64_MAX == ULONG_MAX
        return __builtin_clzl(u);
    #elif UINT64_MAX == ULLONG_MAX
        return __builtin_clzll(u);
    #else
        #error Neither unsigned long nor unsigned long long is a 64-bit unsigned integer
    #endif
    }

    static inline int CountTrailingZeros(uint32_t u)
    {
        if (!u)
            return 32;

        return __builtin_ctz(u);
    }
    static inline int CountTrailingZeros(uint64_t u)
    {
        if (!u)
            return 64;

    #if UINT64_MAX == ULONG_MAX
        return __builtin_ctzl(u);
    #elif UINT64_MAX == ULLONG_MAX
        return __builtin_ctzll(u);
    #else
        #error Neither unsigned long nor unsigned long long is a 64-bit unsigned integer
    #endif
    }

    static inline int PopCount(uint32_t u)
    {
        return __builtin_popcount(u);
    }
    static inline int PopCount(uint64_t u)
    {
        return __builtin_popcountll(u);
    }
#elif defined(_MSC_VER)
    static inline int CountLeadingZeros(uint32_t u)
    {
        unsigned long leading_zero;
        if (_BitScanReverse(&leading_zero, u)) {
            return (int)(31 - leading_zero);
        } else {
            return 32;
        }
    }
    static inline int CountLeadingZeros(uint64_t u)
    {
        unsigned long leading_zero;
    #ifdef _WIN64
        if (_BitScanReverse64(&leading_zero, u)) {
            return (int)(63 - leading_zero);
        } else {
            return 64;
        }
    #else
        if (_BitScanReverse(&leading_zero, u >> 32)) {
            return (int)(31 - leading_zero);
        } else if (_BitScanReverse(&leading_zero, (uint32_t)u)) {
            return (int)(63 - leading_zero);
        } else {
            return 64;
        }
    #endif
    }

    static inline int CountTrailingZeros(uint32_t u)
    {
        unsigned long trailing_zero;
        if (_BitScanForward(&trailing_zero, u)) {
            return (int)trailing_zero;
        } else {
            return 32;
        }
    }
    static inline int CountTrailingZeros(uint64_t u)
    {
        unsigned long trailing_zero;
    #ifdef _WIN64
        if (_BitScanForward64(&trailing_zero, u)) {
            return (int)trailing_zero;
        } else {
            return 64;
        }
    #else
        if (_BitScanForward(&trailing_zero, (uint32_t)u)) {
            return trailing_zero;
        } else if (_BitScanForward(&trailing_zero, u >> 32)) {
            return 32 + trailing_zero;
        } else {
            return 64;
        }
    #endif
    }

    static inline int PopCount(uint32_t u)
    {
    #ifdef _M_ARM64
        uint32_t count;

        u = u - ((u >> 1) & 0x55555555);
        u = (u & 0x33333333) + ((u >> 2) & 0x33333333);
        count = ((u + (u >> 4) & 0xF0F0F0F) * 0x1010101) >> 24;

        return (int)count;
    #else
        return __popcnt(u);
    #endif
    }
    static inline int PopCount(uint64_t u)
    {
    #ifdef _M_X64
        return (int)__popcnt64(u);
    #else
        int count = PopCount((uint32_t)(u >> 32)) + PopCount((uint32_t)u);
        return count;
    #endif
    }
#else
    #error No implementation of CountLeadingZeros(), CountTrailingZeros() and PopCount() for this compiler / toolchain
#endif

static inline Size AlignLen(Size len, Size align)
{
    Size aligned = (len + align - 1) / align * align;
    return aligned;
}

template <typename T>
static inline T *AlignUp(T *ptr, Size align)
{
    uint8_t *aligned = (uint8_t *)(((uintptr_t)ptr + align - 1) / align * align);
    return (T *)aligned;
}

template <typename T>
static inline T *AlignDown(T *ptr, Size align)
{
    uint8_t *aligned = (uint8_t *)((uintptr_t)ptr / align * align);
    return (T *)aligned;
}

// Calling memcpy (and friends) with a NULL source pointer is undefined behavior
// even if length is 0. This is dumb, work around this.
static inline void *memcpy_safe(void *dest, const void *src, size_t len)
{
    if (len) {
        memcpy(dest, src, len);
    }
    return dest;
}
static inline void *memmove_safe(void *dest, const void *src, size_t len)
{
    if (len) {
        memmove(dest, src, len);
    }
    return dest;
}
static inline void *memset_safe(void *dest, int c, size_t len)
{
    if (len) {
        memset(dest, c, len);
    }
    return dest;
}

template <typename T, typename = typename std::enable_if<std::is_enum<T>::value, T>>
typename std::underlying_type<T>::type MaskEnum(T value)
{
    auto mask = 1 << static_cast<typename std::underlying_type<T>::type>(value);
    return (typename std::underlying_type<T>::type)mask;
}

template <typename Fun>
class DeferGuard {
    RG_DELETE_COPY(DeferGuard)

    Fun f;
    bool enabled;

public:
    DeferGuard() = delete;
    DeferGuard(Fun f_, bool enable = true) : f(std::move(f_)), enabled(enable) {}
    ~DeferGuard()
    {
        if (enabled) {
            f();
        }
    }

    DeferGuard(DeferGuard &&other)
        : f(std::move(other.f)), enabled(other.enabled)
    {
        other.enabled = false;
    }

    void Disable() { enabled = false; }
};

// Honestly, I don't understand all the details in there, this comes from Andrei Alexandrescu.
// https://channel9.msdn.com/Shows/Going+Deep/C-and-Beyond-2012-Andrei-Alexandrescu-Systematic-Error-Handling-in-C
struct DeferGuardHelper {};
template <typename Fun>
DeferGuard<Fun> operator+(DeferGuardHelper, Fun &&f)
{
    return DeferGuard<Fun>(std::forward<Fun>(f));
}

// Write 'DEFER { code };' to do something at the end of the current scope, you
// can use DEFER_N(Name) if you need to disable the guard for some reason, and
// DEFER_NC(Name, Captures) if you need to capture values.
#define RG_DEFER \
    auto RG_UNIQUE_NAME(defer) = RG::DeferGuardHelper() + [&]()
#define RG_DEFER_N(Name) \
    auto Name = RG::DeferGuardHelper() + [&]()
#define RG_DEFER_C(...) \
    auto RG_UNIQUE_NAME(defer) = RG::DeferGuardHelper() + [&, __VA_ARGS__]()
#define RG_DEFER_NC(Name, ...) \
    auto Name = RG::DeferGuardHelper() + [&, __VA_ARGS__]()

// Heavily inspired from FunctionRef in LLVM
template<typename Fn> class FunctionRef;
template<typename Ret, typename ...Params>
class FunctionRef<Ret(Params...)> {
    Ret (*callback)(intptr_t callable, Params ...params) = nullptr;
    intptr_t callable;

    template<typename Callable>
    static Ret callback_fn(intptr_t callable, Params ...params)
        { return (*reinterpret_cast<Callable*>(callable))(std::forward<Params>(params)...); }

public:
    FunctionRef() = default;

    template <typename Callable>
    FunctionRef(Callable &&callable,
                std::enable_if_t<!std::is_same<std::remove_cv_t<std::remove_reference_t<Callable>>, FunctionRef>::value> * = nullptr,
                std::enable_if_t<std::is_void<Ret>::value ||
                                 std::is_convertible<decltype(std::declval<Callable>()(std::declval<Params>()...)),
                                                     Ret>::value> * = nullptr)
      : callback(callback_fn<typename std::remove_reference<Callable>::type>),
        callable(reinterpret_cast<intptr_t>(&callable)) {}

    Ret operator()(Params ...params) const
        { return callback(callable, std::forward<Params>(params)...); }

    bool IsValid() const { return callback; }
};

#define RG_INIT_(ClassName) \
    class ClassName { \
    public: \
        ClassName(); \
    }; \
    static ClassName RG_UNIQUE_NAME(init); \
    ClassName::ClassName()
#define RG_INIT(Name) RG_INIT_(RG_CONCAT(RG_UNIQUE_NAME(InitHelper), Name))

#define RG_EXIT_(ClassName) \
    class ClassName { \
    public: \
        ~ClassName(); \
    }; \
    static ClassName RG_UNIQUE_NAME(exit); \
    ClassName::~ClassName()
#define RG_EXIT(Name) RG_EXIT_(RG_CONCAT(RG_UNIQUE_NAME(ExitHelper), Name))

template <typename T>
T MultiCmp()
{
    return 0;
}
template <typename T, typename... Args>
T MultiCmp(T cmp_value, Args... other_args)
{
    if (cmp_value) {
        return cmp_value;
    } else {
        return MultiCmp<T>(other_args...);
    }
}

template <typename T, typename U>
T ApplyMask(T value, U mask, bool enable)
{
    if (enable) {
        return value | (T)mask;
    } else {
        return value & ~(T)mask;
    }
}

enum class ParseFlag {
    Log = 1 << 0,
    Validate = 1 << 1,
    End = 1 << 2
};
#define RG_DEFAULT_PARSE_FLAGS ((int)ParseFlag::Log | (int)ParseFlag::Validate | (int)ParseFlag::End)

template <typename T>
struct Vec2 {
    T x;
    T y;
};

template <typename T>
struct Vec3 {
    T x;
    T y;
    T z;
};

// ------------------------------------------------------------------------
// Memory / Allocator
// ------------------------------------------------------------------------

// I'd love to make Span default to { nullptr, 0 } but unfortunately that makes
// it a non-POD and prevents putting it in a union.
template <typename T>
struct Span {
    T *ptr;
    Size len;

    Span() = default;
    constexpr Span(T &value) : ptr(&value), len(1) {}
    constexpr Span(std::initializer_list<T> l) : ptr(l.begin()), len((Size)l.size()) {}
    constexpr Span(T *ptr_, Size len_) : ptr(ptr_), len(len_) {}
    template <Size N>
    constexpr Span(T (&arr)[N]) : ptr(arr), len(N) {}

    void Reset()
    {
        ptr = nullptr;
        len = 0;
    }

    T *begin() { return ptr; }
    const T *begin() const { return ptr; }
    T *end() { return ptr + len; }
    const T *end() const { return ptr + len; }

    bool IsValid() const { return ptr; }

    T &operator[](Size idx)
    {
        RG_ASSERT(idx >= 0 && idx < len);
        return ptr[idx];
    }
    const T &operator[](Size idx) const
    {
        RG_ASSERT(idx >= 0 && idx < len);
        return ptr[idx];
    }

    operator Span<const T>() const { return Span<const T>(ptr, len); }

    bool operator==(const Span &other) const
    {
        if (len != other.len)
            return false;

        for (Size i = 0; i < len; i++) {
            if (ptr[i] != other.ptr[i])
                return false;
        }

        return true;
    }
    bool operator!=(const Span &other) const { return !(*this == other); }

    Span Take(Size offset, Size sub_len) const
    {
        RG_ASSERT(sub_len >= 0 && sub_len <= len);
        RG_ASSERT(offset >= 0 && offset <= len - sub_len);

        Span<T> sub;
        sub.ptr = ptr + offset;
        sub.len = sub_len;
        return sub;
    }

    template <typename U>
    Span<U> As() const { return Span<U>((U *)ptr, len); }
};

// Use strlen() to build Span<const char> instead of the template-based
// array constructor.
template <>
struct Span<const char> {
    const char *ptr;
    Size len;

    Span() = default;
    constexpr Span(const char &ch) : ptr(&ch), len(1) {}
    constexpr Span(const char *ptr_, Size len_) : ptr(ptr_), len(len_) {}
#ifdef __clang__
    constexpr Span(const char *const &str) : ptr(str), len(str ? (Size)__builtin_strlen(str) : 0) {}
#else
    constexpr Span(const char *const &str) : ptr(str), len(str ? (Size)strlen(str) : 0) {}
#endif

    void Reset()
    {
        ptr = nullptr;
        len = 0;
    }

    const char *begin() const { return ptr; }
    const char *end() const { return ptr + len; }

    bool IsValid() const { return ptr; }

    char operator[](Size idx) const
    {
        RG_ASSERT(idx >= 0 && idx < len);
        return ptr[idx];
    }

    // The implementation comes later, after TestStr() is available
    bool operator==(Span<const char> other) const;
    bool operator==(const char *other) const;
    bool operator!=(Span<const char> other) const { return !(*this == other); }
    bool operator!=(const char *other) const { return !(*this == other); }

    Span Take(Size offset, Size sub_len) const
    {
        RG_ASSERT(sub_len >= 0 && sub_len <= len);
        RG_ASSERT(offset >= 0 && offset <= len - sub_len);

        Span<const char> sub;
        sub.ptr = ptr + offset;
        sub.len = sub_len;
        return sub;
    }

    template <typename U>
    Span<U> As() const { return Span<U>((U *)ptr, len); }
};

template <typename T>
static inline constexpr Span<T> MakeSpan(T *ptr, Size len)
{
    return Span<T>(ptr, len);
}
template <typename T>
static inline constexpr Span<T> MakeSpan(T *ptr, T *end)
{
    return Span<T>(ptr, end - ptr);
}
template <typename T, Size N>
static inline constexpr Span<T> MakeSpan(T (&arr)[N])
{
    return Span<T>(arr, N);
}

template <typename T>
class Strider {
public:
    void *ptr = nullptr;
    Size stride;

    Strider() = default;
    constexpr Strider(T *ptr_) : ptr(ptr_), stride(RG_SIZE(T)) {}
    constexpr Strider(T *ptr_, Size stride_) : ptr(ptr_), stride(stride_) {}

    bool IsValid() const { return ptr; }

    T &operator[](Size idx) const
    {
        RG_ASSERT(idx >= 0);
        return *(T *)((uint8_t *)ptr + (idx * stride));
    }
};

template <typename T>
static inline constexpr Strider<T> MakeStrider(T *ptr)
{
    return Strider<T>(ptr, RG_SIZE(T));
}
template <typename T>
static inline constexpr Strider<T> MakeStrider(T *ptr, Size stride)
{
    return Strider<T>(ptr, stride);
}
template <typename T, Size N>
static inline constexpr Strider<T> MakeStrider(T (&arr)[N])
{
    return Strider<T>(arr, RG_SIZE(T));
}

enum class AllocFlag {
    Zero = 1,
    Resizable = 2
};

class Allocator {
    RG_DELETE_COPY(Allocator)

public:
    Allocator() = default;
    virtual ~Allocator() = default;

    virtual void *Allocate(Size size, unsigned int flags = 0) = 0;
    virtual void *Resize(void *ptr, Size old_size, Size new_size, unsigned int flags = 0) = 0;
    virtual void Release(const void *ptr, Size size) = 0;
};

Allocator *GetDefaultAllocator();
Allocator *GetNullAllocator();

static inline void *AllocateRaw(Allocator *alloc, Size size, unsigned int flags = 0)
{
    RG_ASSERT(size >= 0);

    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    void *ptr = alloc->Allocate(size, flags);
    return ptr;
}

template <typename T>
T *AllocateOne(Allocator *alloc, unsigned int flags = 0)
{
    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    Size size = RG_SIZE(T);

    T *ptr = (T *)alloc->Allocate(size, flags);
    return ptr;
}

template <typename T>
Span<T> AllocateSpan(Allocator *alloc, Size len, unsigned int flags = 0)
{
    RG_ASSERT(len >= 0);

    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    Size size = len * RG_SIZE(T);

    T *ptr = (T *)alloc->Allocate(size, flags);
    return MakeSpan(ptr, len);
}

static inline void *ResizeRaw(Allocator *alloc, void *ptr, Size old_size, Size new_size,
                              unsigned int flags = 0)
{
    RG_ASSERT(new_size >= 0);

    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    ptr = alloc->Resize(ptr, old_size, new_size, flags);
    return ptr;
}

template <typename T>
Span<T> ResizeSpan(Allocator *alloc, Span<T> mem, Size new_len,
                   unsigned int flags = 0)
{
    RG_ASSERT(new_len >= 0);

    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    Size old_size = mem.len * RG_SIZE(T);
    Size new_size = new_len * RG_SIZE(T);

    mem.ptr = (T *)alloc->Resize(mem.ptr, old_size, new_size, flags);
    return MakeSpan(mem.ptr, new_len);
}

static inline void ReleaseRaw(Allocator *alloc, const void *ptr, Size size)
{
    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    alloc->Release(ptr, size);
}

template<typename T>
void ReleaseOne(Allocator *alloc, T *ptr)
{
    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    alloc->Release((void *)ptr, RG_SIZE(T));
}

template<typename T>
void ReleaseSpan(Allocator *alloc, Span<T> mem)
{
    if (!alloc) {
        alloc = GetDefaultAllocator();
    }

    Size size = mem.len * RG_SIZE(T);

    alloc->Release((void *)mem.ptr, size);
}

class LinkedAllocator final: public Allocator {
    struct Node {
        Node *prev;
        Node *next;
    };
    struct Bucket {
         // Keep head first or stuff will break
        Node head;
        uint8_t data[];
    };

    Allocator *allocator;
    // We want allocators to be memmovable, which means we can't use a circular linked list.
    // Even though it makes the code less nice.
    Node list = {};

public:
    LinkedAllocator(Allocator *alloc = nullptr) : allocator(alloc) {}
    ~LinkedAllocator() override { ReleaseAll(); }

    LinkedAllocator(LinkedAllocator &&other) { *this = std::move(other); }
    LinkedAllocator& operator=(LinkedAllocator &&other);

    void ReleaseAll();

    void *Allocate(Size size, unsigned int flags = 0) override;
    void *Resize(void *ptr, Size old_size, Size new_size, unsigned int flags = 0) override;
    void Release(const void *ptr, Size size) override;

    bool IsUsed() const { return list.next; }

private:
    static Bucket *PointerToBucket(void *ptr);
};

class BlockAllocatorBase: public Allocator {
    struct Bucket {
        Size used;
        uint8_t data[];
    };

    Size block_size;

    Bucket *current_bucket = nullptr;
    uint8_t *last_alloc = nullptr;

public:
    BlockAllocatorBase(Size block_size = RG_BLOCK_ALLOCATOR_DEFAULT_SIZE)
        : block_size(block_size)
    {
        RG_ASSERT(block_size > 0);
    }

    void *Allocate(Size size, unsigned int flags = 0) override;
    void *Resize(void *ptr, Size old_size, Size new_size, unsigned int flags = 0) override;
    void Release(const void *ptr, Size size) override;

    bool IsUsed() const
    {
        LinkedAllocator *alloc = ((BlockAllocatorBase *)this)->GetAllocator();
        return alloc->IsUsed();
    }

protected:
    void CopyFrom(BlockAllocatorBase *other);
    void ForgetCurrentBlock();

    virtual LinkedAllocator *GetAllocator() = 0;

private:
    bool AllocateSeparately(Size aligned_size) const { return aligned_size >= block_size / 2; }
};

class BlockAllocator final: public BlockAllocatorBase {
    LinkedAllocator allocator;

protected:
    LinkedAllocator *GetAllocator() override { return &allocator; }

public:
    BlockAllocator(Size block_size = RG_BLOCK_ALLOCATOR_DEFAULT_SIZE)
        : BlockAllocatorBase(block_size) {}

    BlockAllocator(BlockAllocator &&other) { *this = std::move(other); }
    BlockAllocator& operator=(BlockAllocator &&other);

    void ReleaseAll();
};

class IndirectBlockAllocator final: public BlockAllocatorBase {
    LinkedAllocator *allocator;

protected:
    LinkedAllocator *GetAllocator() override { return allocator; }

public:
    IndirectBlockAllocator(LinkedAllocator *alloc, Size block_size = RG_BLOCK_ALLOCATOR_DEFAULT_SIZE)
        : BlockAllocatorBase(block_size), allocator(alloc) {}

    IndirectBlockAllocator(IndirectBlockAllocator &&other) { *this = std::move(other); }
    IndirectBlockAllocator& operator=(IndirectBlockAllocator &&other);

    void ReleaseAll();
};

// ------------------------------------------------------------------------
// Reference counting
// ------------------------------------------------------------------------

template <typename T>
class RetainPtr {
    T *p = nullptr;

public:
    RetainPtr() = default;
    RetainPtr(T *p, void (*delete_func)(std::remove_const_t<T> *))
        : p(p)
    {
        RG_ASSERT(p);
        RG_ASSERT(delete_func);
        RG_ASSERT(!p->delete_func || delete_func == p->delete_func);

        p->Ref();
        p->delete_func = delete_func;
    }
    RetainPtr(T *p, bool ref = true)
        : p(p)
    {
        if (p) {
            RG_ASSERT(p->delete_func);

            if (ref) {
                p->Ref();
            }
        }
    }

    ~RetainPtr()
    {
        if (p && !p->Unref()) {
            p->delete_func((std::remove_const_t<T> *)p);
        }
    }

    RetainPtr(const RetainPtr &other)
    {
        p = other.p;
        if (p) {
            p->Ref();
        }
    }
    RetainPtr &operator=(const RetainPtr &other)
    {
        if (p && !p->Unref()) {
            p->delete_func((std::remove_const_t<T> *)p);
        }

        p = other.p;
        if (p) {
            p->Ref();
        }

        return *this;
    }

    operator RetainPtr<const T>() const
    {
        RetainPtr<const T> ptr((const T *)p);
        return ptr;
    }

    bool IsValid() const { return p; }
    operator bool() const { return p; }

    T &operator*() const
    {
        RG_ASSERT(p);
        return *p;
    }
    T *operator->() const { return p; }
    T *GetRaw() const { return p; }
};

template <typename T>
class RetainObject {
    mutable void (*delete_func)(T *) = nullptr;
    mutable std::atomic_int refcount { 0 };

public:
    void Ref() const { refcount++; }
    bool Unref() const
    {
        int new_count = --refcount;
        RG_ASSERT(new_count >= 0);
        return new_count;
    }

    friend class RetainPtr<T>;
    friend class RetainPtr<const T>;
};

// ------------------------------------------------------------------------
// Collections
// ------------------------------------------------------------------------

template <typename T, Size N, Size AlignAs = alignof(T)>
class LocalArray {
public:
    alignas(AlignAs) T data[N];
    Size len = 0;

    typedef T value_type;
    typedef T *iterator_type;

    LocalArray() = default;
    LocalArray(std::initializer_list<T> l)
    {
        RG_ASSERT(l.size() <= N);
        for (const T &it: l) {
            data[len++] = it;
        }
        len = (Size)l.size();
    }

    void Clear()
    {
        for (Size i = 0; i < len; i++) {
            data[i] = T();
        }
        len = 0;
    }

    operator Span<T>() { return Span<T>(data, len); }
    operator Span<const T>() const { return Span<const T>(data, len); }

    T *begin() { return data; }
    const T *begin() const { return data; }
    T *end() { return data + len; }
    const T *end() const { return data + len; }

    Size Available() const { return RG_LEN(data) - len; }

    T &operator[](Size idx)
    {
        RG_ASSERT(idx >= 0 && idx < len);
        return data[idx];
    }
    const T &operator[](Size idx) const
    {
        RG_ASSERT(idx >= 0 && idx < len);
        return data[idx];
    }

    bool operator==(const LocalArray &other) const
    {
        if (len != other.len)
            return false;

        for (Size i = 0; i < len; i++) {
            if (data[i] != other.data[i])
                return false;
        }

        return true;
    }
    bool operator!=(const LocalArray &other) const { return !(*this == other); }

    T *AppendDefault(Size count = 1)
    {
        RG_ASSERT(len <= N - count);

        T *first = data + len;
        if constexpr(!std::is_trivial<T>::value) {
            for (Size i = 0; i < count; i++) {
                new (data + len) T();
                len++;
            }
        } else {
            memset_safe(first, 0, count * RG_SIZE(T));
            len += count;
        }

        return first;
    }

    T *Append(const T &value)
    {
        RG_ASSERT(len < N);

        T *it = data + len;
        *it = value;
        len++;

        return it;
    }
    T *Append(Span<const T> values)
    {
        RG_ASSERT(values.len <= N - len);

        T *it = data + len;
        for (Size i = 0; i < values.len; i++) {
            data[len + i] = values[i];
        }
        len += values.len;

        return it;
    }

    void RemoveFrom(Size first)
    {
        RG_ASSERT(first >= 0 && first <= len);

        for (Size i = first; i < len; i++) {
            data[i] = T();
        }
        len = first;
    }
    void RemoveLast(Size count = 1)
    {
        RG_ASSERT(count >= 0 && count <= len);
        RemoveFrom(len - count);
    }

    Span<T> Take() const { return Span<T>((T *)data, len); }
    Span<T> Take(Size offset, Size len) const { return Span<T>((T *)data, N).Take(offset, len); }
    Span<T> TakeAvailable() const { return Span<T>((T *)data + len, N - len); }

    template <typename U = T>
    Span<U> As() const { return Span<U>((U *)data, len); }
};

template <typename T>
class HeapArray {
    // StaticAssert(std::is_trivially_copyable<T>::value);

public:
    T *ptr = nullptr;
    Size len = 0;
    Size capacity = 0;
    Allocator *allocator = nullptr;

    typedef T value_type;
    typedef T *iterator_type;

    HeapArray() = default;
    HeapArray(Allocator *alloc, Size min_capacity = 0) : allocator(alloc)
        { SetCapacity(min_capacity); }
    HeapArray(Size min_capacity) { Reserve(min_capacity); }
    HeapArray(std::initializer_list<T> l)
    {
        Reserve(l.size());
        for (const T &it: l) {
            ptr[len++] = it;
        }
    }
    ~HeapArray() { Clear(); }

    HeapArray(HeapArray &&other) { *this = std::move(other); }
    HeapArray &operator=(HeapArray &&other)
    {
        Clear();
        memmove_safe(this, &other, RG_SIZE(other));
        memset_safe(&other, 0, RG_SIZE(other));
        return *this;
    }
    HeapArray(const HeapArray &other) { *this = other; }
    HeapArray &operator=(const HeapArray &other)
    {
        RemoveFrom(0);
        Grow(other.capacity);
        if constexpr(!std::is_trivial<T>::value) {
            for (Size i = 0; i < other.len; i++) {
                ptr[i] = other.ptr[i];
            }
        } else {
            memcpy_safe(ptr, other.ptr, (size_t)(other.len * RG_SIZE(*ptr)));
        }
        len = other.len;
        return *this;
    }

    void Clear()
    {
        RemoveFrom(0);
        SetCapacity(0);
    }

    operator Span<T>() { return Span<T>(ptr, len); }
    operator Span<const T>() const { return Span<const T>(ptr, len); }

    T *begin() { return ptr; }
    const T *begin() const { return ptr; }
    T *end() { return ptr + len; }
    const T *end() const { return ptr + len; }

    Size Available() const { return capacity - len; }

    T &operator[](Size idx)
    {
        RG_ASSERT(idx >= 0 && idx < len);
        return ptr[idx];
    }
    const T &operator[](Size idx) const
    {
        RG_ASSERT(idx >= 0 && idx < len);
        return ptr[idx];
    }

    bool operator==(const HeapArray &other) const
    {
        if (len != other.len)
            return false;

        for (Size i = 0; i < len; i++) {
            if (ptr[i] != other.ptr[i])
                return false;
        }

        return true;
    }
    bool operator!=(const HeapArray &other) const { return !(*this == other); }

    void SetCapacity(Size new_capacity)
    {
        RG_ASSERT(new_capacity >= 0);

        if (new_capacity != capacity) {
            if (len > new_capacity) {
                for (Size i = new_capacity; i < len; i++) {
                    ptr[i].~T();
                }
                len = new_capacity;
            }

            ptr = (T *)ResizeRaw(allocator, ptr, capacity * RG_SIZE(T), new_capacity * RG_SIZE(T));
            capacity = new_capacity;
        }
    }

    void Reserve(Size min_capacity)
    {
        if (min_capacity > capacity) {
            SetCapacity(min_capacity);
        }
    }

    T *Grow(Size reserve_capacity = 1)
    {
        RG_ASSERT(capacity >= 0);
        RG_ASSERT(reserve_capacity >= 0);
        RG_ASSERT((size_t)capacity + (size_t)reserve_capacity <= RG_SIZE_MAX);

        if (reserve_capacity > capacity - len) {
            Size needed = capacity + reserve_capacity;

            Size new_capacity;
            if (needed <= RG_HEAPARRAY_BASE_CAPACITY) {
                new_capacity = RG_HEAPARRAY_BASE_CAPACITY;
            } else {
                new_capacity = (Size)((double)(needed - 1) * RG_HEAPARRAY_GROWTH_FACTOR);
            }

            SetCapacity(new_capacity);
        }

        return ptr + len;
    }

    void Trim(Size extra_capacity = 0) { SetCapacity(len + extra_capacity); }

    T *AppendDefault(Size count = 1)
    {
        Grow(count);

        T *first = ptr + len;
        if constexpr(!std::is_trivial<T>::value) {
            for (Size i = 0; i < count; i++) {
                new (ptr + len) T();
                len++;
            }
        } else {
            memset_safe(first, 0, count * RG_SIZE(T));
            len += count;
        }

        return first;
    }

    T *Append(const T &value)
    {
        Grow();

        T *first = ptr + len;
        if constexpr(!std::is_trivial<T>::value) {
            new (ptr + len) T;
        }
        ptr[len++] = value;
        return first;
    }
    T *Append(Span<const T> values)
    {
        Grow(values.len);

        T *first = ptr + len;
        for (const T &value: values) {
            if constexpr(!std::is_trivial<T>::value) {
                new (ptr + len) T;
            }
            ptr[len++] = value;
        }
        return first;
    }

    void RemoveFrom(Size first)
    {
        RG_ASSERT(first >= 0 && first <= len);

        if constexpr(!std::is_trivial<T>::value) {
            for (Size i = first; i < len; i++) {
                ptr[i].~T();
            }
        }
        len = first;
    }
    void RemoveLast(Size count = 1)
    {
        RG_ASSERT(count >= 0 && count <= len);
        RemoveFrom(len - count);
    }

    Span<T> Take() const { return Span<T>(ptr, len); }
    Span<T> Take(Size offset, Size len) const { return Span<T>(ptr, this->len).Take(offset, len); }
    Span<T> TakeAvailable() const { return Span<T>((T *)ptr + len, capacity - len); }

    Span<T> Leak()
    {
        Span<T> span = *this;

        ptr = nullptr;
        len = 0;
        capacity = 0;

        return span;
    }
    Span<T> TrimAndLeak(Size extra_capacity = 0)
    {
        Trim(extra_capacity);
        return Leak();
    }

    template <typename U = T>
    Span<U> As() const { return Span<U>((U *)ptr, len); }
};

template <typename T, Size BucketSize = 64, typename AllocatorType = BlockAllocator>
class BucketArray {
    RG_DELETE_COPY(BucketArray)

public:
    struct Bucket {
        T *values;
        AllocatorType allocator;
    };

    template <typename U>
    class Iterator {
    public:
        typedef std::bidirectional_iterator_tag iterator_category;
        typedef Size value_type;
        typedef Size difference_type;
        typedef Iterator *pointer;
        typedef Iterator &reference;

        U *queue = nullptr;
        Size bucket_idx;
        Size bucket_offset;
        Bucket *bucket;
        Bucket *next_bucket;

        Iterator() = default;
        Iterator(U *queue, Size bucket_idx, Size bucket_offset)
            : queue(queue), bucket_idx(bucket_idx), bucket_offset(bucket_offset),
              bucket(GetBucketSafe(bucket_idx)), next_bucket(GetBucketSafe(bucket_idx + 1)) {}

        T *operator->() { return &bucket->values[bucket_offset]; }
        const T *operator->() const { return &bucket->values[bucket_offset]; }
        T &operator*() { return bucket->values[bucket_offset]; }
        const T &operator*() const { return bucket->values[bucket_offset]; }

        Iterator &operator++()
        {
            if (++bucket_offset >= BucketSize) {
                bucket_idx++;
                if (next_bucket) {
                    // We support deletion of all values up to (and including) the current one.
                    // When the user does that, some or all front buckets may be gone, but we can
                    // use next_bucket to fix bucket_idx.
                    while (bucket_idx >= queue->buckets.len ||
                           queue->buckets[bucket_idx] != next_bucket) {
                        bucket_idx--;
                    }
                }
                bucket_offset = 0;

                bucket = GetBucketSafe(bucket_idx);
                next_bucket = GetBucketSafe(bucket_idx + 1);
            }

            return *this;
        }
        Iterator operator++(int)
        {
            Iterator ret = *this;
            ++(*this);
            return ret;
        }

        Iterator &operator--()
        {
            if (--bucket_offset < 0) {
                RG_ASSERT(bucket_idx > 0);

                bucket_idx--;
                bucket_offset = BucketSize - 1;

                bucket = GetBucketSafe(bucket_idx);
                next_bucket = GetBucketSafe(bucket_idx + 1);
            }

            return *this;
        }
        Iterator operator--(int)
        {
            Iterator ret = *this;
            --(*this);
            return ret;
        }

        bool operator==(const Iterator &other) const
            { return queue == other.queue && bucket == other.bucket &&
                     bucket_offset == other.bucket_offset; }
        bool operator!=(const Iterator &other) const { return !(*this == other); }

    private:
        Bucket *GetBucketSafe(Size idx)
            { return idx < queue->buckets.len ? queue->buckets[idx] : nullptr; }
    };

    HeapArray<Bucket *> buckets;
    Size offset = 0;
    Size len = 0;

    typedef T value_type;
    typedef Iterator<BucketArray> iterator_type;

    BucketArray() {}
    BucketArray(std::initializer_list<T> l)
    {
        for (const T &value: l) {
            Append(value);
        }
    }
    ~BucketArray() { ClearBucketsAndValues(); }

    BucketArray(BucketArray &&other) { *this = std::move(other); }
    BucketArray &operator=(BucketArray &&other)
    {
        ClearBucketsAndValues();
        memmove_safe(this, &other, RG_SIZE(other));
        memset_safe(&other, 0, RG_SIZE(other));
        return *this;
    }

    void Clear()
    {
        ClearBucketsAndValues();

        offset = 0;
        len = 0;
    }

    iterator_type begin() { return iterator_type(this, 0, offset); }
    Iterator<const BucketArray<T, BucketSize>> begin() const { return Iterator<const BucketArray>(this, 0, offset); }
    iterator_type end()
    {
        Size end_idx = offset + len;
        Size bucket_idx = end_idx / BucketSize;
        Size bucket_offset = end_idx % BucketSize;

        return iterator_type(this, bucket_idx, bucket_offset);
    }
    Iterator<const BucketArray<T, BucketSize>> end() const
    {
        Size end_idx = offset + len;
        Size bucket_idx = end_idx / BucketSize;
        Size bucket_offset = end_idx % BucketSize;

        return Iterator<const BucketArray>(this, bucket_idx, bucket_offset);
    }

    const T &operator[](Size idx) const
    {
        RG_ASSERT(idx >= 0 && idx < len);

        idx += offset;
        Size bucket_idx = idx / BucketSize;
        Size bucket_offset = idx % BucketSize;

        return buckets[bucket_idx]->values[bucket_offset];
    }
    T &operator[](Size idx) { return (T &)(*(const BucketArray *)this)[idx]; }

    T *AppendDefault(Allocator **out_alloc = nullptr)
    {
        Size bucket_idx = (offset + len) / BucketSize;
        Size bucket_offset = (offset + len) % BucketSize;

        if (bucket_idx >= buckets.len) {
            Bucket *new_bucket = AllocateOne<Bucket>(buckets.allocator);
            new (&new_bucket->allocator) AllocatorType();
            new_bucket->values = (T *)AllocateRaw(&new_bucket->allocator, BucketSize * RG_SIZE(T));

            buckets.Append(new_bucket);
        }

        T *first = buckets[bucket_idx]->values + bucket_offset;
        new (first) T();

        len++;

        if (out_alloc) {
            *out_alloc = &buckets[bucket_idx]->allocator;
        }
        return first;
    }

    T *Append(const T &value, Allocator **out_alloc = nullptr)
    {
        T *it = AppendDefault(out_alloc);
        *it = value;
        return it;
    }

    void RemoveFrom(Size from)
    {
        RG_ASSERT(from >= 0 && from <= len);

        if (from == len)
            return;
        if (!from) {
            Clear();
            return;
        }

        Size start_idx = offset + from;
        Size start_bucket_idx = start_idx / BucketSize;
        Size start_bucket_offset = start_idx % BucketSize;

        iterator_type from_it(this, start_bucket_idx, start_bucket_offset);
        DeleteValues(from_it, end());

        Size delete_idx = start_bucket_idx + !!start_bucket_offset;
        for (Size i = delete_idx; i < buckets.len; i++) {
            DeleteBucket(buckets[i]);
        }
        buckets.RemoveFrom(delete_idx);

        len = from;
    }
    void RemoveLast(Size count = 1)
    {
        RG_ASSERT(count >= 0 && count <= len);
        RemoveFrom(len - count);
    }

    void RemoveFirst(Size count = 1)
    {
        RG_ASSERT(count >= 0 && count <= len);

        if (count == len) {
            Clear();
            return;
        }

        Size end_idx = offset + count;
        Size end_bucket_idx = end_idx / BucketSize;
        Size end_bucket_offset = end_idx % BucketSize;

        iterator_type until_it(this, end_bucket_idx, end_bucket_offset);
        DeleteValues(begin(), until_it);

        if (end_bucket_idx) {
            for (Size i = 0; i < end_bucket_idx; i++) {
                DeleteBucket(buckets[i]);
            }
            memmove_safe(&buckets[0], &buckets[end_bucket_idx],
                         (size_t)((buckets.len - end_bucket_idx) * RG_SIZE(Bucket *)));
            buckets.RemoveLast(end_bucket_idx);
        }

        offset = (offset + count) % BucketSize;
        len -= count;
    }

    void Trim()
    {
        buckets.Trim();
    }

private:
    void ClearBucketsAndValues()
    {
        DeleteValues(begin(), end());

        for (Bucket *bucket: buckets) {
            DeleteBucket(bucket);
        }
        buckets.Clear();
    }

    void DeleteValues([[maybe_unused]] iterator_type begin,
                      [[maybe_unused]] iterator_type end)
    {
        if constexpr(!std::is_trivial<T>::value) {
            for (iterator_type it = begin; it != end; ++it) {
                it->~T();
            }
        }
    }

    void DeleteBucket(Bucket *bucket)
    {
        bucket->allocator.~AllocatorType();
        ReleaseOne(buckets.allocator, bucket);
    }
};

template <Size N>
class Bitset {
public:
    template <typename T>
    class Iterator {
    public:
        typedef std::input_iterator_tag iterator_category;
        typedef Size value_type;
        typedef Size difference_type;
        typedef Iterator *pointer;
        typedef Iterator &reference;

        T *bitset = nullptr;
        Size offset;
        size_t bits = 0;
        int ctz;

        Iterator() = default;
        Iterator(T *bitset, Size offset)
            : bitset(bitset), offset(offset - 1)
        {
            operator++();
        }

        Size operator*() const
        {
            RG_ASSERT(offset <= RG_LEN(bitset->data));

            if (offset == RG_LEN(bitset->data))
                return -1;
            return offset * RG_SIZE(size_t) * 8 + ctz;
        }

        Iterator &operator++()
        {
            RG_ASSERT(offset <= RG_LEN(bitset->data));

            while (!bits) {
                if (offset == RG_LEN(bitset->data) - 1)
                    return *this;
                bits = bitset->data[++offset];
            }

            ctz = CountTrailingZeros((uint64_t)bits);
            bits ^= (size_t)1 << ctz;

            return *this;
        }

        Iterator operator++(int)
        {
            Iterator ret = *this;
            ++(*this);
            return ret;
        }

        bool operator==(const Iterator &other) const
            { return bitset == other.bitset && offset == other.offset; }
        bool operator!=(const Iterator &other) const { return !(*this == other); }
    };

    typedef Size value_type;
    typedef Iterator<Bitset> iterator_type;

    static constexpr Size Bits = N;
    size_t data[(N + RG_BITS(size_t) - 1) / RG_BITS(size_t)] = {};

    void Clear()
    {
        memset_safe(data, 0, RG_SIZE(data));
    }

    Iterator<Bitset> begin() { return Iterator<Bitset>(this, 0); }
    Iterator<const Bitset> begin() const { return Iterator<const Bitset>(this, 0); }
    Iterator<Bitset> end() { return Iterator<Bitset>(this, RG_LEN(data)); }
    Iterator<const Bitset> end() const { return Iterator<const Bitset>(this, RG_LEN(data)); }

    Size PopCount() const
    {
        Size count = 0;
        for (size_t bits: data) {
#if RG_SIZE_MAX == INT64_MAX
            count += RG::PopCount((uint64_t)bits);
#else
            count += RG::PopCount((uint32_t)bits);
#endif
        }
        return count;
    }

    inline bool Test(Size idx) const
    {
        RG_ASSERT(idx >= 0 && idx < N);

        Size offset = idx / (RG_SIZE(size_t) * 8);
        size_t mask = (size_t)1 << (idx % (RG_SIZE(size_t) * 8));

        return data[offset] & mask;
    }
    inline void Set(Size idx, bool value = true)
    {
        RG_ASSERT(idx >= 0 && idx < N);

        Size offset = idx / (RG_SIZE(size_t) * 8);
        size_t mask = (size_t)1 << (idx % (RG_SIZE(size_t) * 8));

        data[offset] = ApplyMask(data[offset], mask, value);
    }
    inline bool TestAndSet(Size idx, bool value = true)
    {
        RG_ASSERT(idx >= 0 && idx < N);

        Size offset = idx / (RG_SIZE(size_t) * 8);
        size_t mask = (size_t)1 << (idx % (RG_SIZE(size_t) * 8));

        bool ret = data[offset] & mask;
        data[offset] = ApplyMask(data[offset], mask, value);

        return ret;
    }

    Bitset &operator&=(const Bitset &other)
    {
        for (Size i = 0; i < RG_LEN(data); i++) {
            data[i] &= other.data[i];
        }
        return *this;
    }
    Bitset operator&(const Bitset &other)
    {
        Bitset ret;
        for (Size i = 0; i < RG_LEN(data); i++) {
            ret.data[i] = data[i] & other.data[i];
        }
        return ret;
    }

    Bitset &operator|=(const Bitset &other)
    {
        for (Size i = 0; i < RG_LEN(data); i++) {
            data[i] |= other.data[i];
        }
        return *this;
    }
    Bitset operator|(const Bitset &other)
    {
        Bitset ret;
        for (Size i = 0; i < RG_LEN(data); i++) {
            ret.data[i] = data[i] | other.data[i];
        }
        return ret;
    }

    Bitset &operator^=(const Bitset &other)
    {
        for (Size i = 0; i < RG_LEN(data); i++) {
            data[i] ^= other.data[i];
        }
        return *this;
    }
    Bitset operator^(const Bitset &other)
    {
        Bitset ret;
        for (Size i = 0; i < RG_LEN(data); i++) {
            ret.data[i] = data[i] ^ other.data[i];
        }
        return ret;
    }

    Bitset &Flip()
    {
        for (Size i = 0; i < RG_LEN(data); i++) {
            data[i] = ~data[i];
        }
        return *this;
    }
    Bitset operator~()
    {
        Bitset ret;
        for (Size i = 0; i < RG_LEN(data); i++) {
            ret.data[i] = ~data[i];
        }
        return ret;
    }

    // XXX: Shift operators
};

template <typename KeyType, typename ValueType,
          typename Handler = typename std::remove_pointer<ValueType>::type::HashHandler>
class HashTable {
public:
    template <typename T>
    class Iterator {
    public:
        typedef std::forward_iterator_tag iterator_category;
        typedef ValueType value_type;
        typedef Size difference_type;
        typedef Iterator *pointer;
        typedef Iterator &reference;

        T *table = nullptr;
        Size offset;

        Iterator() = default;
        Iterator(T *table, Size offset)
            : table(table), offset(offset - 1) { operator++(); }

        ValueType &operator*()
        {
            RG_ASSERT(!table->IsEmpty(offset));
            return table->data[offset];
        }
        const ValueType &operator*() const
        {
            RG_ASSERT(!table->IsEmpty(offset));
            return table->data[offset];
        }

        Iterator &operator++()
        {
            RG_ASSERT(offset < table->capacity);
            while (++offset < table->capacity && table->IsEmpty(offset));
            return *this;
        }

        Iterator operator++(int)
        {
            Iterator ret = *this;
            ++(*this);
            return ret;
        }

        // Beware, in some cases a previous value may be seen again after this action
        void Remove()
        {
            table->Remove(&table->data[offset]);
            offset--;
        }

        bool operator==(const Iterator &other) const
            { return table == other.table && offset == other.offset; }
        bool operator!=(const Iterator &other) const { return !(*this == other); }
    };

    typedef Size value_type;
    typedef Iterator<HashTable> iterator_type;

    size_t *used = nullptr;
    ValueType *data = nullptr;
    Size count = 0;
    Size capacity = 0;
    Allocator *allocator = nullptr;

    HashTable() = default;
    HashTable(std::initializer_list<ValueType> l)
    {
        for (const ValueType &value: l) {
            Set(value);
        }
    }
    ~HashTable()
    {
        if constexpr(std::is_trivial<ValueType>::value) {
            count = 0;
            Rehash(0);
        } else {
            Clear();
        }
    }

    HashTable(HashTable &&other) { *this = std::move(other); }
    HashTable &operator=(HashTable &&other)
    {
        Clear();
        memmove_safe(this, &other, RG_SIZE(other));
        memset_safe(&other, 0, RG_SIZE(other));
        return *this;
    }
    HashTable(const HashTable &other) { *this = other; }
    HashTable &operator=(const HashTable &other)
    {
        Clear();
        for (const ValueType &value: other) {
            Set(value);
        }
        return *this;
    }

    void Clear()
    {
        for (Size i = 0; i < capacity; i++) {
            if (!IsEmpty(i)) {
                data[i].~ValueType();
            }
        }

        count = 0;
        Rehash(0);
    }

    void RemoveAll()
    {
        static_assert(!std::is_pointer<ValueType>::value);

        for (Size i = 0; i < capacity; i++) {
            if (!IsEmpty(i)) {
                data[i].~ValueType();
            }
        }

        count = 0;
        if (used) {
            size_t len = (size_t)(capacity + (RG_SIZE(size_t) * 8) - 1) / RG_SIZE(size_t);
            memset_safe(used, 0, len);
        }
    }

    Iterator<HashTable> begin() { return Iterator<HashTable>(this, 0); }
    Iterator<const HashTable> begin() const { return Iterator<const HashTable>(this, 0); }
    Iterator<HashTable> end() { return Iterator<HashTable>(this, capacity); }
    Iterator<const HashTable> end() const { return Iterator<const HashTable>(this, capacity); }

    template <typename T = KeyType>
    ValueType *Find(const T &key)
        { return (ValueType *)((const HashTable *)this)->Find(key); }
    template <typename T = KeyType>
    const ValueType *Find(const T &key) const
    {
        if (!capacity)
            return nullptr;

        uint64_t hash = Handler::HashKey(key);
        Size idx = HashToIndex(hash);
        return Find(&idx, key);
    }
    template <typename T = KeyType>
    ValueType FindValue(const T &key, const ValueType &default_value)
        { return (ValueType)((const HashTable *)this)->FindValue(key, default_value); }
    template <typename T = KeyType>
    const ValueType FindValue(const T &key, const ValueType &default_value) const
    {
        const ValueType *it = Find(key);
        return it ? *it : default_value;
    }

    ValueType *Set(const ValueType &value)
    {
        const KeyType &key = Handler::GetKey(value);

        bool inserted;
        ValueType *ptr = Insert(key, &inserted);

        *ptr = value;

        return ptr;
    }
    ValueType *SetDefault(const KeyType &key)
    {
        bool inserted;
        ValueType *ptr = Insert(key, &inserted);

        if (!inserted) {
            ptr->~ValueType();
        }
        new (ptr) ValueType();

        return ptr;
    }

    ValueType *TrySet(const ValueType &value, bool *out_inserted = nullptr)
    {
        const KeyType &key = Handler::GetKey(value);

        bool inserted;
        ValueType *ptr = Insert(key, &inserted);

        if (inserted) {
            *ptr = value;
        }

        if (out_inserted) {
            *out_inserted = inserted;
        }
        return ptr;
    }
    ValueType *TrySetDefault(const KeyType &key, bool *out_inserted = nullptr)
    {
        bool inserted;
        ValueType *ptr = Insert(key, &inserted);

        if (inserted) {
            new (ptr) ValueType();
        }

        if (out_inserted) {
            *out_inserted = inserted;
        }
        return ptr;
    }

    void Remove(ValueType *it)
    {
        if (!it)
            return;

        Size empty_idx = it - data;
        RG_ASSERT(!IsEmpty(empty_idx));

        it->~ValueType();
        count--;

        // Move following slots if needed
        {
            Size idx = (empty_idx + 1) & (capacity - 1);

            while (!IsEmpty(idx)) {
                Size real_idx = KeyToIndex(Handler::GetKey(data[idx]));

                if (TestNewSlot(real_idx, empty_idx)) {
                    memmove_safe(&data[empty_idx], &data[idx], RG_SIZE(*data));
                    empty_idx = idx;
                }

                idx = (idx + 1) & (capacity - 1);
            }
        }

        MarkEmpty(empty_idx);
        new (&data[empty_idx]) ValueType();
    }
    template <typename T = KeyType>
    void Remove(const T &key) { Remove(Find(key)); }

    void Trim()
    {
        if (count) {
            Size new_capacity = (Size)1 << (64 - CountLeadingZeros((uint64_t)count));

            if (new_capacity < RG_HASHTABLE_BASE_CAPACITY) {
                new_capacity = RG_HASHTABLE_BASE_CAPACITY;
            } else if (count > (double)new_capacity * RG_HASHTABLE_MAX_LOAD_FACTOR) {
                new_capacity *= 2;
            }

            Rehash(new_capacity);
        } else {
            Rehash(0);
        }
    }

    Size IsEmpty(Size idx) const { return IsEmpty(used, idx); }

private:
    template <typename T = KeyType>
    ValueType *Find(Size *idx, const T &key)
        { return (ValueType *)((const HashTable *)this)->Find(idx, key); }
    template <typename T = KeyType>
    const ValueType *Find(Size *idx, const T &key) const
    {
        if constexpr(std::is_pointer<ValueType>::value) {
            while (data[*idx]) {
                const KeyType &it_key = Handler::GetKey(data[*idx]);
                if (Handler::TestKeys(it_key, key))
                    return &data[*idx];
                *idx = (*idx + 1) & (capacity - 1);
            }
            return nullptr;
        } else {
            while (!IsEmpty(*idx)) {
                const KeyType &it_key = Handler::GetKey(data[*idx]);
                if (Handler::TestKeys(it_key, key))
                    return &data[*idx];
                *idx = (*idx + 1) & (capacity - 1);
            }
            return nullptr;
        }
    }

    ValueType *Insert(const KeyType &key, bool *out_inserted)
    {
        uint64_t hash = Handler::HashKey(key);

        if (capacity) {
            Size idx = HashToIndex(hash);
            ValueType *it = Find(&idx, key);
            if (!it) {
                if (count >= (Size)((double)capacity * RG_HASHTABLE_MAX_LOAD_FACTOR)) {
                    Rehash(capacity << 1);
                    idx = HashToIndex(hash);
                    while (!IsEmpty(idx)) {
                        idx = (idx + 1) & (capacity - 1);
                    }
                }
                count++;
                MarkUsed(idx);

                *out_inserted = true;
                return &data[idx];
            } else {
                *out_inserted = false;
                return it;
            }
        } else {
            Rehash(RG_HASHTABLE_BASE_CAPACITY);

            Size idx = HashToIndex(hash);
            count++;
            MarkUsed(idx);

            *out_inserted = true;
            return &data[idx];
        }
    }

    void Rehash(Size new_capacity)
    {
        if (new_capacity == capacity)
            return;
        RG_ASSERT(count <= new_capacity);

        size_t *old_used = used;
        ValueType *old_data = data;
        Size old_capacity = capacity;

        if (new_capacity) {
            used = (size_t *)AllocateRaw(allocator,
                                         (new_capacity + (RG_SIZE(size_t) * 8) - 1) / RG_SIZE(size_t),
                                         (int)AllocFlag::Zero);
            data = (ValueType *)AllocateRaw(allocator, new_capacity * RG_SIZE(ValueType));
            for (Size i = 0; i < new_capacity; i++) {
                new (&data[i]) ValueType();
            }
            capacity = new_capacity;

            for (Size i = 0; i < old_capacity; i++) {
                if (!IsEmpty(old_used, i)) {
                    Size new_idx = KeyToIndex(Handler::GetKey(old_data[i]));
                    while (!IsEmpty(new_idx)) {
                        new_idx = (new_idx + 1) & (capacity - 1);
                    }
                    MarkUsed(new_idx);
                    memmove_safe(&data[new_idx], &old_data[i], RG_SIZE(*data));
                }
            }
        } else {
            used = nullptr;
            data = nullptr;
            capacity = 0;
        }

        ReleaseRaw(allocator, old_used, (old_capacity + (RG_SIZE(size_t) * 8) - 1) / RG_SIZE(size_t));
        ReleaseRaw(allocator, old_data, old_capacity * RG_SIZE(ValueType));
    }

    void MarkUsed(Size idx)
    {
        used[idx / (RG_SIZE(size_t) * 8)] |= (1ull << (idx % (RG_SIZE(size_t) * 8)));
    }
    void MarkEmpty(Size idx)
    {
        used[idx / (RG_SIZE(size_t) * 8)] &= ~(1ull << (idx % (RG_SIZE(size_t) * 8)));
    }
    Size IsEmpty(size_t *used, Size idx) const
    {
        return !(used[idx / (RG_SIZE(size_t) * 8)] & (1ull << (idx % (RG_SIZE(size_t) * 8))));
    }

    Size HashToIndex(uint64_t hash) const
    {
        return (Size)(hash & (uint64_t)(capacity - 1));
    }
    Size KeyToIndex(const KeyType &key) const
    {
        uint64_t hash = Handler::HashKey(key);
        return HashToIndex(hash);
    }

    // XXX: Testing all slots each time (see Remove()) is slow and stupid
    bool TestNewSlot(Size idx, Size dest_idx) const
    {
        for (;;) {
            if (idx == dest_idx) {
                return true;
            } else if (IsEmpty(idx)) {
                return false;
            }

            idx = (idx + 1) & (capacity - 1);
        }
    }
};

template <typename T>
class HashTraits {
public:
    static uint64_t Hash(const T &key) { return key.Hash(); }
    static bool Test(const T &key1, const T &key2) { return key1 == key2; }
};

// Stole the Hash function from Thomas Wang (see here: https://gist.github.com/badboy/6267743)
#define DEFINE_INTEGER_HASH_TRAITS_32(Type) \
    template <> \
    class HashTraits<Type> { \
    public: \
        static uint64_t Hash(Type key) \
        { \
            uint32_t hash = (uint32_t)key; \
             \
            hash = (hash ^ 61) ^ (hash >> 16); \
            hash += hash << 3; \
            hash ^= hash >> 4; \
            hash *= 0x27D4EB2D; \
            hash ^= hash >> 15; \
             \
            return (uint64_t)hash; \
        } \
         \
        static bool Test(Type key1, Type key2) { return key1 == key2; } \
    }
#define DEFINE_INTEGER_HASH_TRAITS_64(Type) \
    template <> \
    class HashTraits<Type> { \
    public: \
        static uint64_t Hash(Type key) \
        { \
            uint64_t hash = (uint64_t)key; \
             \
            hash = (~hash) + (hash << 18); \
            hash ^= hash >> 31; \
            hash *= 21; \
            hash ^= hash >> 11; \
            hash += hash << 6; \
            hash ^= hash >> 22; \
             \
            return hash; \
        } \
         \
        static bool Test(Type key1, Type key2) { return key1 == key2; } \
    }

DEFINE_INTEGER_HASH_TRAITS_32(char);
DEFINE_INTEGER_HASH_TRAITS_32(unsigned char);
DEFINE_INTEGER_HASH_TRAITS_32(short);
DEFINE_INTEGER_HASH_TRAITS_32(unsigned short);
DEFINE_INTEGER_HASH_TRAITS_32(int);
DEFINE_INTEGER_HASH_TRAITS_32(unsigned int);
#ifdef __LP64__
    DEFINE_INTEGER_HASH_TRAITS_64(long);
    DEFINE_INTEGER_HASH_TRAITS_64(unsigned long);
#else
    DEFINE_INTEGER_HASH_TRAITS_32(long);
    DEFINE_INTEGER_HASH_TRAITS_32(unsigned long);
#endif
DEFINE_INTEGER_HASH_TRAITS_64(long long);
DEFINE_INTEGER_HASH_TRAITS_64(unsigned long long);
#if RG_SIZE_MAX == INT64_MAX
    DEFINE_INTEGER_HASH_TRAITS_64(void *);
    DEFINE_INTEGER_HASH_TRAITS_64(const void *);
#else
    DEFINE_INTEGER_HASH_TRAITS_32(void *);
    DEFINE_INTEGER_HASH_TRAITS_32(const void *);
#endif

#undef DEFINE_INTEGER_HASH_TRAITS_32
#undef DEFINE_INTEGER_HASH_TRAITS_64

template <>
class HashTraits<const char *> {
public:
    // FNV-1a
    static uint64_t Hash(Span<const char> key)
    {
        uint64_t hash = 0xCBF29CE484222325ull;
        for (char c: key) {
            hash ^= (uint64_t)c;
            hash *= 0x100000001B3ull;
        }

        return hash;
    }
    static uint64_t Hash(const char *key)
    {
        uint64_t hash = 0xCBF29CE484222325ull;
        for (Size i = 0; key[i]; i++) {
            hash ^= (uint64_t)key[i];
            hash *= 0x100000001B3ull;
        }

        return hash;
    }

    static bool Test(const char *key1, const char *key2) { return !strcmp(key1, key2); }
    static bool Test(const char *key1, Span<const char> key2) { return key2 == key1; }
};

template <>
class HashTraits<Span<const char>> {
public:
    // FNV-1a
    static uint64_t Hash(Span<const char> key)
    {
        uint64_t hash = 0xCBF29CE484222325ull;
        for (char c: key) {
            hash ^= (uint64_t)c;
            hash *= 0x100000001B3ull;
        }

        return hash;
    }
    static uint64_t Hash(const char *key)
    {
        uint64_t hash = 0xCBF29CE484222325ull;
        for (Size i = 0; key[i]; i++) {
            hash ^= (uint64_t)key[i];
            hash *= 0x100000001B3ull;
        }

        return hash;
    }

    static bool Test(Span<const char> key1, Span<const char> key2) { return key1 == key2; }
    static bool Test(Span<const char> key1, const char * key2) { return key1 == key2; }
};

#define RG_HASHTABLE_HANDLER_EX_N(Name, ValueType, KeyType, KeyMember, HashFunc, TestFunc) \
    class Name { \
    public: \
        static KeyType GetKey(const ValueType &value) \
            { return (KeyType)(value.KeyMember); } \
        static KeyType GetKey(const ValueType *value) \
            { return (KeyType)(value->KeyMember); } \
        template <typename TestKey> \
        static uint64_t HashKey(TestKey key) \
            { return HashFunc(key); } \
        template <typename TestKey> \
        static bool TestKeys(KeyType key1, TestKey key2) \
            { return TestFunc((key1), (key2)); } \
    }
#define RG_HASHTABLE_HANDLER_EX(ValueType, KeyType, KeyMember, HashFunc, TestFunc) \
    RG_HASHTABLE_HANDLER_EX_N(HashHandler, ValueType, KeyType, KeyMember, HashFunc, TestFunc)
#define RG_HASHTABLE_HANDLER(ValueType, KeyMember) \
    RG_HASHTABLE_HANDLER_EX(ValueType, decltype(ValueType::KeyMember), KeyMember, HashTraits<decltype(ValueType::KeyMember)>::Hash, HashTraits<decltype(ValueType::KeyMember)>::Test)
#define RG_HASHTABLE_HANDLER_N(Name, ValueType, KeyMember) \
    RG_HASHTABLE_HANDLER_EX_N(Name, ValueType, decltype(ValueType::KeyMember), KeyMember, HashTraits<decltype(ValueType::KeyMember)>::Hash, HashTraits<decltype(ValueType::KeyMember)>::Test)
#define RG_HASHTABLE_HANDLER_T(ValueType, KeyType, KeyMember) \
    RG_HASHTABLE_HANDLER_EX(ValueType, KeyType, KeyMember, HashTraits<KeyType>::Hash, HashTraits<KeyType>::Test)
#define RG_HASHTABLE_HANDLER_NT(Name, ValueType, KeyType, KeyMember) \
    RG_HASHTABLE_HANDLER_EX_N(Name, ValueType, KeyType, KeyMember, HashTraits<KeyType>::Hash, HashTraits<KeyType>::Test)

template <typename KeyType, typename ValueType>
class HashMap {
public:
    struct Bucket {
        KeyType key;
        ValueType value;

        RG_HASHTABLE_HANDLER(Bucket, key);
    };

    HashTable<KeyType, Bucket> table;

    HashMap() {}
    HashMap(std::initializer_list<Bucket> l)
    {
        for (const Bucket &bucket: l) {
            Set(bucket.key, bucket.value);
        }
    }

    void Clear() { table.Clear(); }
    void RemoveAll() { table.RemoveAll(); }

    template <typename T = KeyType>
    ValueType *Find(const T &key)
        { return (ValueType *)((const HashMap *)this)->Find(key); }
    template <typename T = KeyType>
    const ValueType *Find(const T &key) const
    {
        const Bucket *table_it = table.Find(key);
        return table_it ? &table_it->value : nullptr;
    }
    template <typename T = KeyType>
    ValueType FindValue(const T &key, const ValueType &default_value)
        { return (ValueType)((const HashMap *)this)->FindValue(key, default_value); }
    template <typename T = KeyType>
    const ValueType FindValue(const T &key, const ValueType &default_value) const
    {
        const ValueType *it = Find(key);
        return it ? *it : default_value;
    }

    ValueType *Set(const KeyType &key, const ValueType &value)
        { return &table.Set({ key, value })->value; }
    Bucket *SetDefault(const KeyType &key)
    {
        Bucket *table_it = table.SetDefault(key);
        table_it->key = key;
        return table_it;
    }

    ValueType *TrySet(const KeyType &key, const ValueType &value, bool *out_inserted = nullptr)
    {
        Bucket *ptr = table.TrySet({ key, value }, out_inserted);
        return &ptr->value;
    }
    Bucket *TrySetDefault(const KeyType &key, bool *out_inserted = nullptr)
    {
        bool inserted;
        Bucket *ptr = table.TrySetDefault(key, &inserted);

        if (inserted) {
            ptr->key = key;
        }

        if (out_inserted) {
            *out_inserted = inserted;
        }
        return ptr;
    }

    void Remove(ValueType *it)
    {
        if (!it)
            return;
        table.Remove((Bucket *)((uint8_t *)it - RG_OFFSET_OF(Bucket, value)));
    }
    void Remove(Bucket *it)
    {
        if (!it)
            return;
        table.Remove(it);
    }
    template <typename T = KeyType>
    void Remove(const KeyType &key) { Remove(Find(key)); }

    void Trim() { table.Trim(); }
};

template <typename ValueType>
class HashSet {
    class Handler {
    public:
        static ValueType GetKey(const ValueType &value) { return value; }
        static ValueType GetKey(const ValueType *value) { return *value; }
        static uint64_t HashKey(const ValueType &value)
            { return HashTraits<ValueType>::Hash(value); }
        static bool TestKeys(const ValueType &value1, const ValueType &value2)
            { return HashTraits<ValueType>::Test(value1, value2); }
    };

public:
    HashTable<ValueType, ValueType, Handler> table;

    HashSet() {}
    HashSet(std::initializer_list<ValueType> l)
    {
        for (const ValueType &value: l) {
            Set(value);
        }
    }

    void Clear() { table.Clear(); }
    void RemoveAll() { table.RemoveAll(); }

    template <typename T = ValueType>
    ValueType *Find(const T &value) { return table.Find(value); }
    template <typename T = ValueType>
    const ValueType *Find(const T &value) const { return table.Find(value); }
    template <typename T = ValueType>
    ValueType FindValue(const T &value, const ValueType &default_value)
        { return table.FindValue(value, default_value); }
    template <typename T = ValueType>
    const ValueType FindValue(const T &value, const ValueType &default_value) const
        { return table.FindValue(value, default_value); }

    ValueType *Set(const ValueType &value) { return table.Set(value); }
    ValueType *TrySet(const ValueType &value, bool *out_inserted = nullptr)
        { return table.TrySet(value, out_inserted); }

    void Remove(ValueType *it) { table.Remove(it); }
    template <typename T = ValueType>
    void Remove(const T &value) { Remove(Find(value)); }

    void Trim() { table.Trim(); }
};

// ------------------------------------------------------------------------
// Date
// ------------------------------------------------------------------------

union LocalDate {
    int32_t value;
    struct {
#ifdef RG_BIG_ENDIAN
        int16_t year;
        int8_t month;
        int8_t day;
#else
        int8_t day;
        int8_t month;
        int16_t year;
#endif
    } st;

    LocalDate() = default;
#ifdef RG_BIG_ENDIAN
    LocalDate(int16_t year, int8_t month, int8_t day)
        : st({ year, month, day }) { RG_ASSERT(IsValid()); }
#else
    LocalDate(int16_t year, int8_t month, int8_t day)
        : st({ day, month, year }) { RG_ASSERT(IsValid()); }
#endif

    static inline bool IsLeapYear(int16_t year)
    {
        return (year % 4 == 0 && year % 100 != 0) || year % 400 == 0;
    }
    static inline int8_t DaysInMonth(int16_t year, int8_t month)
    {
        static const int8_t DaysPerMonth[12] = { 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31 };
        return (int8_t)(DaysPerMonth[month - 1] + (month == 2 && IsLeapYear(year)));
    }

    static LocalDate Parse(Span<const char> date_str, unsigned int flags = RG_DEFAULT_PARSE_FLAGS,
                           Span<const char> *out_remaining = nullptr);
    static LocalDate FromJulianDays(int days);
    static LocalDate FromCalendarDate(int days) { return LocalDate::FromJulianDays(days + 2440588); }

    bool IsValid() const
    {
        if (st.year < -4712)
            return false;
        if (st.month < 1 || st.month > 12)
            return false;
        if (st.day < 1 || st.day > DaysInMonth(st.year, st.month))
            return false;

        return true;
    }

    bool operator==(LocalDate other) const { return value == other.value; }
    bool operator!=(LocalDate other) const { return value != other.value; }
    bool operator>(LocalDate other) const { return value > other.value; }
    bool operator>=(LocalDate other) const { return value >= other.value; }
    bool operator<(LocalDate other) const { return value < other.value; }
    bool operator<=(LocalDate other) const { return value <= other.value; }

    int ToJulianDays() const;
    int ToCalendarDate() const { return ToJulianDays() - 2440588; }

    int GetWeekDay() const;

    int operator-(LocalDate other) const
        { return ToJulianDays() - other.ToJulianDays(); }

    LocalDate operator+(int days) const
    {
        if (days < 5 && days > -5) {
            LocalDate date = *this;
            if (days > 0) {
                while (days--) {
                    ++date;
                }
            } else {
                while (days++) {
                    --date;
                }
            }
            return date;
        } else {
            return LocalDate::FromJulianDays(ToJulianDays() + days);
        }
    }
    // That'll fail with INT_MAX days but that's far more days than can
    // be represented as a date anyway
    LocalDate operator-(int days) const { return *this + (-days); }

    LocalDate &operator+=(int days) { *this = *this + days; return *this; }
    LocalDate &operator-=(int days) { *this = *this - days; return *this; }
    LocalDate &operator++();
    LocalDate operator++(int) { LocalDate date = *this; ++(*this); return date; }
    LocalDate &operator--();
    LocalDate operator--(int) { LocalDate date = *this; --(*this); return date; }

    uint64_t Hash() const { return HashTraits<int32_t>::Hash(value); }
};

// ------------------------------------------------------------------------
// Time
// ------------------------------------------------------------------------

int64_t GetUnixTime();
int64_t GetMonotonicTime();

#if defined(_MSC_VER) && !defined(_M_ARM64)
static inline int64_t GetClockCounter()
{
    return (int64_t)__rdtsc();
}
#elif defined(__i386__) || defined(__x86_64__)
static inline int64_t GetClockCounter()
{
    uint32_t counter_low, counter_high;
    __asm__ __volatile__ ("cpuid; rdtsc"
                          : "=a" (counter_low), "=d" (counter_high)
                          : : "%ebx", "%ecx");
    int64_t counter = ((int64_t)counter_high << 32) | counter_low;
    return counter;
}
#endif

struct TimeSpec {
    int16_t year;
    int8_t month;
    int8_t day;
    int8_t week_day; // 1 (monday) to 7 (sunday)

    int8_t hour;
    int8_t min;
    int8_t sec;
    int16_t msec;

    int16_t offset; // minutes
};

enum class TimeMode {
    Local,
    UTC
};
static const char *const TimeModeNames[] = {
    "Local",
    "UTC"
};

TimeSpec DecomposeTime(int64_t time, TimeMode mode = TimeMode::Local);

// ------------------------------------------------------------------------
// Format
// ------------------------------------------------------------------------

class StreamWriter;

enum class FmtType {
    Str1,
    Str2,
    Buffer,
    Char,
    Bool,
    Integer,
    Unsigned,
    Float,
    Double,
    Binary,
    Octal,
    BigHex,
    SmallHex,
    MemorySize,
    DiskSize,
    Date,
    TimeISO,
    TimeNice,
    Random,
    FlagNames,
    FlagOptions,
    Span
};

class FmtArg {
public:
    FmtType type;
    union {
        const char *str1;
        Span<const char> str2;
        char buf[32];
        char ch;
        bool b;
        int64_t i;
        uint64_t u;
        struct {
            float value;
            int min_prec;
            int max_prec;
        } f;
        struct {
            double value;
            int min_prec;
            int max_prec;
        } d;
        const void *ptr;
        LocalDate date;
        struct {
            TimeSpec spec;
            bool ms;
        } time;
        struct {
            Size len;
            const char *chars;
        } random;
        struct {
            uint64_t flags;
            union {
                Span<const char *const> names;
                Span<const struct OptionDesc> options;
            } u;
            const char *separator;
        } flags;

        struct {
            FmtType type;
            int type_len;
            const void *ptr;
            Size len;
            const char *separator;
        } span;
    } u;

    int repeat = 1;
    int pad_len = 0;
    char pad_char = 0;

    FmtArg() = default;
    FmtArg(std::nullptr_t) : type(FmtType::Str1) { u.str1 = "(null)"; }
    FmtArg(const char *str) : type(FmtType::Str1) { u.str1 = str ? str : "(null)"; }
    FmtArg(Span<const char> str) : type(FmtType::Str2) { u.str2 = str; }
    FmtArg(char c) : type(FmtType::Char) { u.ch = c; }
    FmtArg(bool b) : type(FmtType::Bool) { u.b = b; }
    FmtArg(unsigned char i)  : type(FmtType::Unsigned) { u.u = i; }
    FmtArg(short i) : type(FmtType::Integer) { u.i = i; }
    FmtArg(unsigned short i) : type(FmtType::Unsigned) { u.u = i; }
    FmtArg(int i) : type(FmtType::Integer) { u.i = i; }
    FmtArg(unsigned int i) : type(FmtType::Unsigned) { u.u = i; }
    FmtArg(long i) : type(FmtType::Integer) { u.i = i; }
    FmtArg(unsigned long i) : type(FmtType::Unsigned) { u.u = i; }
    FmtArg(long long i) : type(FmtType::Integer) { u.i = i; }
    FmtArg(unsigned long long i) : type(FmtType::Unsigned) { u.u = i; }
    FmtArg(float f) : type(FmtType::Float) { u.f = { f, 0, INT_MAX }; }
    FmtArg(double d) : type(FmtType::Double) { u.d = { d, 0, INT_MAX }; }
    FmtArg(const void *ptr) : type(FmtType::BigHex) { u.u = (uint64_t)ptr; }
    FmtArg(const LocalDate &date) : type(FmtType::Date) { u.date = date; }

    FmtArg &Repeat(int new_repeat) { repeat = new_repeat; return *this; }
    FmtArg &Pad(int len, char c = ' ') { pad_char = c; pad_len = len; return *this; }
    FmtArg &Pad0(int len) { return Pad(len, '0'); }
};

static inline FmtArg FmtBin(uint64_t u)
{
    FmtArg arg;
    arg.type = FmtType::Binary;
    arg.u.u = u;
    return arg;
}
static inline FmtArg FmtOctal(uint64_t u)
{
    FmtArg arg;
    arg.type = FmtType::Octal;
    arg.u.u = u;
    return arg;
}
static inline FmtArg FmtHex(uint64_t u, FmtType type = FmtType::BigHex)
{
    RG_ASSERT(type == FmtType::BigHex || type == FmtType::SmallHex);

    FmtArg arg;
    arg.type = type;
    arg.u.u = u;
    return arg;
}

static inline FmtArg FmtFloat(float f, int min_prec, int max_prec)
{
    FmtArg arg;
    arg.type = FmtType::Float;
    arg.u.f.value = f;
    arg.u.f.min_prec = min_prec;
    arg.u.f.max_prec = max_prec;
    return arg;
}
static inline FmtArg FmtFloat(float f, int prec) { return FmtFloat(f, prec, prec); }
static inline FmtArg FmtFloat(float f) { return FmtFloat(f, 0, INT_MAX); }

static inline FmtArg FmtDouble(double d, int min_prec, int max_prec)
{
    FmtArg arg;
    arg.type = FmtType::Double;
    arg.u.d.value = d;
    arg.u.d.min_prec = min_prec;
    arg.u.d.max_prec = max_prec;
    return arg;
}
static inline FmtArg FmtDouble(double d, int prec) { return FmtDouble(d, prec, prec); }
static inline FmtArg FmtDouble(double d) { return FmtDouble(d, 0, INT_MAX); }

static inline FmtArg FmtMemSize(int64_t size)
{
    FmtArg arg;
    arg.type = FmtType::MemorySize;
    arg.u.i = size;
    return arg;
}
static inline FmtArg FmtDiskSize(int64_t size)
{
    FmtArg arg;
    arg.type = FmtType::DiskSize;
    arg.u.i = size;
    return arg;
}

static inline FmtArg FmtTimeISO(TimeSpec spec, bool ms = false)
{
    FmtArg arg;
    arg.type = FmtType::TimeISO;
    arg.u.time.spec = spec;
    arg.u.time.ms = ms;
    return arg;
}

static inline FmtArg FmtTimeNice(TimeSpec spec, bool ms = false)
{
    FmtArg arg;
    arg.type = FmtType::TimeNice;
    arg.u.time.spec = spec;
    arg.u.time.ms = ms;
    return arg;
}

static inline FmtArg FmtRandom(Size len, const char *chars = nullptr)
{
    RG_ASSERT(len < 256);
    len = std::min(len, (Size)256);

    FmtArg arg;
    arg.type = FmtType::Random;
    arg.u.random.len = len;
    arg.u.random.chars = chars;
    return arg;
}

static inline FmtArg FmtFlags(uint64_t flags, Span<const char *const> names, const char *sep = ", ")
{
    FmtArg arg;
    arg.type = FmtType::FlagNames;
    arg.u.flags.flags = flags & ((1ull << names.len) - 1);
    arg.u.flags.u.names = names;
    arg.u.flags.separator = sep;
    return arg;
}

static inline FmtArg FmtFlags(uint64_t flags, Span<const struct OptionDesc> options, const char *sep = ", ")
{
    FmtArg arg;
    arg.type = FmtType::FlagOptions;
    arg.u.flags.flags = flags & ((1ull << options.len) - 1);
    arg.u.flags.u.options = options;
    arg.u.flags.separator = sep;
    return arg;
}

template <typename T>
FmtArg FmtSpan(Span<T> arr, FmtType type, const char *sep = ", ")
{
    FmtArg arg;
    arg.type = FmtType::Span;
    arg.u.span.type = type;
    arg.u.span.type_len = RG_SIZE(T);
    arg.u.span.ptr = (const void *)arr.ptr;
    arg.u.span.len = arr.len;
    arg.u.span.separator = sep;
    return arg;
}
template <typename T>
FmtArg FmtSpan(Span<T> arr, const char *sep = ", ") { return FmtSpan(arr, FmtArg(T()).type, sep); }
template <typename T, Size N>
FmtArg FmtSpan(T (&arr)[N], FmtType type, const char *sep = ", ") { return FmtSpan(MakeSpan(arr), type, sep); }
template <typename T, Size N>
FmtArg FmtSpan(T (&arr)[N], const char *sep = ", ") { return FmtSpan(MakeSpan(arr), sep); }

enum class LogLevel {
    Debug,
    Info,
    Warning,
    Error
};

Span<char> FmtFmt(const char *fmt, Span<const FmtArg> args, Span<char> out_buf);
Span<char> FmtFmt(const char *fmt, Span<const FmtArg> args, HeapArray<char> *out_buf);
Span<char> FmtFmt(const char *fmt, Span<const FmtArg> args, Allocator *alloc);
void PrintFmt(const char *fmt, Span<const FmtArg> args, StreamWriter *out_st);
void PrintFmt(const char *fmt, Span<const FmtArg> args, FILE *out_fp);
void PrintLnFmt(const char *fmt, Span<const FmtArg> args, StreamWriter *out_st);
void PrintLnFmt(const char *fmt, Span<const FmtArg> args, FILE *out_fp);

#define DEFINE_FMT_VARIANT(Name, Ret, Type) \
    static inline Ret Name(Type out, const char *fmt) \
    { \
        return Name##Fmt(fmt, {}, out); \
    } \
    template <typename... Args> \
    Ret Name(Type out, const char *fmt, Args... args) \
    { \
        const FmtArg fmt_args[] = { FmtArg(args)... }; \
        return Name##Fmt(fmt, fmt_args, out); \
    }

DEFINE_FMT_VARIANT(Fmt, Span<char>, Span<char>)
DEFINE_FMT_VARIANT(Fmt, Span<char>, HeapArray<char> *)
DEFINE_FMT_VARIANT(Fmt, Span<char>, Allocator *)
DEFINE_FMT_VARIANT(Print, void, StreamWriter *)
DEFINE_FMT_VARIANT(Print, void, FILE *)
DEFINE_FMT_VARIANT(PrintLn, void, StreamWriter *)
DEFINE_FMT_VARIANT(PrintLn, void, FILE *)

#undef DEFINE_FMT_VARIANT

// Print formatted strings to stdout
template <typename... Args>
void Print(const char *fmt, Args... args)
{
    Print(stdout, fmt, args...);
}
template <typename... Args>
void PrintLn(const char *fmt, Args... args)
{
    PrintLn(stdout, fmt, args...);
}

// PrintLn variants without format strings
void PrintLn(StreamWriter *out_st);
void PrintLn(FILE *out_fp);
void PrintLn();

// ------------------------------------------------------------------------
// Debug and errors
// ------------------------------------------------------------------------

typedef void LogFunc(LogLevel level, const char *ctx, const char *msg);
typedef void LogFilterFunc(LogLevel level, const char *ctx, const char *msg,
                           FunctionRef<LogFunc> func);

const char *GetQualifiedEnv(const char *name);
bool GetDebugFlag(const char *name);

void LogFmt(LogLevel level, const char *ctx, const char *fmt, Span<const FmtArg> args);

static inline void Log(LogLevel level, const char *ctx)
{
    LogFmt(level, ctx, "", {});
}
static inline void Log(LogLevel level, const char *ctx, const char *fmt)
{
    LogFmt(level, ctx, fmt, {});
}
template <typename... Args>
static inline void Log(LogLevel level, const char *ctx, const char *fmt, Args... args)
{
    const FmtArg fmt_args[] = { FmtArg(args)... };
    LogFmt(level, ctx, fmt, fmt_args);
}

// Shortcut log functions
#ifdef RG_DEBUG
    const char *DebugLogContext(const char *filename, int line);

    #define LogDebug(...) Log(LogLevel::Debug, DebugLogContext(__FILE__, __LINE__) __VA_OPT__(,) __VA_ARGS__)
    #define LogInfo(...) Log(LogLevel::Info, nullptr __VA_OPT__(,) __VA_ARGS__)
    #define LogWarning(...) Log(LogLevel::Warning, DebugLogContext(__FILE__, __LINE__) __VA_OPT__(,) __VA_ARGS__)
    #define LogError(...) Log(LogLevel::Error, DebugLogContext(__FILE__, __LINE__) __VA_OPT__(,) __VA_ARGS__)
#else
    template <typename... Args>
    static inline void LogDebug(Args...) {}
    template <typename... Args>
    static inline void LogInfo(Args... args) { Log(LogLevel::Info, nullptr, args...); }
    template <typename... Args>
    static inline void LogWarning(Args... args) { Log(LogLevel::Warning, "Warning: ", args...); }
    template <typename... Args>
    static inline void LogError(Args... args) { Log(LogLevel::Error, "Error: ", args...); }
#endif

void SetLogHandler(const std::function<LogFunc> &func);
void DefaultLogHandler(LogLevel level, const char *ctx, const char *msg);

void PushLogFilter(const std::function<LogFilterFunc> &func);
void PopLogFilter();

#ifdef _WIN32
bool RedirectLogToWindowsEvents(const char *name);
#endif

// ------------------------------------------------------------------------
// Strings
// ------------------------------------------------------------------------

bool CopyString(const char *str, Span<char> buf);
bool CopyString(Span<const char> str, Span<char> buf);
Span<char> DuplicateString(Span<const char> str, Allocator *alloc);

static inline bool IsAsciiAlpha(int c)
{
    return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z');
}
static inline bool IsAsciiDigit(int c)
{
    return (c >= '0' && c <= '9');
}
static inline bool IsAsciiAlphaOrDigit(int c)
{
    return IsAsciiAlpha(c) || IsAsciiDigit(c);
}
static inline bool IsAsciiWhite(int c)
{
    return c == ' ' || c == '\t' || c == '\v' ||
           c == '\n' || c == '\r' || c == '\f';
}

static inline char UpperAscii(int c)
{
    if (c >= 'a' && c <= 'z') {
        return (char)(c - 32);
    } else {
        return (char)c;
    }
}
static inline char LowerAscii(int c)
{
    if (c >= 'A' && c <= 'Z') {
        return (char)(c + 32);
    } else {
        return (char)c;
    }
}

static inline bool TestStr(Span<const char> str1, Span<const char> str2)
{
    if (str1.len != str2.len)
        return false;
    for (Size i = 0; i < str1.len; i++) {
        if (str1[i] != str2[i])
            return false;
    }
    return true;
}
static inline bool TestStr(Span<const char> str1, const char *str2)
{
    Size i;
    for (i = 0; i < str1.len && str2[i]; i++) {
        if (str1[i] != str2[i])
            return false;
    }
    return (i == str1.len) && !str2[i];
}
static inline bool TestStr(const char *str1, Span<const char> str2)
    { return TestStr(str2, str1); }
static inline bool TestStr(const char *str1, const char *str2)
    { return !strcmp(str1, str2); }

// Allow direct Span<const char> equality comparison
inline bool Span<const char>::operator==(Span<const char> other) const
    { return TestStr(*this, other); }
inline bool Span<const char>::operator==(const char *other) const
    { return TestStr(*this, other); }

// Case insensitive (ASCII) versions
static inline bool TestStrI(Span<const char> str1, Span<const char> str2)
{
    if (str1.len != str2.len)
        return false;
    for (Size i = 0; i < str1.len; i++) {
        if (LowerAscii(str1[i]) != LowerAscii(str2[i]))
            return false;
    }
    return true;
}
static inline bool TestStrI(Span<const char> str1, const char *str2)
{
    Size i;
    for (i = 0; i < str1.len && str2[i]; i++) {
        if (LowerAscii(str1[i]) != LowerAscii(str2[i]))
            return false;
    }
    return (i == str1.len) && !str2[i];
}
static inline bool TestStrI(const char *str1, Span<const char> str2)
    { return TestStrI(str2, str1); }
static inline bool TestStrI(const char *str1, const char *str2)
{
    Size i = 0;
    int delta;
    do {
        delta = LowerAscii(str1[i]) - LowerAscii(str2[i]);
    } while (str1[i++] && !delta);
    return !delta;
}

static inline int CmpStr(Span<const char> str1, Span<const char> str2)
{
    for (Size i = 0; i < str1.len && i < str2.len; i++) {
        int delta = str1[i] - str2[i];
        if (delta)
            return delta;
    }
    if (str1.len < str2.len) {
        return -str2[str1.len];
    } else if (str1.len > str2.len) {
        return str1[str2.len];
    } else {
        return 0;
    }
}
static inline int CmpStr(Span<const char> str1, const char *str2)
{
    Size i;
    for (i = 0; i < str1.len && str2[i]; i++) {
        int delta = str1[i] - str2[i];
        if (delta)
            return delta;
    }
    if (str1.len == i) {
        return -str2[i];
    } else {
        return str1[i];
    }
}
static inline int CmpStr(const char *str1, Span<const char> str2)
    { return -CmpStr(str2, str1); }
static inline int CmpStr(const char *str1, const char *str2)
    { return strcmp(str1, str2); }

static inline bool StartsWith(Span<const char> str, Span<const char> prefix)
{
    Size i = 0;
    while (i < str.len && i < prefix.len) {
        if (str[i] != prefix[i])
            return false;
        i++;
    }

    return (i == prefix.len);
}
static inline bool StartsWith(Span<const char> str, const char *prefix)
{
    Size i = 0;
    while (i < str.len && prefix[i]) {
        if (str[i] != prefix[i])
            return false;
        i++;
    }

    return !prefix[i];
}
static inline bool StartsWith(const char *str, const char *prefix)
{
    Size i = 0;
    while (str[i] && prefix[i]) {
        if (str[i] != prefix[i])
            return false;
        i++;
    }

    return !prefix[i];
}

static inline bool EndsWith(Span<const char> str, const char *suffix)
{
    Size i = str.len - 1;
    Size j = (Size)strlen(suffix) - 1;
    while (i >= 0 && j >= 0) {
        if (str[i] != suffix[j])
            return false;

        i--;
        j--;
    }

    return j < 0;
}

static inline Size FindStr(Span<const char> str, Span<const char> needle)
{
    if (!needle.len)
        return 0;
    if (needle.len > str.len)
        return -1;

    Size end = str.len - needle.len;

    for (Size i = 0; i <= end; i++) {
        if (!memcmp(str.ptr + i, needle.ptr, (size_t)needle.len))
            return i; 
    }

    return -1;
}
static inline Size FindStr(const char *str, const char *needle)
{
    const char *ret = strstr(str, needle);
    return ret ? ret - str : -1;
}

static inline Span<char> SplitStr(Span<char> str, char split_char, Span<char> *out_remainder = nullptr)
{
    Size part_len = 0;
    while (part_len < str.len) {
        if (str[part_len] == split_char) {
            if (out_remainder) {
                *out_remainder = str.Take(part_len + 1, str.len - part_len - 1);
            }
            return str.Take(0, part_len);
        }
        part_len++;
    }

    if (out_remainder) {
        *out_remainder = str.Take(str.len, 0);
    }
    return str;
}
static inline Span<char> SplitStr(char *str, char split_char, char **out_remainder = nullptr)
{
    Size part_len = 0;
    while (str[part_len]) {
        if (str[part_len] == split_char) {
            if (out_remainder) {
                *out_remainder = str + part_len + 1;
            }
            return MakeSpan(str, part_len);
        }
        part_len++;
    }

    if (out_remainder) {
        *out_remainder = str + part_len;
    }
    return MakeSpan(str, part_len);
}
static inline Span<const char> SplitStr(Span<const char> str, char split_char, Span<const char> *out_remainder = nullptr)
    { return SplitStr(MakeSpan((char *)str.ptr, str.len), split_char, (Span<char> *)out_remainder); }
static inline Span<const char> SplitStr(const char *str, char split_char, const char **out_remainder = nullptr)
    { return SplitStr((char *)str, split_char, (char **)out_remainder); }

static inline Span<char> SplitStr(Span<char> str, const char *split_str, Span<char> *out_remainder = nullptr)
{
    RG_ASSERT(split_str[0]);

    Size part_len = 0;
    while (part_len < str.len) {
        if (StartsWith(str.Take(part_len, str.len - part_len), split_str)) {
            if (out_remainder) {
                Size split_len = strlen(split_str);
                *out_remainder = str.Take(part_len + split_len, str.len - part_len - split_len);
            }
            return str.Take(0, part_len);
        }
        part_len++;
    }

    if (out_remainder) {
        *out_remainder = str.Take(str.len, 0);
    }
    return str;
}
static inline Span<char> SplitStr(char *str, const char *split_str, char **out_remainder = nullptr)
{
    RG_ASSERT(split_str[0]);

    Size part_len = 0;
    while (str[part_len]) {
        if (StartsWith(str + part_len, split_str)) {
            if (out_remainder) {
                Size split_len = strlen(split_str);
                *out_remainder = str + part_len + split_len;
            }
            return MakeSpan(str, part_len);
        }
        part_len++;
    }

    if (out_remainder) {
        *out_remainder = str + part_len;
    }
    return MakeSpan(str, part_len);
}
static inline Span<const char> SplitStr(Span<const char> str, const char *split_str, Span<const char> *out_remainder = nullptr)
    { return SplitStr(MakeSpan((char *)str.ptr, str.len), split_str, (Span<char> *)out_remainder); }
static inline Span<const char> SplitStr(const char *str, const char *split_str, const char **out_remainder = nullptr)
    { return SplitStr((char *)str, split_str, (char **)out_remainder); }

static inline Span<char> SplitStrLine(Span<char> str, Span<char> *out_remainder = nullptr)
{
    Span<char> part = SplitStr(str, '\n', out_remainder);
    if (part.len < str.len && part.len && part[part.len - 1] == '\r') {
        part.len--;
    }
    return part;
}
static inline Span<char> SplitStrLine(char *str, char **out_remainder = nullptr)
{
    Span<char> part = SplitStr(str, '\n', out_remainder);
    if (str[part.len] && part.len && part[part.len - 1] == '\r') {
        part.len--;
    }
    return part;
}
static inline Span<const char> SplitStrLine(Span<const char> str, Span<const char> *out_remainder = nullptr)
    { return SplitStrLine(MakeSpan((char *)str.ptr, str.len), (Span<char> *)out_remainder); }
static inline Span<const char> SplitStrLine(const char *str, const char **out_remainder = nullptr)
    { return SplitStrLine((char *)str, (char **)out_remainder); }

static inline Span<char> SplitStrAny(Span<char> str, const char *split_chars, Span<char> *out_remainder = nullptr)
{
    Bitset<256> split_mask;
    for (Size i = 0; split_chars[i]; i++) {
        uint8_t c = (uint8_t)split_chars[i];
        split_mask.Set(c);
    }

    Size part_len = 0;
    while (part_len < str.len) {
        uint8_t c = (uint8_t)str[part_len];

        if (split_mask.Test(c)) {
            if (out_remainder) {
                *out_remainder = str.Take(part_len + 1, str.len - part_len - 1);
            }
            return str.Take(0, part_len);
        }
        part_len++;
    }

    if (out_remainder) {
        *out_remainder = str.Take(str.len, 0);
    }
    return str;
}
static inline Span<char> SplitStrAny(char *str, const char *split_chars, char **out_remainder = nullptr)
{
    Bitset<256> split_mask;
    for (Size i = 0; split_chars[i]; i++) {
        uint8_t c = (uint8_t)split_chars[i];
        split_mask.Set(c);
    }

    Size part_len = 0;
    while (str[part_len]) {
        uint8_t c = (uint8_t)str[part_len];

        if (split_mask.Test(c)) {
            if (out_remainder) {
                *out_remainder = str + part_len + 1;
            }
            return MakeSpan(str, part_len);
        }
        part_len++;
    }

    if (out_remainder) {
        *out_remainder = str + part_len;
    }
    return MakeSpan(str, part_len);
}
static inline Span<const char> SplitStrAny(Span<const char> str, const char *split_chars, Span<const char> *out_remainder = nullptr)
    { return SplitStrAny(MakeSpan((char *)str.ptr, str.len), split_chars, (Span<char> *)out_remainder); }
static inline Span<const char> SplitStrAny(const char *str, const char *split_chars, const char **out_remainder = nullptr)
    { return SplitStrAny((char *)str, split_chars, (char **)out_remainder); }

static inline Span<const char> SplitStrReverse(Span<const char> str, char split_char,
                                               Span<const char> *out_remainder = nullptr)
{
    Size remainder_len = str.len - 1;
    while (remainder_len >= 0) {
        if (str[remainder_len] == split_char) {
            if (out_remainder) {
                *out_remainder = str.Take(0, remainder_len);
            }
            return str.Take(remainder_len + 1, str.len - remainder_len - 1);
        }
        remainder_len--;
    }

    if (out_remainder) {
        *out_remainder = str.Take(0, 0);
    }
    return str;
}
static inline Span<const char> SplitStrReverse(const char *str, char split_char,
                                               Span<const char> *out_remainder = nullptr)
    { return SplitStrReverse(MakeSpan(str, strlen(str)), split_char, out_remainder); }

static inline Span<const char> SplitStrReverseAny(Span<const char> str, const char *split_chars,
                                                  Span<const char> *out_remainder = nullptr)
{
    Bitset<256> split_mask;
    for (Size i = 0; split_chars[i]; i++) {
        uint8_t c = (uint8_t)split_chars[i];
        split_mask.Set(c);
    }

    Size remainder_len = str.len - 1;
    while (remainder_len >= 0) {
        uint8_t c = (uint8_t)str[remainder_len];

        if (split_mask.Test(c)) {
            if (out_remainder) {
                *out_remainder = str.Take(0, remainder_len);
            }
            return str.Take(remainder_len + 1, str.len - remainder_len - 1);
        }
        remainder_len--;
    }

    if (out_remainder) {
        *out_remainder = str.Take(0, 0);
    }
    return str;
}
static inline Span<const char> SplitStrReverseAny(const char *str, const char *split_chars,
                                                  Span<const char> *out_remainder = nullptr)
    { return SplitStrReverseAny(MakeSpan(str, strlen(str)), split_chars, out_remainder); }

static inline Span<char> TrimStrLeft(Span<char> str, const char *trim_chars = " \t\r\n")
{
    while (str.len && strchr(trim_chars, str[0]) && str[0]) {
        str.ptr++;
        str.len--;
    }

    return str;
}
static inline Span<char> TrimStrRight(Span<char> str, const char *trim_chars = " \t\r\n")
{
    while (str.len && strchr(trim_chars, str[str.len - 1]) && str[str.len - 1]) {
        str.len--;
    }

    return str;
}
static inline Span<char> TrimStr(Span<char> str, const char *trim_chars = " \t\r\n")
{
    str = TrimStrRight(str, trim_chars);
    str = TrimStrLeft(str, trim_chars);

    return str;
}
static inline Span<const char> TrimStrLeft(Span<const char> str, const char *trim_chars = " \t\r\n")
    { return TrimStrLeft(MakeSpan((char *)str.ptr, str.len), trim_chars); }
static inline Span<const char> TrimStrRight(Span<const char> str, const char *trim_chars = " \t\r\n")
    { return TrimStrRight(MakeSpan((char *)str.ptr, str.len), trim_chars); }
static inline Span<const char> TrimStr(Span<const char> str, const char *trim_chars = " \t\r\n")
    { return TrimStr(MakeSpan((char *)str.ptr, str.len), trim_chars); }

template <typename T>
bool ParseInt(Span<const char> str, T *out_value, unsigned int flags = RG_DEFAULT_PARSE_FLAGS,
              Span<const char> *out_remaining = nullptr)
{
    if (!str.len) [[unlikely]] {
        if (flags & (int)ParseFlag::Log) {
            LogError("Cannot convert empty string to integer");
        }
        return false;
    }

    uint64_t value = 0;

    Size pos = 0;
    uint64_t neg = 0;
    if (str.len >= 2) {
        if (std::numeric_limits<T>::min() < 0 && str[0] == '-') {
            pos = 1;
            neg = UINT64_MAX;
        } else if (str[0] == '+') {
            pos = 1;
        }
    }

    for (; pos < str.len; pos++) {
        unsigned int digit = (unsigned int)(str[pos] - '0');
        if (digit > 9) [[unlikely]] {
            if (!pos || flags & (int)ParseFlag::End) {
                if (flags & (int)ParseFlag::Log) {
                    LogError("Malformed integer number '%1'", str);
                }
                return false;
            } else {
                break;
            }
        }

        uint64_t new_value = (value * 10) + digit;
        if (new_value < value) [[unlikely]]
            goto overflow;
        value = new_value;
    }
    if (value > (uint64_t)std::numeric_limits<T>::max()) [[unlikely]]
        goto overflow;
    value = ((value ^ neg) - neg);

    if (out_remaining) {
        *out_remaining = str.Take(pos, str.len - pos);
    }
    *out_value = (T)value;
    return true;

overflow:
    if (flags & (int)ParseFlag::Log) {
        LogError("Integer overflow for number '%1' (max = %2)", str,
                std::numeric_limits<T>::max());
    }
    return false;
}

bool ParseBool(Span<const char> str, bool *out_value, unsigned int flags = RG_DEFAULT_PARSE_FLAGS,
               Span<const char> *out_remaining = nullptr);

bool ParseSize(Span<const char> str, int64_t *out_size, unsigned int flags = RG_DEFAULT_PARSE_FLAGS,
               Span<const char> *out_remaining = nullptr);
#if RG_SIZE_MAX < INT64_MAX
static inline bool ParseSize(Span<const char> str, Size *out_size,
                             unsigned int flags = RG_DEFAULT_PARSE_FLAGS, Span<const char> *out_remaining = nullptr)
{
    int64_t size = 0;
    if (!ParseSize(str, &size, flags, out_remaining))
        return false;

    if (size > RG_SIZE_MAX) [[unlikely]] {
        if (flags & (int)ParseFlag::Log) {
            LogError("Size value is too high");
        }
        return false;
    }

    *out_size = (Size)size;
    return true;
}
#endif

bool ParseDuration(Span<const char> str, int64_t *out_duration, unsigned int flags = RG_DEFAULT_PARSE_FLAGS,
                   Span<const char> *out_remaining = nullptr);

static inline Size EncodeUtf8(int32_t c, char out_buf[4])
{
    if (c < 0x80) {
        out_buf[0] = (char)c;
        return 1;
    } else if (c < 0x800) {
        out_buf[0] = (char)(0xC0 | (c >> 6));
        out_buf[1] = (char)(0x80 | (c & 0x3F));
        return 2;
    } else if (c >= 0xD800 && c < 0xE000) {
        return 0;
    } else if (c < 0x10000) {
        out_buf[0] = (char)(0xE0 | (c >> 12));
        out_buf[1] = (char)(0x80 | ((c >> 6) & 0x3F));
        out_buf[2] = (char)(0x80 | (c & 0x3F));
        return 3;
    } else if (c < 0x110000) {
        out_buf[0] = (char)(0xF0 | (c >> 18));
        out_buf[1] = (char)(0x80 | ((c >> 12) & 0x3F));
        out_buf[2] = (char)(0x80 | ((c >> 6) & 0x3F));
        out_buf[3] = (char)(0x80 | (c & 0x3F));
        return 4;
    } else {
        return 0;
    }
}

static inline int CountUtf8Bytes(char c)
{
    int ones = CountLeadingZeros((uint32_t)~c << 24);
    return std::min(std::max(ones, 1), 4);
}

static inline Size DecodeUtf8(const char *str, int32_t *out_c)
{
    RG_ASSERT(str[0]);

    const uint8_t *ptr = (const uint8_t *)str;

    if (ptr[0] < 0x80) {
        *out_c = ptr[0];
        return 1;
    } else if (ptr[0] - 0xC2 > 0xF4 - 0xC2) [[unlikely]] {
        return 0;
    } else if (ptr[1]) [[likely]] {
        if (ptr[0] < 0xE0 && (ptr[1] & 0xC0) == 0x80) {
            *out_c = ((ptr[0] & 0x1F) << 6) | (ptr[1] & 0x3F);
            return 2;
        } else if (ptr[2]) [[likely]] {
            if (ptr[0] < 0xF0 && (ptr[1] & 0xC0) == 0x80 &&
                                 (ptr[2] & 0xC0) == 0x80) {
                *out_c = ((ptr[0] & 0xF) << 12) | ((ptr[1] & 0x3F) << 6) | (ptr[2] & 0x3F);
                return 3;
            } else if (ptr[3]) [[likely]] {
                if ((ptr[1] & 0xC0) == 0x80 &&
                        (ptr[2] & 0xC0) == 0x80 &&
                        (ptr[3] & 0xC0) == 0x80) {
                    *out_c = ((ptr[0] & 0x7) << 18) | ((ptr[1] & 0x3F) << 12) | ((ptr[2] & 0x3F) << 6) | (ptr[3] & 0x3F);
                    return 4;
                }
            }
        }
    }

    return 0;
}

static inline Size DecodeUtf8(Span<const char> str, Size offset, int32_t *out_c)
{
    RG_ASSERT(offset < str.len);

    const uint8_t *ptr = (const uint8_t *)(str.ptr + offset);
    Size available = str.len - offset;

    if (ptr[0] < 0x80) {
        *out_c = ptr[0];
        return 1;
    } else if (ptr[0] - 0xC2 > 0xF4 - 0xC2) [[unlikely]] {
        return 0;
    } else if (ptr[0] < 0xE0 && available >= 2 && (ptr[1] & 0xC0) == 0x80) {
        *out_c = ((ptr[0] & 0x1F) << 6) | (ptr[1] & 0x3F);
        return 2;
    } else if (ptr[0] < 0xF0 && available >= 3 && (ptr[1] & 0xC0) == 0x80 &&
                                                  (ptr[2] & 0xC0) == 0x80) {
        *out_c = ((ptr[0] & 0xF) << 12) | ((ptr[1] & 0x3F) << 6) | (ptr[2] & 0x3F);
        return 3;
    } else if (available >= 4 && (ptr[1] & 0xC0) == 0x80 &&
                                 (ptr[2] & 0xC0) == 0x80 &&
                                 (ptr[3] & 0xC0) == 0x80) {
        *out_c = ((ptr[0] & 0x7) << 18) | ((ptr[1] & 0x3F) << 12) | ((ptr[2] & 0x3F) << 6) | (ptr[3] & 0x3F);
        return 4;
    } else {
        return 0;
    }
}

// ------------------------------------------------------------------------
// System
// ------------------------------------------------------------------------

#ifdef _WIN32
    #define RG_PATH_SEPARATORS "\\/"
    #define RG_PATH_DELIMITER ';'
    #define RG_EXECUTABLE_EXTENSION ".exe"
    #define RG_SHARED_LIBRARY_EXTENSION ".dll"
#else
    #define RG_PATH_SEPARATORS "/"
    #define RG_PATH_DELIMITER ':'
    #define RG_EXECUTABLE_EXTENSION ""
    #define RG_SHARED_LIBRARY_EXTENSION ".so"
#endif

#ifdef _WIN32
bool IsWin32Utf8();
Size ConvertUtf8ToWin32Wide(Span<const char> str, Span<wchar_t> out_str_w);
Size ConvertWin32WideToUtf8(const wchar_t *str_w, Span<char> out_str);
char *GetWin32ErrorString(uint32_t error_code = UINT32_MAX);
#endif

static inline bool IsPathSeparator(char c)
{
#ifdef _WIN32
    return c == '/' || c == '\\';
#else
    return c == '/';
#endif
}

enum class CompressionType {
    None,
    Zlib,
    Gzip,
    Brotli,
    LZ4
};
static const char *const CompressionTypeNames[] = {
    "None",
    "Zlib",
    "Gzip",
    "Brotli",
    "LZ4"
};
static const char *const CompressionTypeExtensions[] = {
    nullptr,
    nullptr,
    ".gz",
    ".br",
    ".lz4"
};

Span<const char> GetPathDirectory(Span<const char> filename);
Span<const char> GetPathExtension(Span<const char> filename,
                                  CompressionType *out_compression_type = nullptr);
CompressionType GetPathCompression(Span<const char> filename);

Span<char> NormalizePath(Span<const char> path, Span<const char> root_directory, Allocator *alloc);
static inline Span<char> NormalizePath(Span<const char> path, Allocator *alloc)
    { return NormalizePath(path, {}, alloc); }

bool PathIsAbsolute(const char *path);
bool PathIsAbsolute(Span<const char> path);
bool PathContainsDotDot(const char *path);

enum class StatFlag {
    IgnoreMissing = 1 << 0,
    FollowSymlink = 1 << 1
};

enum class FileType {
    Directory,
    File,
    Link,
    Device,
    Pipe,
    Socket
};
static const char *const FileTypeNames[] = {
    "Directory",
    "File",
    "Link",
    "Device",
    "Pipe",
    "Socket"
};

struct FileInfo {
    FileType type;

    int64_t size;
    int64_t mtime;
    int64_t btime;
    unsigned int mode;

    uint32_t uid;
    uint32_t gid;
};

enum class StatResult {
    Success,

    MissingPath,
    AccessDenied,
    OtherError
};

StatResult StatFile(const char *filename, unsigned int flags, FileInfo *out_info);
static inline StatResult StatFile(const char *filename, FileInfo *out_info)
    { return StatFile(filename, 0, out_info); }

enum class RenameFlag {
    Overwrite = 1 << 0,
    Sync = 1 << 1
};

// Sync failures are logged but not reported as errors (function returns true)
bool RenameFile(const char *src_filename, const char *dest_filename, unsigned int flags);

enum class EnumResult {
    Success,

    MissingPath,
    AccessDenied,
    PartialEnum,
    CallbackFail,
    OtherError
};

EnumResult EnumerateDirectory(const char *dirname, const char *filter, Size max_files,
                              FunctionRef<bool(const char *, FileType)> func);
bool EnumerateFiles(const char *dirname, const char *filter, Size max_depth, Size max_files,
                    Allocator *str_alloc, HeapArray<const char *> *out_files);
bool IsDirectoryEmpty(const char *dirname);

bool TestFile(const char *filename);
bool TestFile(const char *filename, FileType type);
bool IsDirectory(const char *filename);

bool MatchPathName(const char *path, const char *spec);
bool MatchPathSpec(const char *path, const char *spec);

bool FindExecutableInPath(const char *path, const char *name,
                          Allocator *alloc = nullptr, const char **out_path = nullptr);
bool FindExecutableInPath(const char *name, Allocator *alloc = nullptr,
                          const char **out_path = nullptr);

bool SetWorkingDirectory(const char *directory);
const char *GetWorkingDirectory();

const char *GetApplicationExecutable(); // Can be NULL (EmSDK)
const char *GetApplicationDirectory(); // Can be NULL (EmSDK)

bool MakeDirectory(const char *directory, bool error_if_exists = true);
bool MakeDirectoryRec(Span<const char> directory);
bool UnlinkDirectory(const char *directory, bool error_if_missing = false);
bool UnlinkFile(const char *filename, bool error_if_missing = false);
bool EnsureDirectoryExists(const char *filename);

enum class OpenFlag {
    Read = 1 << 0,
    Write = 1 << 1,
    Append = 1 << 2,

    Directory = 1 << 3,
    Exists = 1 << 4,
    Exclusive = 1 << 5
};

enum class OpenResult {
    Success = 0,

    MissingPath = 1 << 0,
    FileExists = 1 << 1,
    AccessDenied = 1 << 2,
    OtherError = 1 << 3
};

OpenResult OpenDescriptor(const char *filename, unsigned int flags, unsigned int silent, int *out_fd);
OpenResult OpenFile(const char *filename, unsigned int flags, unsigned int silent, FILE **out_fp);

static inline OpenResult OpenDescriptor(const char *filename, unsigned int flags, int *out_fd)
    { return OpenDescriptor(filename, flags, 0, out_fd); }
static inline OpenResult OpenFile(const char *filename, unsigned int flags, FILE **out_fp)
    { return OpenFile(filename, flags, 0, out_fp); }
static inline int OpenDescriptor(const char *filename, unsigned int flags)
{
    int fd = -1;
    if (OpenDescriptor(filename, flags, &fd) != OpenResult::Success)
        return -1;
    return fd;
}
static inline FILE *OpenFile(const char *filename, unsigned int flags)
{
    FILE *fp;
    if (OpenFile(filename, flags, &fp) != OpenResult::Success)
        return nullptr;
    return fp;
}

bool FlushFile(int fd, const char *filename);
bool FlushFile(FILE *fp, const char *filename);

bool FileIsVt100(FILE *fp);

#ifdef _WIN32
enum class PipeMode {
    Byte,
    Message
};

bool CreateOverlappedPipe(bool overlap0, bool overlap1, PipeMode mode, void *out_handles[2]); // HANDLE
void CloseHandleSafe(void **handle_ptr); // HANDLE
#else
void SetSignalHandler(int signal, void (*func)(int), struct sigaction *prev = nullptr);

bool CreatePipe(int pfd[2]);
void CloseDescriptorSafe(int *fd_ptr);
#endif

struct ExecuteInfo {
    struct KeyValue {
        const char *key;
        const char *value;
    };

    const char *work_dir = nullptr;

    bool reset_env = false;
    Span<const KeyValue> env_variables = {};
};

bool ExecuteCommandLine(const char *cmd_line, const ExecuteInfo &info,
                        FunctionRef<Span<const uint8_t>()> in_func,
                        FunctionRef<void(Span<uint8_t> buf)> out_func, int *out_code);
bool ExecuteCommandLine(const char *cmd_line, const ExecuteInfo &info,
                        Span<const uint8_t> in_buf, Size max_len,
                        HeapArray<uint8_t> *out_buf, int *out_code);

// Simple variants
static inline bool ExecuteCommandLine(const char *cmd_line, const ExecuteInfo &info,int *out_code)
    { return ExecuteCommandLine(cmd_line, info, {}, {}, out_code); }
static inline bool ExecuteCommandLine(const char *cmd_line, const ExecuteInfo &info,
                                      Span<const uint8_t> in_buf,
                                      FunctionRef<void(Span<uint8_t> buf)> out_func, int *out_code)
{
    const auto read_once = [&]() {
        Span<const uint8_t> buf = in_buf;
        in_buf = {};
        return buf;
    };

    return ExecuteCommandLine(cmd_line, info, read_once, out_func, out_code);
}

// Char variants
static inline bool ExecuteCommandLine(const char *cmd_line, const ExecuteInfo &info,
                                      Span<const char> in_buf,
                                      FunctionRef<void(Span<char> buf)> out_func, int *out_code)
{
    const auto write = [&](Span<uint8_t> buf) { out_func(buf.As<char>()); };
    return ExecuteCommandLine(cmd_line, info, in_buf.As<const uint8_t>(), write, out_code);
}
static inline bool ExecuteCommandLine(const char *cmd_line, const ExecuteInfo &info,
                                      Span<const char> in_buf, Size max_len,
                                      HeapArray<char> *out_buf, int *out_code)
{
    return ExecuteCommandLine(cmd_line, info, in_buf.As<const uint8_t>(), max_len,
                              (HeapArray<uint8_t> *)out_buf, out_code);
}

Size ReadCommandOutput(const char *cmd_line, Span<char> out_output);
bool ReadCommandOutput(const char *cmd_line, HeapArray<char> *out_output);

void WaitDelay(int64_t delay);

enum class WaitForResult {
    Interrupt,
    Message,
    Timeout
};

// After WaitForInterrupt() has been called once (even with timeout 0), a few
// signals (such as SIGINT, SIGHUP) and their Windows equivalent will be permanently ignored.
// Beware, on Unix platforms, this may not work correctly if not called from the main thread.
WaitForResult WaitForInterrupt(int64_t timeout = -1);
void SignalWaitFor();

int GetCoreCount();

#ifndef _WIN32
bool DropRootIdentity();
#endif
#ifdef __linux__
bool NotifySystemd();
#endif

#ifndef _WIN32
    #define RG_POSIX_RESTART_EINTR(CallCode, ErrorCond) \
        ([&]() { \
            decltype(CallCode) ret; \
            while ((ret = (CallCode)) ErrorCond && errno == EINTR); \
            return ret; \
        })()
#endif

void InitRG();
int Main(int argc, char **argv);

static inline int RunApp(int argc, char **argv)
{
    InitRG();
    return Main(argc, argv);
}

// ------------------------------------------------------------------------
// Standard paths
// ------------------------------------------------------------------------

const char *GetUserConfigPath(const char *name, Allocator *alloc); // Can return NULL
const char *GetUserCachePath(const char *name, Allocator *alloc); // Can return NULL
const char *GetSystemConfigPath(const char *name, Allocator *alloc);
const char *GetTemporaryDirectory();

const char *FindConfigFile(Span<const char *const> names, Allocator *alloc,
                           LocalArray<const char *, 4> *out_possibilities = nullptr);

const char *CreateUniqueFile(Span<const char> directory, const char *prefix, const char *extension,
                             Allocator *alloc, FILE **out_fp = nullptr);
const char *CreateUniqueDirectory(Span<const char> directory, const char *prefix, Allocator *alloc);

// ------------------------------------------------------------------------
// Random
// ------------------------------------------------------------------------

void ZeroMemorySafe(void *ptr, Size len);
void FillRandomSafe(void *buf, Size len);
static inline void FillRandomSafe(Span<uint8_t> buf) { FillRandomSafe(buf.ptr, buf.len); }
int GetRandomIntSafe(int min, int max);

class FastRandom {
    uint64_t state[4];

public:
    FastRandom();
    FastRandom(uint64_t seed);

    void Fill(void *buf, Size len);
    void Fill(Span<uint8_t> buf) { Fill(buf.ptr, buf.len); }

    int GetInt(int min, int max);

private:
    uint64_t Next();
};

template <int Min = 0, int Max = INT_MAX>
class FastRandomInt {
    static_assert(Min >= 0);
    static_assert(Max > Min);

    FastRandom rng;

public:
    typedef int result_type;

    static constexpr int min() { return Min; }
    static constexpr int max() { return Max; }

    int operator()() { return rng.GetInt(Min, Max); }
};

int GetRandomIntFast(int min, int max);

// ------------------------------------------------------------------------
// Sockets
// ------------------------------------------------------------------------

enum class SocketType {
    Dual,
    IPv4,
    IPv6,
    Unix
};
static const char *const SocketTypeNames[] = {
    "Dual",
    "IPv4",
    "IPv6",
    "Unix"
};

enum class SocketMode {
    ConnectStream,
    ConnectDatagrams,
    FreeDatagrams
};

int OpenIPSocket(SocketType type, int port, SocketMode mode = SocketMode::ConnectStream);

int OpenUnixSocket(const char *path, SocketMode mode = SocketMode::ConnectStream);
int ConnectToUnixSocket(const char *path, SocketMode mode = SocketMode::ConnectStream);

void CloseSocket(int fd);

// ------------------------------------------------------------------------
// Tasks
// ------------------------------------------------------------------------

class Async {
    RG_DELETE_COPY(Async)

    bool stop_after_error;
    std::atomic_bool success { true };
    std::atomic_int remaining_tasks { 0 };

    class AsyncPool *pool;

public:
    Async(int threads = -1, bool stop_after_error = true);
    Async(Async *parent, bool stop_after_error = true);
    ~Async();

    void Run(const std::function<bool()> &f);
    bool Sync();

    static bool IsTaskRunning();
    static int GetWorkerIdx();
    static int GetWorkerCount();

    friend class AsyncPool;
};

// ------------------------------------------------------------------------
// Fibers
// ------------------------------------------------------------------------

class Fiber {
    RG_DELETE_COPY(Fiber)

    std::function<bool()> f;

#ifdef _WIN32
    void *fiber = nullptr;
#elif defined(RG_FIBER_USE_UCONTEXT)
    ucontext_t ucp = {};
#else
    pthread_t thread;
    bool joinable = false;

    std::mutex mutex;
    std::condition_variable cv;
    std::unique_lock<std::mutex> lock { mutex };
    int toggle = 1;
#endif

    bool done = true;
    bool success = false;

public:
    Fiber(const std::function<bool()> &f, Size stack_size = RG_FIBER_DEFAULT_STACK_SIZE);
    ~Fiber();

    void SwitchTo();
    bool Finalize();

    static bool SwitchBack();

private:
#if defined(_WIN64)
    static void FiberCallback(void *udata);
#elif defined(_WIN32)
    static void __stdcall FiberCallback(void *udata);
#elif defined(RG_FIBER_USE_UCONTEXT)
    static void FiberCallback(unsigned int high, unsigned int low);
#else
    static void *ThreadCallback(void *udata);
    void Toggle(int to, std::unique_lock<std::mutex> *lock);
#endif
};

// ------------------------------------------------------------------------
// Streams
// ------------------------------------------------------------------------

enum class CompressionSpeed {
    Default,
    Slow,
    Fast
};

class StreamDecoder;
class StreamEncoder;

class StreamReader {
    RG_DELETE_COPY(StreamReader)

    enum class SourceType {
        Memory,
        File,
        Function
    };

    const char *filename = nullptr;
    bool error = true;

    int64_t read_total = 0;
    int64_t read_max = -1;

    struct {
        SourceType type = SourceType::Memory;
        union U {
            struct {
                Span<const uint8_t> buf;
                Size pos;
            } memory;
            struct {
                FILE *fp;
                bool owned;
            } file;
            std::function<Size(Span<uint8_t> buf)> func;

            // StreamReader deals with func destructor
            U() {}
            ~U() {}
        } u;

        bool eof = false;
    } source;

    StreamDecoder *decoder = nullptr;

    int64_t raw_len = -1;
    Size raw_read = 0;
    bool eof = false;

    BlockAllocator str_alloc;

public:
    StreamReader() { Close(true); }
    StreamReader(Span<const uint8_t> buf, const char *filename = nullptr,
                 CompressionType compression_type = CompressionType::None)
        : StreamReader() { Open(buf, filename, compression_type); }
    StreamReader(FILE *fp, const char *filename,
                 CompressionType compression_type = CompressionType::None)
        : StreamReader() { Open(fp, filename, compression_type); }
    StreamReader(const char *filename,
                 CompressionType compression_type = CompressionType::None)
        : StreamReader() { Open(filename, compression_type); }
    StreamReader(const std::function<Size(Span<uint8_t>)> &func, const char *filename = nullptr,
                 CompressionType compression_type = CompressionType::None)
        : StreamReader() { Open(func, filename, compression_type); }
    ~StreamReader() { Close(true); }

    // Call before Open!
    void SetDecoder(StreamDecoder *decoder);

    bool Open(Span<const uint8_t> buf, const char *filename = nullptr,
              CompressionType compression_type = CompressionType::None);
    bool Open(FILE *fp, const char *filename,
              CompressionType compression_type = CompressionType::None);
    OpenResult Open(const char *filename, CompressionType compression_type = CompressionType::None);
    bool Open(const std::function<Size(Span<uint8_t>)> &func, const char *filename = nullptr,
              CompressionType compression_type = CompressionType::None);
    bool Close() { return Close(false); }

    // File-specific
    bool Rewind();

    const char *GetFileName() const { return filename; }
    int64_t GetReadLimit() { return read_max; }
    bool IsValid() const { return filename && !error; }
    bool IsEOF() const { return eof; }

    FILE *GetFile() const;
    int GetDescriptor() const;

    void SetReadLimit(int64_t limit) { read_max = limit; }

    Size Read(Span<uint8_t> out_buf);
    Size Read(Span<char> out_buf) { return Read(out_buf.As<uint8_t>()); }
    Size Read(Size buf_len, void *out_buf) { return Read(MakeSpan((uint8_t *)out_buf, buf_len)); }

    Size ReadAll(Size max_len, HeapArray<uint8_t> *out_buf);
    Size ReadAll(Size max_len, HeapArray<char> *out_buf)
        { return ReadAll(max_len, (HeapArray<uint8_t> *)out_buf); }

    int64_t ComputeRawLen();
    int64_t GetRawRead() const { return raw_read; }

private:
    bool Close(bool implicit);

    bool InitDecompressor(CompressionType type);

    Size ReadRaw(Size max_len, void *out_buf);

    friend class StreamDecoder;
};

static inline Size ReadFile(const char *filename, Span<uint8_t> out_buf)
{
    StreamReader st(filename);
    return st.Read(out_buf);
}
static inline Size ReadFile(const char *filename, Span<char> out_buf)
{
    StreamReader st(filename);
    return st.Read(out_buf);
}
static inline Size ReadFile(const char *filename, Size max_len, HeapArray<uint8_t> *out_buf)
{
    StreamReader st(filename);
    return st.ReadAll(max_len, out_buf);
}
static inline Size ReadFile(const char *filename, Size max_len, HeapArray<char> *out_buf)
{
    StreamReader st(filename);
    return st.ReadAll(max_len, out_buf);
}

class StreamDecoder {
protected:
    StreamReader *reader;

public:
    StreamDecoder(StreamReader *reader) : reader(reader) {}
    virtual ~StreamDecoder() {}

    virtual Size Read(Size max_len, void *out_buf) = 0;

protected:
    const char *GetFileName() const { return reader->filename; }
    bool IsValid() const { return reader->IsValid(); }
    bool IsSourceEOF() const { return reader->source.eof; }

    Size ReadRaw(Size max_len, void *out_buf) { return reader->ReadRaw(max_len, out_buf); }

    void SetEOF(bool eof) { reader->eof = eof; }
};

typedef StreamDecoder *CreateDecompressorFunc(StreamReader *reader, CompressionType type);

class StreamDecompressorHelper {
public:
    StreamDecompressorHelper(CompressionType type, CreateDecompressorFunc *func);
};

#define RG_REGISTER_DECOMPRESSOR(Type, Cls) \
    static StreamDecoder *RG_UNIQUE_NAME(CreateDecompressor)(StreamReader *reader, CompressionType type) \
    { \
        StreamDecoder *decompressor = new Cls(reader, type); \
        return decompressor; \
    } \
    static StreamDecompressorHelper RG_UNIQUE_NAME(CreateDecompressorHelper)((Type), RG_UNIQUE_NAME(CreateDecompressor))

class LineReader {
    RG_DELETE_COPY(LineReader)

    HeapArray<char> buf;
    Span<char> view = {};

    StreamReader *st;
    bool error;
    bool eof = false;

    Span<char> line = {};
    int line_number = 0;

public:
    LineReader(StreamReader *st) : st(st), error(!st->IsValid()) {}

    const char *GetFileName() const { return st->GetFileName(); }
    int GetLineNumber() const { return line_number; }
    bool IsValid() const { return !error; }
    bool IsEOF() const { return eof; }

    bool Next(Span<char> *out_line);
    bool Next(Span<const char> *out_line) { return Next((Span<char> *)out_line); }

    void PushLogFilter();
};

enum class StreamWriterFlag {
    Exclusive = 1 << 0,
    Atomic = 1 << 1
};

class StreamWriter {
    RG_DELETE_COPY(StreamWriter)

    enum class DestinationType {
        Memory,
        File,
        Function
    };

    const char *filename = nullptr;
    bool error = true;

    struct {
        DestinationType type = DestinationType::Memory;
        union U {
            struct {
                HeapArray<uint8_t> *memory;
                Size start;
            } mem;
            struct {
                FILE *fp;
                bool owned;

                // Atomic write mode
                const char *tmp_filename;
                bool tmp_exclusive;
            } file;
            std::function<bool(Span<const uint8_t>)> func;

            // StreamWriter deals with func destructor
            U() {}
            ~U() {}
        } u;

        bool vt100;
    } dest;

    StreamEncoder *encoder = nullptr;

    int64_t raw_written = 0;

    BlockAllocator str_alloc;

public:
    StreamWriter() { Close(true); }
    StreamWriter(HeapArray<uint8_t> *mem, const char *filename = nullptr,
                 CompressionType compression_type = CompressionType::None,
                 CompressionSpeed compression_speed = CompressionSpeed::Default)
        : StreamWriter() { Open(mem, filename, compression_type, compression_speed); }
    StreamWriter(HeapArray<char> *mem, const char *filename = nullptr,
                 CompressionType compression_type = CompressionType::None,
                 CompressionSpeed compression_speed = CompressionSpeed::Default)
        : StreamWriter() { Open(mem, filename, compression_type, compression_speed); }
    StreamWriter(FILE *fp, const char *filename,
                 CompressionType compression_type = CompressionType::None,
                 CompressionSpeed compression_speed = CompressionSpeed::Default)
        : StreamWriter() { Open(fp, filename, compression_type, compression_speed); }
    StreamWriter(const char *filename, unsigned int flags = 0,
                 CompressionType compression_type = CompressionType::None,
                 CompressionSpeed compression_speed = CompressionSpeed::Default)
        : StreamWriter() { Open(filename, flags, compression_type, compression_speed); }
    StreamWriter(const std::function<bool(Span<const uint8_t>)> &func, const char *filename = nullptr,
                 CompressionType compression_type = CompressionType::None,
                 CompressionSpeed compression_speed = CompressionSpeed::Default)
        : StreamWriter() { Open(func, filename, compression_type, compression_speed); }
    ~StreamWriter() { Close(true); }

    // Call before Open!
    void SetEncoder(StreamEncoder *encoder);

    bool Open(HeapArray<uint8_t> *mem, const char *filename = nullptr,
              CompressionType compression_type = CompressionType::None,
              CompressionSpeed compression_speed = CompressionSpeed::Default);
    bool Open(HeapArray<char> *mem, const char *filename = nullptr,
              CompressionType compression_type = CompressionType::None,
              CompressionSpeed compression_speed = CompressionSpeed::Default)
        { return Open((HeapArray<uint8_t> *)mem, filename, compression_type, compression_speed); }
    bool Open(FILE *fp, const char *filename,
              CompressionType compression_type = CompressionType::None,
              CompressionSpeed compression_speed = CompressionSpeed::Default);
    bool Open(const char *filename, unsigned int flags = 0,
              CompressionType compression_type = CompressionType::None,
              CompressionSpeed compression_speed = CompressionSpeed::Default);
    bool Open(const std::function<bool(Span<const uint8_t>)> &func, const char *filename = nullptr,
              CompressionType compression_type = CompressionType::None,
              CompressionSpeed compression_speed = CompressionSpeed::Default);
    bool Close() { return Close(false); }

    // For compressed streams, Flush may not be complete and only Close() can finalize the file.
    bool Flush();

    const char *GetFileName() const { return filename; }
    bool IsVt100() const { return dest.vt100; }
    bool IsValid() const { return filename && !error; }

    FILE *GetFile() const;
    int GetDescriptor() const;

    bool Write(Span<const uint8_t> buf);
    bool Write(Span<const char> buf) { return Write(buf.As<const uint8_t>()); }
    bool Write(char buf) { return Write(MakeSpan(&buf, 1)); }
    bool Write(const void *buf, Size len) { return Write(MakeSpan((const uint8_t *)buf, len)); }

    int64_t GetRawWritten() const { return raw_written; }

private:
    bool Close(bool implicit);

    bool InitCompressor(CompressionType type, CompressionSpeed speed);

    bool WriteRaw(Span<const uint8_t> buf);

    friend class StreamEncoder;
};

static inline bool WriteFile(Span<const uint8_t> buf, const char *filename, unsigned int flags = 0)
{
    StreamWriter st(filename, flags);
    st.Write(buf);
    return st.Close();
}
static inline bool WriteFile(Span<const char> buf, const char *filename, unsigned int flags = 0)
{
    StreamWriter st(filename, flags);
    st.Write(buf);
    return st.Close();
}

class StreamEncoder {
protected:
    StreamWriter *writer;

public:
    StreamEncoder(StreamWriter *writer) : writer(writer) {}
    virtual ~StreamEncoder() {}

    virtual bool Write(Span<const uint8_t> buf) = 0;
    virtual bool Finalize() = 0;

protected:
    const char *GetFileName() const { return writer->filename; }
    bool IsValid() const { return writer->IsValid(); }

    bool WriteRaw(Span<const uint8_t> buf) { return writer->WriteRaw(buf); }
};

typedef StreamEncoder *CreateCompressorFunc(StreamWriter *writer, CompressionType type, CompressionSpeed speed);

class StreamCompressorHelper {
public:
    StreamCompressorHelper(CompressionType type, CreateCompressorFunc *func);
};

#define RG_REGISTER_COMPRESSOR(Type, Cls) \
    static StreamEncoder *RG_UNIQUE_NAME(CreateCompressor)(StreamWriter *writer, CompressionType type, CompressionSpeed speed) \
    { \
        StreamEncoder *compressor = new Cls(writer, type, speed); \
        return compressor; \
    } \
    static StreamCompressorHelper RG_UNIQUE_NAME(CreateCompressorHelper)((Type), RG_UNIQUE_NAME(CreateCompressor))

bool SpliceStream(StreamReader *reader, int64_t max_len, StreamWriter *writer);

bool IsCompressorAvailable(CompressionType compression_type);
bool IsDecompressorAvailable(CompressionType compression_type);

// For convenience, don't close them
extern StreamReader stdin_st;
extern StreamWriter stdout_st;
extern StreamWriter stderr_st;

// ------------------------------------------------------------------------
// INI
// ------------------------------------------------------------------------

struct IniProperty {
    Span<const char> section;
    Span<const char> key;
    Span<const char> value;
};

class IniParser {
    RG_DELETE_COPY(IniParser)

    HeapArray<char> current_section;

    enum class LineType {
        Section,
        KeyValue,
        Exit
    };

    LineReader reader;
    bool eof = false;
    bool error = false;

public:
    IniParser(StreamReader *st) : reader(st) {}

    const char *GetFileName() const { return reader.GetFileName(); }
    bool IsValid() const { return !error; }
    bool IsEOF() const { return eof; }

    bool Next(IniProperty *out_prop);
    bool NextInSection(IniProperty *out_prop);

    void PushLogFilter() { reader.PushLogFilter(); }

private:
    LineType FindNextLine(IniProperty *out_prop);
};

// ------------------------------------------------------------------------
// Assets
// ------------------------------------------------------------------------

// Keep in sync with version in packer.cc
struct AssetInfo {
    const char *name;
    CompressionType compression_type;
    Span<const uint8_t> data;

    RG_HASHTABLE_HANDLER(AssetInfo, name);
};

#ifdef FELIX_HOT_ASSETS

bool ReloadAssets();
Span<const AssetInfo> GetPackedAssets();
const AssetInfo *FindPackedAsset(const char *name);

#else

// Reserved for internal use
void InitPackedMap(Span<const AssetInfo> assets);

extern "C" const Span<const AssetInfo> PackedAssets;
extern HashTable<const char *, const AssetInfo *> PackedAssets_map;

// By definining functions here (with static inline), we don't need PackedAssets
// to exist unless these functions are called. It also allows the compiler to remove
// calls to ReloadAssets in non-debug builds (LTO or not).

static inline bool ReloadAssets()
{
    return false;
}

static inline Span<const AssetInfo> GetPackedAssets()
{
    return PackedAssets;
}

static inline const AssetInfo *FindPackedAsset(const char *name)
{
    InitPackedMap(PackedAssets);
    return PackedAssets_map.FindValue(name, nullptr);
}

#endif

// These functions won't win any beauty or speed contest but whatever
bool PatchFile(StreamReader *reader, StreamWriter *writer,
               FunctionRef<void(Span<const char>, StreamWriter *)> func);
bool PatchFile(Span<const uint8_t> data, StreamWriter *writer,
               FunctionRef<void(Span<const char>, StreamWriter *)> func);
bool PatchFile(const AssetInfo &asset, StreamWriter *writer,
               FunctionRef<void(Span<const char>, StreamWriter *)> func);
Span<const uint8_t> PatchFile(Span<const uint8_t> data, Allocator *alloc,
                              FunctionRef<void(Span<const char> key, StreamWriter *)> func);
Span<const uint8_t> PatchFile(const AssetInfo &asset, Allocator *alloc,
                              FunctionRef<void(Span<const char> key, StreamWriter *)> func);

// ------------------------------------------------------------------------
// Options
// ------------------------------------------------------------------------

struct OptionDesc {
    const char *name;
    const char *help;
};

enum class OptionMode {
    Rotate,
    Skip,
    Stop
};

enum class OptionType {
    NoValue,
    Value,
    OptionalValue
};

class OptionParser {
    RG_DELETE_COPY(OptionParser)

    Span<const char *> args;
    OptionMode mode;

    Size pos = 0;
    Size limit;
    Size smallopt_offset = 0;
    char buf[80];

    bool test_failed = false;

public:

    const char *current_option = nullptr;
    const char *current_value = nullptr;

    OptionParser(Span<const char *> args, OptionMode mode = OptionMode::Rotate)
        : args(args), mode(mode), limit(args.len) {}
    OptionParser(int argc, char **argv, OptionMode mode = OptionMode::Rotate)
        : args(argc > 0 ? (const char **)(argv + 1) : nullptr, std::max(0, argc - 1)),
          mode(mode), limit(args.len) {}

    const char *Next();

    const char *ConsumeValue();
    const char *ConsumeNonOption();
    void ConsumeNonOptions(HeapArray<const char *> *non_options);

    Span<const char *> GetRemainingArguments() const { return args.Take(pos, args.len - pos); }

    bool Test(const char *test1, const char *test2, OptionType type = OptionType::NoValue);
    bool Test(const char *test1, OptionType type = OptionType::NoValue)
        { return Test(test1, nullptr, type); }

    bool TestHasFailed() const { return test_failed; }

    void LogUnknownError() const;
    void LogUnusedArguments() const;
};

template <typename T>
bool OptionToEnum(Span<const char *const> options, Span<const char> str, T *out_value)
{
    for (Size i = 0; i < options.len; i++) {
        const char *opt = options[i];

        if (TestStr(opt, str)) {
            *out_value = (T)i;
            return true;
        }
    }

    return false;
}

template <typename T>
bool OptionToEnum(Span<const OptionDesc> options, Span<const char> str, T *out_value)
{
    for (Size i = 0; i < options.len; i++) {
        const OptionDesc &desc = options[i];

        if (TestStr(desc.name, str)) {
            *out_value = (T)i;
            return true;
        }
    }

    return false;
}

template <typename T>
bool OptionToEnumI(Span<const char *const> options, Span<const char> str, T *out_value)
{
    for (Size i = 0; i < options.len; i++) {
        const char *opt = options[i];

        if (TestStrI(opt, str)) {
            *out_value = (T)i;
            return true;
        }
    }

    return false;
}

template <typename T>
bool OptionToEnumI(Span<const OptionDesc> options, Span<const char> str, T *out_value)
{
    for (Size i = 0; i < options.len; i++) {
        const OptionDesc &desc = options[i];

        if (TestStrI(desc.name, str)) {
            *out_value = (T)i;
            return true;
        }
    }

    return false;
}

template <typename T>
bool OptionToFlag(Span<const char *const> options, Span<const char> str, T *out_flags, bool enable = true)
{
    for (Size i = 0; i < options.len; i++) {
        const char *opt = options[i];

        if (TestStr(opt, str)) {
            *out_flags = ApplyMask(*out_flags, 1u << i, enable);
            return true;
        }
    }

    return false;
}

template <typename T>
bool OptionToFlag(Span<const OptionDesc> options, Span<const char> str, T *out_flags, bool enable = true)
{
    for (Size i = 0; i < options.len; i++) {
        const OptionDesc &desc = options[i];

        if (TestStr(desc.name, str)) {
            *out_flags = ApplyMask(*out_flags, 1u << i, enable);
            return true;
        }
    }

    return false;
}

template <typename T>
bool OptionToFlagI(Span<const char *const> options, Span<const char> str, T *out_flags, bool enable = true)
{
    for (Size i = 0; i < options.len; i++) {
        const char *opt = options[i];

        if (TestStrI(opt, str)) {
            *out_flags = ApplyMask(*out_flags, 1u << i, enable);
            return true;
        }
    }

    return false;
}

template <typename T>
bool OptionToFlagI(Span<const OptionDesc> options, Span<const char> str, T *out_flags, bool enable = true)
{
    for (Size i = 0; i < options.len; i++) {
        const OptionDesc &desc = options[i];

        if (TestStrI(desc.name, str)) {
            *out_flags = ApplyMask(*out_flags, 1u << i, enable);
            return true;
        }
    }

    return false;
}

// ------------------------------------------------------------------------
// Console prompter (simplified readline)
// ------------------------------------------------------------------------

class ConsolePrompter {
    int prompt_columns = 0;

    HeapArray<HeapArray<char>> entries;
    Size entry_idx = 0;
    Size str_offset = 0;

    int columns = 0;
    int rows = 0;
    int rows_with_extra = 0;
    int x = 0;
    int y = 0;

    const char *fake_input = "";
#ifdef _WIN32
    uint32_t surrogate_buf = 0;
#endif

public:
    const char *prompt = ">>> ";
    const char *mask = nullptr;

    HeapArray<char> str;

    ConsolePrompter();

    bool Read(Span<const char> *out_str = nullptr);
    bool ReadYN(bool *out_value);

    void Commit();

private:
    bool ReadRaw(Span<const char> *out_str);
    bool ReadRawYN(bool *out_value);
    bool ReadBuffered(Span<const char> *out_str);
    bool ReadBufferedYN(bool *out_value);

    void ChangeEntry(Size new_idx);

    Size SkipForward(Size offset, Size count);
    Size SkipBackward(Size offset, Size count);
    Size FindForward(Size offset, const char *chars);
    Size FindBackward(Size offset, const char *chars);

    void Delete(Size start, Size end);

    void RenderRaw();
    void RenderBuffered();

    Vec2<int> GetConsoleSize();
    int32_t ReadChar();

    int ComputeWidth(Span<const char> str);

    void EnsureNulTermination();
};

const char *Prompt(const char *prompt, const char *default_value, const char *mask, Allocator *alloc);
static inline const char *Prompt(const char *prompt, Allocator *alloc)
    { return Prompt(prompt, nullptr, nullptr, alloc); }
bool PromptYN(const char *prompt, bool *out_value);

// ------------------------------------------------------------------------
// Mime types
// ------------------------------------------------------------------------

const char *GetMimeType(Span<const char> extension, const char *default_typ = "application/octet-stream");

bool CanCompressFile(const char *filename);

}
