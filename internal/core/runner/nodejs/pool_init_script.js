#!/usr/bin/env node
/**
 * dify-sandbox Node.js pool worker script.
 *
 * Keeps the process alive, reads JSON requests line-by-line from stdin,
 * executes user code with the same koffi+nodejs.so seccomp isolation as
 * the original fork-mode prescript.js, then writes a single-line JSON
 * response to stdout.
 *
 * Protocol
 * --------
 * stdin  (one JSON line per request):
 *   {
 *     "code":           "<b64-xor-encrypted user code>",
 *     "key":            "<b64 key>",
 *     "preload":        "<b64-xor-encrypted preload | omit>",
 *     "enable_network": false,
 *     "uid":            65537,
 *     "gid":            65537
 *   }
 *
 * stdout (one JSON line per response):
 *   { "stdout": "...", "stderr": "...", "error": null | "<msg>" }
 *
 * Security note
 * -------------
 * DifySeccomp(uid, gid, enable_network) is called ONCE at process startup
 * (before the request loop begins) via the SANDBOX_UID / SANDBOX_GID /
 * SANDBOX_ENABLE_NETWORK environment variables set by the Go pool runner.
 * seccomp filters are one-way — arming them multiple times is not allowed.
 */

'use strict';

// ---------------------------------------------------------------------------
// koffi + nodejs.so  (same as prescript.js in fork mode)
// ---------------------------------------------------------------------------

const LIB_PATH = '/var/sandbox/sandbox-nodejs/nodejs.so';

// Call DifySeccomp once at startup.  uid/gid/enable_network are provided by
// the Go pool runner via environment variables so they never change per-request.
const _sandboxUid    = parseInt(process.env.SANDBOX_UID    || '65537', 10);
const _sandboxGid    = parseInt(process.env.SANDBOX_GID    || '0',     10);
const _enableNetwork = process.env.SANDBOX_ENABLE_NETWORK === '1';

try {
    const koffi = require('koffi');
    const lib = koffi.load(LIB_PATH);
    const difySeccomp = lib.func('void DifySeccomp(int, int, bool)');
    difySeccomp(_sandboxUid, _sandboxGid, _enableNetwork);
} catch (e) {
    // Running without seccomp (dev / test environment without .so)
    process.stderr.write('nodejs pool: koffi/DifySeccomp not available: ' + e.message + '\n');
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function decrypt(buf, key) {
    const out = Buffer.from(buf);
    const klen = key.length;
    for (let i = 0; i < out.length; i++) {
        out[i] ^= key[i % klen];
    }
    return out;
}

// ---------------------------------------------------------------------------
// Execute one snippet.
// Stdout/stderr from user code are captured via console shim + redirect.
// ---------------------------------------------------------------------------

function execute(codeStr, preload) {
    const stdoutLines = [];
    const stderrLines = [];
    let error = null;

    // Redirect console before calling user code.
    const origLog   = console.log;
    const origError = console.error;
    const origWarn  = console.warn;
    const origInfo  = console.info;

    console.log   = (...a) => stdoutLines.push(a.map(String).join(' '));
    console.error = (...a) => stderrLines.push(a.map(String).join(' '));
    console.warn  = (...a) => stderrLines.push(a.map(String).join(' '));
    console.info  = (...a) => stdoutLines.push(a.map(String).join(' '));

    try {
        const fullCode = preload ? preload + '\n\n' + codeStr : codeStr;
        // eslint-disable-next-line no-eval
        eval(fullCode); // noqa: eval
    } catch (e) {
        error = e.stack || e.message || String(e);
    } finally {
        console.log   = origLog;
        console.error = origError;
        console.warn  = origWarn;
        console.info  = origInfo;
    }

    return {
        stdout: stdoutLines.join('\n'),
        stderr: stderrLines.join('\n'),
        error:  error,
    };
}

// ---------------------------------------------------------------------------
// Main loop
// ---------------------------------------------------------------------------

process.stderr.write('NODEJS_POOL_READY\n');

const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (rawLine) => {
    const line = rawLine.trim();
    if (!line) return;

    let response;
    try {
        const data = JSON.parse(line);

        const key     = Buffer.from(data.key, 'base64');
        const code    = decrypt(Buffer.from(data.code, 'base64'), key).toString('utf-8');
        const preload = data.preload
            ? decrypt(Buffer.from(data.preload, 'base64'), key).toString('utf-8')
            : '';

        response = execute(code, preload);
    } catch (e) {
        response = {
            stdout: '',
            stderr: '',
            error: 'protocol error: ' + (e.message || String(e)),
        };
    }

    process.stdout.write(JSON.stringify(response) + '\n');
});

rl.on('close', () => process.exit(0));
