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

const fs = require('fs');
const path = require('path');
const util = require('util');
const { get_napi_version, determine_arch } = require('../../cnoke/src/tools.js');
const pkg = require('../package.json');

function detect() {
    if (process.versions.napi == null || process.versions.napi < pkg.cnoke.napi) {
        let major = parseInt(process.versions.node, 10);
        let required = get_napi_version(pkg.cnoke.napi, major);

        if (required != null) {
            throw new Error(`This engine is based on Node ${process.versions.node}, but ${pkg.name} requires Node >= ${required} in the Node ${major}.x branch (N-API >= ${pkg.cnoke.napi})`);
        } else {
            throw new Error(`This engine is based on Node ${process.versions.node}, but ${pkg.name} does not support the Node ${major}.x branch (N-API < ${pkg.cnoke.napi})`);
        }
    }

    let arch = determine_arch();
    let triplet = `${process.platform}_${arch}`;

    return triplet;
}

function init(triplet, native) {
    if (native == null) {
        let roots = [path.join(__dirname, '..')];
        let triplets = [triplet];

        if (process.resourcesPath != null)
            roots.push(process.resourcesPath);
        if (triplet.startsWith('linux_')) {
            let musl = triplet.replace(/^linux_/, 'musl_');
            triplets.push(musl);
        }

        let filenames = roots.flatMap(root => triplets.flatMap(triplet => [
            `${root}/build/koffi/${triplet}/koffi.node`,
            `${root}/koffi/${triplet}/koffi.node`,
            `${root}/node_modules/koffi/build/koffi/${triplet}/koffi.node`,
            `${root}/../../bin/Koffi/${triplet}/koffi.node`
        ]));

        let first_err = null;

        for (let filename of filenames) {
            if (!fs.existsSync(filename))
                continue;

            try {
                // Trick so that webpack does not try to do anything with require() call
                native = eval('require')(filename);
            } catch (err) {
                if (first_err == null)
                    first_err = err;
                continue;
            }

            break;
        }

        if (first_err != null)
            throw first_err;
    }

    if (native == null)
        throw new Error('Cannot find the native Koffi module; did you bundle it correctly?');
    if (native.version != pkg.version)
        throw new Error('Mismatched native Koffi modules');

    let mod = wrap(native);
    return mod;
}

function wrap(native) {
    let obj = {
        ...native,

        // Deprecated functions
        handle: util.deprecate(native.opaque, 'The koffi.handle() function was deprecated in Koffi 2.1, use koffi.opaque() instead', 'KOFFI001'),
        callback: util.deprecate(native.proto, 'The koffi.callback() function was deprecated in Koffi 2.4, use koffi.proto() instead', 'KOFFI002')
    };

    obj.load = (...args) => {
        let lib = native.load(...args);

        lib.cdecl = util.deprecate((...args) => lib.func('__cdecl', ...args), 'The koffi.cdecl() function was deprecated in Koffi 2.7, use koffi.func(...) instead', 'KOFFI003');
        lib.stdcall = util.deprecate((...args) => lib.func('__stdcall', ...args), 'The koffi.stdcall() function was deprecated in Koffi 2.7, use koffi.func("__stdcall", ...) instead', 'KOFFI004');
        lib.fastcall = util.deprecate((...args) => lib.func('__fastcall', ...args), 'The koffi.fastcall() function was deprecated in Koffi 2.7, use koffi.func("__fastcall", ...) instead', 'KOFFI005');
        lib.thiscall = util.deprecate((...args) => lib.func('__thiscall', ...args), 'The koffi.thiscall() function was deprecated in Koffi 2.7, use koffi.func("__thiscall", ...) instead', 'KOFFI006');

        return lib;
    };

    return obj;
}

module.exports = {
    detect,
    init
};
