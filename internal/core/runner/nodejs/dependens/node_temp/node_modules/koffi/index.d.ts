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

declare module 'koffi' {
    export function load(path: string | null): IKoffiLib;

    interface IKoffiCType { __brand: 'IKoffiCType' }
    interface IKoffiPointerCast { __brand: 'IKoffiPointerCast' }
    interface IKoffiRegisteredCallback { __brand: 'IKoffiRegisteredCallback' }

    type PrimitiveKind = 'Void' | 'Bool' | 'Int8' | 'UInt8' | 'Int16' | 'Int16S' | 'UInt16' | 'UInt16S' |
                         'Int32' | 'Int32S' | 'UInt32' | 'UInt32S' | 'Int64' | 'Int64S' | 'UInt64' | 'UInt64S' |
                         'String' | 'String16' | 'Pointer' | 'Record' | 'Union' | 'Array' | 'Float32' | 'Float64' |
                         'Prototype' | 'Callback';
    type ArrayHint = 'Array' | 'Typed' | 'String';

    type TypeSpec = string | IKoffiCType;
    type TypeSpecWithAlignment = TypeSpec | [number, TypeSpec];
    type TypeInfo = {
        name: string;
        primitive: PrimitiveKind;
        size: number;
        alignment: number;
        disposable: boolean;
        length?: number;
        hint?: ArrayHint;
        ref?: IKoffiCType;
        members?: Record<string, { name: string, type: IKoffiCType, offset: number }>;
    };
    type KoffiFunction = {
        (...args: any[]) : any;
        async: (...args: any[]) => any;
        info: {
            name: string,
            arguments: IKoffiCType[],
            result: IKoffiCType
        };
    };

    export type KoffiFunc<T extends (...args: any) => any> = T & {
       async: (...args: [...Parameters<T>, (err: any, result: ReturnType<T>) => void]) => void;
       info: {
          name: string;
          arguments: IKoffiCType[];
          result: IKoffiCType;
       };
    };

    export interface IKoffiLib {
        func(definition: string): KoffiFunction;
        func(name: string, result: TypeSpec, arguments: TypeSpec[]): KoffiFunction;
        func(convention: string, name: string, result: TypeSpec, arguments: TypeSpec[]): KoffiFunction;

        /** @deprecated */ cdecl(definition: string): KoffiFunction;
        /** @deprecated */ cdecl(name: string, result: TypeSpec, arguments: TypeSpec[]): KoffiFunction;
        /** @deprecated */ stdcall(definition: string): KoffiFunction;
        /** @deprecated */ stdcall(name: string, result: TypeSpec, arguments: TypeSpec[]): KoffiFunction;
        /** @deprecated */ fastcall(definition: string): KoffiFunction;
        /** @deprecated */ fastcall(name: string, result: TypeSpec, arguments: TypeSpec[]): KoffiFunction;
        /** @deprecated */ thiscall(definition: string): KoffiFunction;
        /** @deprecated */ thiscall(name: string, result: TypeSpec, arguments: TypeSpec[]): KoffiFunction;

        symbol(name: string, type: TypeSpec): any;

        unload(): void;
    }

    export function struct(name: string, def: Record<string, TypeSpecWithAlignment>): IKoffiCType;
    export function struct(def: Record<string, TypeSpecWithAlignment>): IKoffiCType;
    export function pack(name: string, def: Record<string, TypeSpecWithAlignment>): IKoffiCType;
    export function pack(def: Record<string, TypeSpecWithAlignment>): IKoffiCType;

    export function union(name: string, def: Record<string, TypeSpecWithAlignment>): IKoffiCType;
    export function union(def: Record<string, TypeSpecWithAlignment>): IKoffiCType;

    export class Union {
        constructor(type: TypeSpec);
        [s: string]: any;
    }

    export function array(ref: TypeSpec, len: number, hint?: ArrayHint | null): IKoffiCType;

    export function opaque(name: string): IKoffiCType;
    export function opaque(): IKoffiCType;
    /** @deprecated */ export function handle(name: string): IKoffiCType;
    /** @deprecated */ export function handle(): IKoffiCType;

    export function pointer(ref: TypeSpec): IKoffiCType;
    export function pointer(ref: TypeSpec, asteriskCount?: number): IKoffiCType;
    export function pointer(name: string, ref: TypeSpec, asteriskCount?: number): IKoffiCType;

    export function out(type: TypeSpec): IKoffiCType;
    export function inout(type: TypeSpec): IKoffiCType;

    export function disposable(type: TypeSpec): IKoffiCType;
    export function disposable(name: string, type: TypeSpec): IKoffiCType;
    export function disposable(name: string, type: TypeSpec, freeFunction: Function): IKoffiCType;

    export function proto(definition: string): IKoffiCType;
    export function proto(name: string, result: TypeSpec, arguments: TypeSpec[]): IKoffiCType;
    export function proto(convention: string, name: string, result: TypeSpec, arguments: TypeSpec[]): IKoffiCType;
    /** @deprecated */ export function callback(definition: string): IKoffiCType;
    /** @deprecated */ export function callback(name: string, result: TypeSpec, arguments: TypeSpec[]): IKoffiCType;
    /** @deprecated */ export function callback(convention: string, name: string, result: TypeSpec, arguments: TypeSpec[]): IKoffiCType;

    export function register(callback: Function, type: TypeSpec): IKoffiRegisteredCallback;
    export function register(thisValue: any, callback: Function, type: TypeSpec): IKoffiRegisteredCallback;
    export function unregister(callback: IKoffiRegisteredCallback): void;

    export function as(value: any, type: TypeSpec): IKoffiPointerCast;
    export function decode(value: any, type: TypeSpec): any;
    export function decode(value: any, type: TypeSpec, len: number): any;
    export function decode(value: any, offset: number, type: TypeSpec): any;
    export function decode(value: any, offset: number, type: TypeSpec, len: number): any;
    export function address(value: any): bigint;
    export function call(value: any, type: TypeSpec, ...args: any[]): any;
    export function encode(ref: any, type: TypeSpec, value: any): void;
    export function encode(ref: any, type: TypeSpec, value: any, len: number): void;
    export function encode(ref: any, offset: number, type: TypeSpec): void;
    export function encode(ref: any, offset: number, type: TypeSpec, value: any): void;
    export function encode(ref: any, offset: number, type: TypeSpec, value: any, len: number): void;

    export function sizeof(type: TypeSpec): number;
    export function alignof(type: TypeSpec): number;
    export function offsetof(type: TypeSpec): number;
    export function resolve(type: TypeSpec): IKoffiCType;
    export function introspect(type: TypeSpec): TypeInfo;

    export function alias(name: string, type: TypeSpec): IKoffiCType;

    export function config(): Record<string, unknown>;
    export function config(cfg: Record<string, unknown>): Record<string, unknown>;
    export function stats(): Record<string, unknown>;

    export function alloc(type: TypeSpec, length: number): any;
    export function free(value: any): void;

    export function errno(): number;
    export function errno(value: number): number;

    export function reset(): void;

    export const internal: Boolean;
    export const extension: String;

    export const os: {
        errno: Record<string, number>
    };

    export const types: Record<string, IKoffiCType>;
}
