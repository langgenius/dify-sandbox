#!/usr/bin/env node

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

const fs = require('fs');
const cnoke = require('./src/index.js');

const VALID_COMMANDS = ['build', 'configure', 'clean'];

main();

async function main() {
    let config = {};
    let command = 'build';

    // Parse options
    {
        let i = 2;

        if (process.argv.length >= 3 && process.argv[2][0] != '-') {
            let cmd = process.argv[2];

            if (VALID_COMMANDS.includes(cmd)) {
                command = cmd;
                i++;
            }
        }

        for (; i < process.argv.length; i++) {
            let arg = process.argv[i];
            let value = null;

            if (arg[0] == '-') {
                if (arg.length > 2 && arg[1] != '-') {
                    value = arg.substr(2);
                    arg = arg.substr(0, 2);
                } else if (arg[1] == '-') {
                    let offset = arg.indexOf('=');

                    if (offset > 2 && arg.length > offset + 1) {
                        value = arg.substr(offset + 1);
                        arg = arg.substr(0, offset);
                    }
                }
                if (value == null && process.argv[i + 1] != null && process.argv[i + 1][0] != '-') {
                    value = process.argv[i + 1];
                    i++; // Skip this value next iteration
                }
            }

            if (arg == '--help') {
                print_usage();
                return;
            } else if (arg == '-d' || arg == '--directory') {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);

                config.project_dir = fs.realpathSync(value);
            } else if (arg == '-p' || arg == '--package') {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);

                config.package_dir = fs.realpathSync(value);
            } else if (arg == '-O' || arg == '--out') {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);

                config.output_directory = value;
            } else if ((command == 'build' || command == 'configure') && (arg == '-v' || arg == '--runtime-version')) {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);
                if (!value.match(/^[0-9]+\.[0-9]+\.[0-9]+$/))
                    throw new Error(`Malformed runtime version '${value}'`);

                config.runtime_version = value;
            } else if ((command == 'build' || command == 'configure') && (arg == '-a' || arg == '--arch')) {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);

                config.arch = value;
            } else if ((command == 'build' || command == 'configure') && (arg == '-t' || arg == '--toolset')) {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);

                config.toolset = value;
            } else if ((command == 'build' || command == 'configure') && (arg == '-C' || arg == '--prefer-clang')) {
                config.prefer_clang = true;
            } else if ((command == 'build' || command == 'configure') && (arg == '-B' || arg == '--config')) {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);

                switch (value.toLowerCase()) {
                    case 'relwithdebinfo': { config.mode = 'RelWithDebInfo'; } break;
                    case 'debug': { config.mode = 'Debug'; } break;
                    case 'release': { config.mode = 'Release'; } break;

                    default: {
                        throw new Error(`Unknown value '${value}' for ${arg}`);
                    } break;
                }
            } else if ((command == 'build' || command == 'configure') && (arg == '-D' || arg == '--debug')) {
                config.mode = 'Debug';
            } else if (command == 'build' && arg == '--verbose') {
                config.verbose = true;
            } else if (command == 'build' && arg == '--prebuild') {
                config.prebuild = true;
            } else if (command == 'build' && (arg == '-T' || arg == '--target')) {
                if (value == null)
                    throw new Error(`Missing value for ${arg}`);

                config.targets = [value];
            } else {
                if (arg[0] == '-') {
                    throw new Error(`Unexpected argument '${arg}'`);
                } else {
                    throw new Error(`Unexpected value '${arg}'`);
                }
            }
        }
    }

    try {
        let builder = new cnoke.Builder(config);
        await builder[command]();
    } catch (err) {
        console.error(err);
        process.exit(1);
    }
}

function print_usage() {
    let help = `Usage: cnoke [command] [options...]

Commands:
    configure                            Configure CMake build
    build                                Build project (configure if needed)
    clean                                Clean build files

Options:
    -d, --directory <DIR>                Change source directory
                                         (default: current working directory)
    -p, --package <DIR>                  Change package directory
                                         (default: current working directory)

    -O, --out <DIR>                      Set explicit output directory
                                         (default: ./build)

    -B, --config <CONFIG>                Change build type: RelWithDebInfo, Debug, Release
                                         (default: ${cnoke.DefaultOptions.mode})
    -D, --debug                          Shortcut for --config Debug

        --prebuild                       Use prebuilt binary if available

    -a, --arch <ARCH>                    Change architecture and ABI
                                         (default: ${cnoke.determine_arch()})
    -v, --runtime-version <VERSION>      Change node version
                                         (default: ${process.version})
    -t, --toolset <TOOLSET>              Change default CMake toolset
    -C, --prefer-clang                   Use Clang instead of default CMake compiler

    -T, --target <TARGET>                Only build the specified target

        --verbose                        Show build commands while building

The ARCH value is similar to process.arch, with the following differences:

- arm is changed to arm32hf or arm32sf depending on the floating-point ABI used (hard-float, soft-float)
- riscv32 is changed to riscv32sf, riscv32hf32, riscv32hf64 or riscv32hf128 depending on the floating-point ABI
- riscv64 is changed to riscv64sf, riscv64hf32, riscv64hf64 or riscv64hf128 depending on the floating-point ABI`;

    console.log(help);
}
