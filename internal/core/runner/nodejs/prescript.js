const argv = process.argv

const koffi = require('koffi')
const lib = koffi.load('./var/sandbox/sandbox-nodejs/nodejs.so')
const difySeccomp = lib.func('void DifySeccomp(int, int, bool)')

const uid = parseInt(argv[2])
const gid = parseInt(argv[3])

const options = JSON.parse(argv[4])

difySeccomp(uid, gid, options['enable_network'])

