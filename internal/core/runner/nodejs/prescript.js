const argv = process.argv

const koffi = require('koffi')
const lib = koffi.load('/tmp/sandbox-nodejs/nodejs.so')
const difySeccomp = lib.func('void DifySeccomp(int, int)')

const uid = parseInt(argv[2])
const gid = parseInt(argv[3])

difySeccomp(uid, gid)

