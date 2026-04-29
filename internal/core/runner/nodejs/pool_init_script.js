#!/usr/bin/env node
/**
 * dify-sandbox Node.js pool worker script.
 *
 * Protocol (same as the Python counterpart):
 *
 * stdin  one JSON line per request:
 *   {"code":"<b64-xor>","key":"<b64>","preload":"<b64-xor>","enable_network":false}
 *
 * stdout one JSON line per response:
 *   {"stdout":"...","stderr":"...","error":null|"<msg>"}
 *
 * Isolation is provided by isolated-vm which runs each snippet inside a fresh
 * V8 Isolate with a configurable memory limit.
 */

'use strict';

let ivm;
try {
    ivm = require('isolated-vm');
} catch (e) {
    process.stderr.write('dify-sandbox: isolated-vm not available: ' + e.message + '\n');
    process.exit(1);
}

const readline = require('readline');

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
// Execute one snippet inside an isolated-vm Isolate.
// ---------------------------------------------------------------------------

async function execute(codeStr, preload, enableNetwork) {
    const stdoutLines = [];
    const stderrLines = [];
    let result = null;
    let error = null;

    const isolate = new ivm.Isolate({ memoryLimit: 128 });
    try {
        const context = await isolate.createContext();
        const global = context.global;

        // Inject console shim via host references.
        const logRef   = new ivm.Reference((...args) => { stdoutLines.push(args.join(' ')); });
        const errorRef = new ivm.Reference((...args) => { stderrLines.push(args.join(' ')); });
        await global.set('__hostLog',   logRef);
        await global.set('__hostError', errorRef);

        await context.eval(`
            globalThis.console = {
                log:   (...a) => { try { __hostLog.applySync(undefined,   a.map(String)); } catch(_){} },
                error: (...a) => { try { __hostError.applySync(undefined, a.map(String)); } catch(_){} },
                warn:  (...a) => { try { __hostError.applySync(undefined, a.map(String)); } catch(_){} },
                info:  (...a) => { try { __hostLog.applySync(undefined,   a.map(String)); } catch(_){} },
            };
        `);

        // Execute preload, then user code.
        const fullCode = preload ? preload + '\n\n' + codeStr : codeStr;
        await (await isolate.compileScript(fullCode)).run(context, { timeout: 30000 });

    } catch (e) {
        error = e.stack || e.message || String(e);
    } finally {
        try { isolate.dispose(); } catch (_) {}
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

async function main() {
    process.stderr.write('NODEJS_POOL_READY\n');

    const rl = readline.createInterface({ input: process.stdin, terminal: false });

    for await (const rawLine of rl) {
        const line = rawLine.trim();
        if (!line) continue;

        let response;
        try {
            const data = JSON.parse(line);
            const key         = Buffer.from(data.key,  'base64');
            const code        = decrypt(Buffer.from(data.code, 'base64'), key).toString('utf-8');
            const preload     = data.preload
                ? decrypt(Buffer.from(data.preload, 'base64'), key).toString('utf-8')
                : '';
            const enableNet   = Boolean(data.enable_network);
            response = await execute(code, preload, enableNet);
        } catch (e) {
            response = { stdout: '', stderr: '', error: 'protocol error: ' + (e.message || String(e)) };
        }

        process.stdout.write(JSON.stringify(response) + '\n');
    }
}

main().catch(err => {
    process.stderr.write('nodejs pool worker fatal: ' + err + '\n');
    process.exit(1);
});
