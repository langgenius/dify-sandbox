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
#include "parser.hh"

#include <napi.h>

namespace RG {

bool PrototypeParser::Parse(const char *str, FunctionInfo *out_func)
{
    tokens.Clear();
    offset = 0;
    valid = true;

    Tokenize(str);

    out_func->ret.type = ParseType(nullptr);
    if (!CanReturnType(out_func->ret.type)) {
        MarkError("You are not allowed to directly return %1 values (maybe try %1 *)", out_func->ret.type->name);
        return false;
    }
    offset += (offset < tokens.len && DetectCallConvention(tokens[offset], &out_func->convention));
    out_func->name = ParseIdentifier();

    Consume("(");
    offset += (offset + 1 < tokens.len && tokens[offset] == "void" && tokens[offset + 1] == ")");
    if (offset < tokens.len && tokens[offset] != ")") {
        for (;;) {
            ParameterInfo param = {};

            if (Match("...")) {
                out_func->variadic = true;
                break;
            }

            param.type = ParseType(&param.directions);

            if (!CanPassType(param.type, param.directions)) {
                MarkError("Type %1 cannot be used as a parameter", param.type->name);
                return false;
            }
            if (out_func->parameters.len >= MaxParameters) {
                MarkError("Functions cannot have more than %1 parameters", MaxParameters);
                return false;
            }
            if ((param.directions & 2) && ++out_func->out_parameters >= MaxParameters) {
                MarkError("Functions cannot have more than out %1 parameters", MaxParameters);
                return false;
            }

            param.offset = (int8_t)out_func->parameters.len;

            out_func->parameters.Append(param);

            offset += (offset < tokens.len && IsIdentifier(tokens[offset]));
            if (offset >= tokens.len || tokens[offset] != ",")
                break;
            offset++;
        }
    }
    Consume(")");

    out_func->required_parameters = (int8_t)out_func->parameters.len;

    Match(";");
    if (offset < tokens.len) {
        MarkError("Unexpected token '%1' after prototype", tokens[offset]);
    }

    return valid;
}

void PrototypeParser::Tokenize(const char *str)
{
    for (Size i = 0; str[i]; i++) {
        char c = str[i];

        if (IsAsciiWhite(c)) {
            continue;
        } else if (IsAsciiAlpha(c) || c == '_') {
            Size j = i;
            while (str[++j] && (IsAsciiAlphaOrDigit(str[j]) || str[j] == '_'));

            Span<const char> tok = MakeSpan(str + i, j - i);
            tokens.Append(tok);

            i = j - 1;
        } else if (IsAsciiDigit(c)) {
            Size j = i;
            while (str[++j] && IsAsciiDigit(str[j]));
            if (str[j] == '.') {
                while (str[++j] && IsAsciiDigit(str[j]));
            }

            Span<const char> tok = MakeSpan(str + i, j - i);
            tokens.Append(tok);

            i = j - 1;
        } else if (c == '.' && str[i + 1] == '.' && str[i + 2] == '.') {
            tokens.Append("...");
            i += 2;
        } else {
            Span<const char> tok = MakeSpan(str + i, 1);
            tokens.Append(tok);
        }
    }
}

const TypeInfo *PrototypeParser::ParseType(int *out_directions)
{
    Size start = offset;

    if (offset >= tokens.len) {
        MarkError("Unexpected end of prototype, expected type");
        return instance->void_type;
    } else if (!IsIdentifier(tokens[offset])) {
        MarkError("Unexpected token '%1', expected type", tokens[offset]);
        return instance->void_type;
    }

    while (++offset < tokens.len && IsIdentifier(tokens[offset]));
    offset--;
    while (++offset < tokens.len && (tokens[offset] == '*' ||
                                     tokens[offset] == '!' ||
                                     tokens[offset] == "const"));
    if (offset < tokens.len && tokens[offset] == "[") [[unlikely]] {
        MarkError("Array types decay to pointers in prototypes (C standard), use pointers");
        return instance->void_type;
    }
    offset--;

    while (offset >= start) {
        Span<const char> str = MakeSpan(tokens[start].ptr, tokens[offset].end() - tokens[start].ptr);
        const TypeInfo *type = ResolveType(env, str, out_directions);

        if (type) {
            offset++;
            return type;
        }
        if (env.IsExceptionPending()) [[unlikely]]
            return instance->void_type;

        offset--;
    }
    offset = start;

    MarkError("Unknown or invalid type name '%1'", tokens[offset]);
    return instance->void_type;
}

const char *PrototypeParser::ParseIdentifier()
{
    if (offset >= tokens.len) {
        MarkError("Unexpected end of prototype, expected identifier");
        return "";
    }
    if (!IsIdentifier(tokens[offset])) {
        MarkError("Unexpected token '%1', expected identifier", tokens[offset]);
        return "";
    }

    Span<const char> tok = tokens[offset++];
    const char *ident = DuplicateString(tok, &instance->str_alloc).ptr;

    return ident;
}

bool PrototypeParser::Consume(const char *expect)
{
    if (offset >= tokens.len) {
        MarkError("Unexpected end of prototype, expected '%1'", expect);
        return false;
    }
    if (tokens[offset] != expect) {
        MarkError("Unexpected token '%1', expected '%2'", tokens[offset], expect);
        return false;
    }

    offset++;
    return true;
}

bool PrototypeParser::Match(const char *expect)
{
    if (offset < tokens.len && tokens[offset] == expect) {
        offset++;
        return true;
    } else {
        return false;
    }
}

bool PrototypeParser::IsIdentifier(Span<const char> tok) const
{
    RG_ASSERT(tok.len);
    return IsAsciiAlpha(tok[0]) || tok[0] == '_';
}

bool ParsePrototype(Napi::Env env, const char *str, FunctionInfo *out_func)
{
    PrototypeParser parser(env);
    return parser.Parse(str, out_func);
}

}
