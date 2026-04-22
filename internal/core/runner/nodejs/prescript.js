const argv = process.argv
const fs = require('fs')

const koffi = require('koffi')
const lib = koffi.load('./var/sandbox/sandbox-nodejs/nodejs.so')
const difySeccomp = lib.func('int DifySeccomp(int, int, bool)')

const uid = parseInt(argv[2])
const gid = parseInt(argv[3])

const options = JSON.parse(argv[4])

const ret = difySeccomp(uid, gid, options['enable_network'])

if (ret !== 0) {
    const errorMessages = {
        1: "Chroot failed",
        2: "Chdir failed", 
        3: "Set no new privs failed",
        4: "Seccomp failed",
        5: "Setuid failed",
        6: "Setgid failed",
        7: "Setgroups failed",
        99: "Unknown error",
    }
    const errorMsg = errorMessages[ret] || `Unknown error code: ${ret}`
    console.error(`DifySeccomp failed: ${errorMsg}`)
    process.exit(-1)
}

const code = fs.readFileSync(3, 'utf8')
eval(code)
