const fs = require('fs')
const koffi = require('koffi')
const lib = koffi.load('./var/sandbox/sandbox-nodejs/nodejs.so')
const difySeccomp = lib.func('void DifySeccomp(int, int, bool)')

const uid = parseInt(process.argv[2])
const gid = parseInt(process.argv[3])
const options = JSON.parse(process.argv[4])

// Read payload from stdin (sync): 2 base64-encoded lines
// 1. preload code (base64-encoded, empty string if none)
// 2. user code (base64-encoded)
const input = fs.readFileSync(0, 'utf-8')
const lines = input.split('\n')
const preloadB64 = lines[0] || ''
const codeB64 = lines[1] || ''

// Execute preload BEFORE seccomp
if (preloadB64) {
  eval(Buffer.from(preloadB64, 'base64').toString('utf-8'))
}

difySeccomp(uid, gid, options['enable_network'])

// Execute user code AFTER seccomp
eval(Buffer.from(codeB64, 'base64').toString('utf-8'))
