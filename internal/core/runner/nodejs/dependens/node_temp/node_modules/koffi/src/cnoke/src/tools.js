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

'use strict';

const crypto = require('crypto');
const fs = require('fs');
const http = require('https');
const path = require('path');
const zlib = require('zlib');

async function download_http(url, dest) {
    console.log('>> Downloading ' + url);

    let [tmp_name, file] = open_temporary_stream(dest);

    try {
        await new Promise((resolve, reject) => {
            let request = http.get(url, response => {
                if (response.statusCode != 200) {
                    let err = new Error(`Download failed: ${response.statusMessage} [${response.statusCode}]`);
                    err.code = response.statusCode;

                    reject(err);

                    return;
                }

                response.pipe(file);

                file.on('finish', () => file.close(() => {
                    try {
                        fs.renameSync(file.path, dest);
                    } catch (err) {
                        if (!fs.existsSync(dest))
                            reject(err);
                    }

                    resolve();
                }));
            });

            request.on('error', reject);
            file.on('error', reject);
        });
    } catch (err) {
        file.close();

        try {
            fs.unlinkSync(tmp_name);
        } catch (err) {
            if (err.code != 'ENOENT')
                throw err;
        }

        throw err;
    }
}

function open_temporary_stream(prefix) {
    let buf = Buffer.allocUnsafe(4);

    for (;;) {
        try {
            crypto.randomFillSync(buf);

            let suffix = buf.toString('hex').padStart(8, '0');
            let filename = `${prefix}.${suffix}`;

            let file = fs.createWriteStream(filename, { flags: 'wx', mode: 0o644 });
            return [filename, file];
        } catch (err) {
            if (err.code != 'EEXIST')
                throw err;
        }
    }
}

function extract_targz(filename, dest_dir, strip = 0) {
    let reader = fs.createReadStream(filename).pipe(zlib.createGunzip());

    return new Promise((resolve, reject) => {
        let header = null;
        let extended = {};

        reader.on('readable', () => {
            try {
                for (;;) {
                    if (header == null) {
                        let buf = reader.read(512);
                        if (buf == null)
                            break;

                        // Two zeroed 512-byte blocks end the stream
                        if (!buf[0])
                            continue;

                        header = {
                            filename: buf.toString('utf-8', 0, 100).replace(/\0/g, ''),
                            mode: parseInt(buf.toString('ascii', 100, 109), 8),
                            size: parseInt(buf.toString('ascii', 124, 137), 8),
                            type: String.fromCharCode(buf[156])
                        };

                        // UStar filename prefix
                        /*if (buf.toString('ascii', 257, 263) == 'ustar\0') {
                            let prefix = buf.toString('utf-8', 345, 500).replace(/\0/g, '');
                            console.log(prefix);
                            header.filename = prefix ? (prefix + '/' + header.filename) : header.filename;
                        }*/

                        Object.assign(header, extended);
                        extended = {};

                        // Safety checks
                        header.filename = header.filename.replace(/\\/g, '/');
                        if (!header.filename.length)
                            throw new Error(`Insecure empty filename inside TAR archive`);
                        if (path_is_absolute(header.filename[0]))
                            throw new Error(`Insecure filename starting with / inside TAR archive`);
                        if (path_has_dotdot(header.filename))
                            throw new Error(`Insecure filename containing '..' inside TAR archive`);

                        for (let i = 0; i < strip; i++)
                            header.filename = header.filename.substr(header.filename.indexOf('/') + 1);
                    }

                    let aligned = Math.floor((header.size + 511) / 512) * 512;
                    let data = header.size ? reader.read(aligned) : null;
                    if (data == null) {
                        if (header.size)
                            break;
                        data = Buffer.alloc(0);
                    }
                    data = data.subarray(0, header.size);

                    if (header.type == '0' || header.type == '7') {
                        let filename = dest_dir + '/' + header.filename;
                        let dirname = path.dirname(filename);

                        fs.mkdirSync(dirname, { recursive: true, mode: 0o755 });
                        fs.writeFileSync(filename, data, { mode: header.mode });
                    } else if (header.type == '5') {
                        let filename = dest_dir + '/' + header.filename;
                        fs.mkdirSync(filename, { recursive: true, mode: header.mode });
                    } else if (header.type == 'L') { // GNU tar
                        extended.filename = data.toString('utf-8').replace(/\0/g, '');
                    } else if (header.type == 'x') { // PAX entry
                        let str = data.toString('utf-8');

                        try {
                            while (str.length) {
                                let matches = str.match(/^([0-9]+) ([a-zA-Z0-9\._]+)=(.*)\n/);

                                let skip = parseInt(matches[1], 10);
                                let key = matches[2];
                                let value = matches[3];

                                switch (key) {
                                    case 'path': { extended.filename = value; } break;
                                    case 'size': { extended.size = parseInt(value, 10); } break;
                                }

                                str = str.substr(skip).trimStart();
                            }
                        } catch (err) {
                            throw new Error('Malformed PAX entry');
                        }
                    }

                    header = null;
                }
            } catch (err) {
                reject(err);
            }
        });

        reader.on('error', reject);
        reader.on('end', resolve);
    });
}

function path_is_absolute(path) {
    if (process.platform == 'win32' && path.match(/^[a-zA-Z]:/))
        path = path.substr(2);
    return is_path_separator(path[0]);
}

function path_has_dotdot(path) {
    let start = 0;

    for (;;) {
        let offset = path.indexOf('..', start);
        if (offset < 0)
            break;
        start = offset + 2;

        if (offset && !is_path_separator(path[offset - 1]))
            continue;
        if (offset + 2 < path.length && !is_path_separator(path[offset + 2]))
            continue;

        return true;
    }

    return false;
}

function is_path_separator(c) {
    if (c == '/')
        return true;
    if (process.platform == 'win32' && c == '\\')
        return true;

    return false;
}

function determine_arch() {
    let arch = process.arch;

    if (arch == 'riscv32' || arch == 'riscv64') {
        let buf = read_file_header(process.execPath, 512);
        let header = decode_elf_header(buf);
        let float_abi = (header.e_flags & 0x6);

        switch (float_abi) {
            case 0: {} break;
            case 2: { arch += 'f'; } break;
            case 4: { arch += 'd'; } break;
            case 6: { arch += 'q'; } break;
        }
    } else if (arch == 'arm') {
        let buf = read_file_header(process.execPath, 512);
        let header = decode_elf_header(buf);

        if (header.e_flags & 0x400) {
            arch += 'hf';
        } else if (header.e_flags & 0x200) {
            arch += 'sf';
        } else {
            throw new Error('Unknown ARM floating-point ABI');
        }
    }

    return arch;
}

function read_file_header(filename, read) {
    let fd = null;

    try {
        let fd = fs.openSync(filename);

        let buf = Buffer.allocUnsafe(read);
        let len = fs.readSync(fd, buf);

        return buf.subarray(0, len);
    } finally {
        if (fd != null)
            fs.closeSync(fd);
    }
}

function decode_elf_header(buf) {
    let header = {};

    if (buf.length < 16)
        throw new Error('Truncated header');
    if (buf[0] != 0x7F || buf[1] != 69 || buf[2] != 76 || buf[3] != 70)
        throw new Error('Invalid magic number');
    if (buf[6] != 1)
        throw new Error('Invalid ELF version');
    if (buf[5] != 1)
        throw new Error('Big-endian architectures are not supported');

    let machine = buf.readUInt16LE(18);

    switch (machine) {
        case 3: { header.e_machine = 'ia32'; } break;
        case 40: { header.e_machine = 'arm'; } break;
        case 62: { header.e_machine = 'amd64'; } break;
        case 183: { header.e_machine = 'arm64'; } break;
        case 243: {
            switch (buf[4]) {
                case 1: { header.e_machine = 'riscv32'; } break;
                case 2: { header.e_machine = 'riscv64'; } break;
            }
        } break;
        default: throw new Error('Unknown ELF machine type');
    }

    switch (buf[4]) {
        case 1: { // 32 bit
            buf = buf.subarray(0, 68);
            if (buf.length < 68)
                throw new Error('Truncated ELF header');

            header.ei_class = 32;
            header.e_flags = buf.readUInt32LE(36);
        } break;
        case 2: { // 64 bit
            buf = buf.subarray(0, 120);
            if (buf.length < 120)
                throw new Error('Truncated ELF header');

            header.ei_class = 64;
            header.e_flags = buf.readUInt32LE(48);
        } break;
        default: throw new Error('Invalid ELF class');
    }

    return header;
}

function unlink_recursive(path) {
    try {
        if (fs.rmSync != null) {
            fs.rmSync(path, { recursive: true, maxRetries: process.platform == 'win32' ? 3 : 0 });
        } else {
            fs.rmdirSync(path, { recursive: true, maxRetries: process.platform == 'win32' ? 3 : 0 });
        }
    } catch (err) {
        if (err.code !== 'ENOENT')
            throw err;
    }
}

function get_napi_version(napi, major) {
    if (napi > 8)
        return null;

    // https://nodejs.org/api/n-api.html#node-api-version-matrix
    const support = {
        6:  ['6.14.2', '6.14.2', '6.14.2'],
        8:  ['8.6.0',  '8.10.0', '8.11.2'],
        9:  ['9.0.0',  '9.3.0',  '9.11.0'],
        10: ['10.0.0', '10.0.0', '10.0.0', '10.16.0', '10.17.0', '10.20.0', '10.23.0'],
        11: ['11.0.0', '11.0.0', '11.0.0', '11.8.0'],
        12: ['12.0.0', '12.0.0', '12.0.0', '12.0.0',  '12.11.0', '12.17.0', '12.19.0', '12.22.0'],
        13: ['13.0.0', '13.0.0', '13.0.0', '13.0.0',  '13.0.0'],
        14: ['14.0.0', '14.0.0', '14.0.0', '14.0.0',  '14.0.0',  '14.0.0',  '14.12.0', '14.17.0'],
        15: ['15.0.0', '15.0.0', '15.0.0', '15.0.0',  '15.0.0',  '15.0.0',  '15.0.0',  '15.12.0']
    };
    const max = Math.max(...Object.keys(support).map(k => parseInt(k, 10)));

    if (major > max)
        return major + '.0.0';
    if (support[major] == null)
        return null;

    let required = support[major][napi - 1] || null;
    return required;
}

// Ignores prerelease suffixes
function cmp_version(ver1, ver2) {
    ver1 = String(ver1).replace(/-.*$/, '').split('.').reduce((acc, v, idx) => acc + parseInt(v, 10) * Math.pow(10, 2 * (5 - idx)), 0);
    ver2 = String(ver2).replace(/-.*$/, '').split('.').reduce((acc, v, idx) => acc + parseInt(v, 10) * Math.pow(10, 2 * (5 - idx)), 0);

    let cmp = Math.min(Math.max(ver1 - ver2, -1), 1);
    return cmp;
}

module.exports = {
    download_http,
    extract_targz,
    path_is_absolute,
    path_has_dotdot,
    determine_arch,
    unlink_recursive,
    get_napi_version,
    cmp_version
};
