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
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');
const tools = require('./tools.js');

const DefaultOptions = {
    mode: 'RelWithDebInfo'
};

function Builder(config = {}) {
    let self = this;

    let app_dir = config.app_dir;
    let project_dir = config.project_dir;
    let package_dir = config.package_dir;

    if (app_dir == null)
        app_dir = __dirname.replace(/\\/g, '/') + '/..';
    if (project_dir == null)
        project_dir = process.cwd();
    app_dir = app_dir.replace(/\\/g, '/');
    project_dir = project_dir.replace(/\\/g, '/');
    if (package_dir == null)
        package_dir = find_parent_directory(project_dir, 'package.json');
    if (package_dir != null)
        package_dir = package_dir.replace(/\\/g, '/');

    let runtime_version = config.runtime_version;
    let arch = config.arch;
    let toolset = config.toolset || null;
    let prefer_clang = config.prefer_clang || false;
    let mode = config.mode || DefaultOptions.mode;
    let targets = config.targets || [];
    let verbose = config.verbose || false;
    let prebuild = config.prebuild || false;

    if (runtime_version == null)
        runtime_version = process.version;
    if (runtime_version.startsWith('v'))
        runtime_version = runtime_version.substr(1);
    if (arch == null)
        arch = tools.determine_arch();

    let options = null;

    let cache_dir = get_cache_directory();
    let build_dir = config.build_dir;
    let work_dir = null;

    if (build_dir == null) {
        let options = read_cnoke_options();

        if (options.output != null) {
            build_dir = expand_path(options.output, options.directory);
        } else {
            build_dir = project_dir + '/build';
        }
    }
    build_dir = build_dir.replace(/\\/g, '/');
    work_dir = build_dir + `/v${runtime_version}_${arch}`;

    let cmake_bin = null;

    this.configure = async function(retry = true) {
        let options = read_cnoke_options();

        let args = [project_dir];

        check_cmake();
        check_compatibility();

        console.log(`>> Node: ${runtime_version}`);
        console.log(`>> Target: ${process.platform}_${arch}`);

        // Prepare build directory
        fs.mkdirSync(build_dir, { recursive: true, mode: 0o755 });
        fs.mkdirSync(work_dir, { recursive: true, mode: 0o755 });

        retry &= fs.existsSync(work_dir + '/CMakeCache.txt');

        // Download or use Node headers
        if (options.api == null) {
            let destname = `${cache_dir}/node-v${runtime_version}-headers.tar.gz`;

            if (!fs.existsSync(destname)) {
                fs.mkdirSync(cache_dir, { recursive: true, mode: 0o755 });

                let url = `https://nodejs.org/dist/v${runtime_version}/node-v${runtime_version}-headers.tar.gz`;
                await tools.download_http(url, destname);
            }

            await tools.extract_targz(destname, work_dir + '/headers', 1);

            args.push(`-DNODE_JS_INCLUDE_DIRS=${work_dir}/headers/include/node`);
        } else {
            console.log(`>> Using local node-api headers`);
            args.push(`-DNODE_JS_INCLUDE_DIRS=${options.api}/include`);
        }

        // Download or create Node import library (Windows)
        if (process.platform == 'win32') {
            if (options.api == null) {
                let dirname;
                switch (arch) {
                    case 'ia32': { dirname = 'win-x86'; } break;
                    case 'x64': { dirname = 'win-x64'; } break;
                    case 'arm64': { dirname = 'win-arm64'; } break;

                    default: {
                        throw new Error(`Unsupported architecture '${arch}' for Node on Windows`);
                    } break;
                }

                let destname = `${cache_dir}/node_v${runtime_version}_${arch}.lib`;

                if (!fs.existsSync(destname)) {
                    fs.mkdirSync(cache_dir, { recursive: true, mode: 0o755 });

                    let url = `https://nodejs.org/dist/v${runtime_version}/${dirname}/node.lib`;
                    await tools.download_http(url, destname);
                }

                fs.copyFileSync(destname, work_dir + '/node.lib');
            } else {
                args.push(`-DNODE_JS_LINK_DEF=${options.api}/def/node_api.def`);
            }
        }

        args.push(`-DCMAKE_MODULE_PATH=${app_dir}/assets`);

        // Set platform flags
        switch (process.platform) {
            case 'win32': {
                fs.copyFileSync(`${app_dir}/assets/win_delay_hook.c`, work_dir + '/win_delay_hook.c');

                args.push(`-DNODE_JS_SOURCES=${work_dir}/win_delay_hook.c`);
                args.push(`-DNODE_JS_LINK_LIB=${work_dir}/node.lib`);

                switch (arch) {
                    case 'ia32': {
                        args.push('-DNODE_JS_LINK_FLAGS=/DELAYLOAD:node.exe;/SAFESEH:NO');
                        args.push('-A', 'Win32');
                    } break;
                    case 'arm64': {
                        args.push('-DNODE_JS_LINK_FLAGS=/DELAYLOAD:node.exe;/SAFESEH:NO');
                        args.push('-A', 'ARM64');
                    } break;
                    case 'x64': {
                        args.push('-DNODE_JS_LINK_FLAGS=/DELAYLOAD:node.exe');
                        args.push('-A', 'x64');
                    } break;
                }
            } break;

            case 'darwin': {
                args.push('-DNODE_JS_LINK_FLAGS=-undefined;dynamic_lookup');

                switch (arch) {
                    case 'arm64': { args.push('-DCMAKE_OSX_ARCHITECTURES=arm64'); } break;
                    case 'x64': { args.push('-DCMAKE_OSX_ARCHITECTURES=x86_64'); } break;
                }
            } break;
        }

        if (process.platform != 'win32') {
            // Prefer Ninja if available
            if (spawnSync('ninja', ['--version']).status === 0)
                args.push('-G', 'Ninja');

            // Use CCache if available
            if (spawnSync('ccache', ['--version']).status === 0) {
                args.push('-DCMAKE_C_COMPILER_LAUNCHER=ccache');
                args.push('-DCMAKE_CXX_COMPILER_LAUNCHER=ccache');
            }
        }

        if (prefer_clang) {
            if (process.platform == 'win32') {
                args.push('-T', 'ClangCL');
            } else {
                args.push('-DCMAKE_C_COMPILER=clang');
                args.push('-DCMAKE_CXX_COMPILER=clang++');
            }
        }
        if (toolset != null)
            args.push('-T', toolset);

        args.push(`-DCMAKE_BUILD_TYPE=${mode}`);
        for (let type of ['ARCHIVE', 'RUNTIME', 'LIBRARY']) {
            for (let suffix of ['', '_DEBUG', '_RELEASE', '_RELWITHDEBINFO'])
                args.push(`-DCMAKE_${type}_OUTPUT_DIRECTORY${suffix}=${build_dir}`);
        }
        args.push('--no-warn-unused-cli');

        console.log('>> Running configuration');

        let proc = spawnSync(cmake_bin, args, { cwd: work_dir, stdio: 'inherit' });
        if (proc.status !== 0) {
            tools.unlink_recursive(work_dir);
            if (retry)
                return self.configure(false);

            throw new Error('Failed to run configure step');
        }
    };

    this.build = async function() {
        check_compatibility();

        if (prebuild) {
            let valid = await check_prebuild();
            if (valid)
                return;
        }

        check_cmake();

        if (!fs.existsSync(work_dir + '/CMakeCache.txt'))
            await self.configure();

        // In case Make gets used
        if (process.env.MAKEFLAGS == null)
            process.env.MAKEFLAGS = '-j' + os.cpus().length;

        let args = [
            '--build', work_dir,
            '--config', mode
        ];

        if (verbose)
            args.push('--verbose');
        for (let target of targets)
            args.push('--target', target);

        console.log('>> Running build');

        let proc = spawnSync(cmake_bin, args, { stdio: 'inherit' });
        if (proc.status !== 0)
            throw new Error('Failed to run build step');
    };

    async function check_prebuild() {
        let options = read_cnoke_options();

        if (options.prebuild != null) {
            fs.mkdirSync(build_dir, { recursive: true, mode: 0o755 });

            let archive_filename = expand_path(options.prebuild, options.directory);

            if (fs.existsSync(archive_filename)) {
                try {
                    console.log('>> Extracting prebuilt binaries...');
                    await tools.extract_targz(archive_filename, build_dir, 1);
                } catch (err) {
                    console.error('Failed to find prebuilt binary for your platform, building manually');
                }
            } else {
                console.error('Cannot find local prebuilt archive');
            }
        }

        if (options.require != null) {
            let require_filename = expand_path(options.require, options.directory);

            if (fs.existsSync(require_filename)) {
                let proc = spawnSync(process.execPath, ['-e', 'require(process.argv[1])', require_filename]);
                if (proc.status === 0)
                    return true;
            }

            console.error('Failed to load prebuilt binary, rebuilding from source');
        }

        return false;
    }

    this.clean = function() {
        tools.unlink_recursive(build_dir);
    };

    function find_parent_directory(dirname, basename)
    {
        if (process.platform == 'win32')
            dirname = dirname.replace(/\\/g, '/');

        do {
            if (fs.existsSync(dirname + '/' + basename))
                return dirname;

            dirname = path.dirname(dirname);
        } while (!dirname.endsWith('/'));

        return null;
    }

    function get_cache_directory() {
        if (process.platform == 'win32') {
            let cache_dir = process.env['LOCALAPPDATA'] || process.env['APPDATA'];
            if (cache_dir == null)
                throw new Error('Missing LOCALAPPDATA and APPDATA environment variable');

            cache_dir = path.join(cache_dir, 'cnoke');
            return cache_dir;
        } else {
            let cache_dir = process.env['XDG_CACHE_HOME'];

            if (cache_dir == null) {
                let home = process.env['HOME'];
                if (home == null)
                    throw new Error('Missing HOME environment variable');

                cache_dir = path.join(home, '.cache');
            }

            cache_dir = path.join(cache_dir, 'cnoke');
            return cache_dir;
        }
    }

    function check_cmake() {
        if (cmake_bin != null)
            return;

        // Check for CMakeLists.txt
        if (!fs.existsSync(project_dir + '/CMakeLists.txt'))
            throw new Error('This directory does not appear to have a CMakeLists.txt file');

        // Check for CMake
        {
            let proc = spawnSync('cmake', ['--version']);

            if (proc.status === 0) {
                cmake_bin = 'cmake';
            } else {
                if (process.platform == 'win32') {
                    // I really don't want to depend on anything in CNoke, and Node.js does not provide
                    // anything to read from the registry. This is okay, REG.exe exists since Windows XP.
                    let proc = spawnSync('reg', ['query', 'HKEY_LOCAL_MACHINE\\SOFTWARE\\Kitware\\CMake', '/v', 'InstallDir']);

                    if (proc.status === 0) {
                        let matches = proc.stdout.toString('utf-8').match(/InstallDir[ \t]+REG_[A-Z_]+[ \t]+(.*)+/);

                        if (matches != null) {
                            let bin = path.join(matches[1].trim(), 'bin\\cmake.exe');

                            if (fs.existsSync(bin))
                                cmake_bin = bin;
                        }
                    }
                }

                if (cmake_bin == null)
                    throw new Error('CMake does not seem to be available');
            }
        }

        console.log(`>> Using CMake binary: ${cmake_bin}`);
    }

    function check_compatibility() {
        let options = read_cnoke_options();

        if (options.node != null && tools.cmp_version(runtime_version, options.node) < 0)
            throw new Error(`Project ${options.name} requires Node.js >= ${options.node}`);

        if (options.napi != null) {
            let major = parseInt(runtime_version, 10);
            let required = tools.get_napi_version(options.napi, major);

            if (required == null)
                throw new Error(`Project ${options.name} does not support the Node ${major}.x branch (old or missing N-API)`);
            if (tools.cmp_version(runtime_version, required) < 0)
                throw new Error(`Project ${options.name} requires Node >= ${required} in the Node ${major}.x branch (with N-API >= ${options.napi})`);
        }
    }

    function read_cnoke_options() {
        if (options != null)
            return options;

        let directory = project_dir;
        let pkg = null;
        let cnoke = null;

        if (package_dir != null) {
            try {
                let json = fs.readFileSync(package_dir + '/package.json', { encoding: 'utf-8' });

                pkg = JSON.parse(json);
                directory = package_dir;
            } catch (err) {
                if (err.code != 'ENOENT')
                    throw err;
            }
        }

        try {
            let json = fs.readFileSync(project_dir + '/CNoke.json', { encoding: 'utf-8' });

            cnoke = JSON.parse(json);
            directory = project_dir;
        } catch (err) {
            if (err.code != 'ENOENT')
                throw err;
        }

        if (cnoke == null)
            cnoke = pkg?.cnoke ?? {};

        options = {
            name: pkg?.name ?? path.basename(project_dir),
            version: pkg?.version ?? null,

            directory: directory,
            ...cnoke
        };

        return options;
    }

    function expand_path(str, root_dir) {
        let expanded = str.replace(/{{ *([a-zA-Z_][a-zA-Z_0-9]*) *}}/g, (match, p1) => {
            switch (p1) {
                case 'version': {
                    let options = read_cnoke_options();
                    return options.version || '';
                } break;
                case 'platform': return process.platform;
                case 'arch': return arch;

                default: return match;
            }
        });

        if (!tools.path_is_absolute(expanded))
            expanded = path.join(root_dir, expanded);

        return expanded;
    }
}

module.exports = {
    Builder,
    DefaultOptions
};
