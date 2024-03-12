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
#include "util.hh"

#include <napi.h>

namespace RG {

struct InstanceData;
struct TypeInfo;
struct FunctionInfo;

class PrototypeParser {
    Napi::Env env;
    InstanceData *instance;

    // All these members are relevant to the current parse only, and get resetted each time
    HeapArray<Span<const char>> tokens;
    Size offset;
    bool valid;

public:
    PrototypeParser(Napi::Env env) : env(env), instance(env.GetInstanceData<InstanceData>()) {}

    bool Parse(const char *str, FunctionInfo *out_func);

private:
    void Tokenize(const char *str);

    const TypeInfo *ParseType(int *out_directions);
    const char *ParseIdentifier();

    bool Consume(const char *expect);
    bool Match(const char *expect);

    bool IsIdentifier(Span<const char> tok) const;

    template <typename... Args>
    void MarkError(const char *fmt, Args... args)
    {
        if (valid) {
            ThrowError<Napi::Error>(env, fmt, args...);
            valid = false;
        }
        valid = false;
    }
};

bool ParsePrototype(Napi::Env env, const char *str, FunctionInfo *out_func);

}
