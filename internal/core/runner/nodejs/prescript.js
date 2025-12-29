const argv = process.argv

const koffi = require('koffi')
const lib = koffi.load('./var/sandbox/sandbox-nodejs/nodejs.so')
const difySeccomp = lib.func('void DifySeccomp(int, int, bool)')

const uid = parseInt(argv[2])
const gid = parseInt(argv[3])

const options = JSON.parse(argv[4])

difySeccomp(uid, gid, options['enable_network'])

// Configure CA certificates for HTTPS in chroot environment
const fs = require('fs')
const https = require('https')

try {
  const caCert = fs.readFileSync('/etc/ssl/certs/ca-certificates.crt', 'utf8')

  // Configure https module
  https.globalAgent.options.ca = caCert

  // Configure undici for fetch - use setGlobalDispatcher
  // In Node.js 18+, undici is available via globalThis
  if (globalThis.undici) {
    const dispatcher = globalThis.undici.setGlobalDispatcher
    if (typeof dispatcher === 'function') {
      const Agent = globalThis.undici.Agent
      const agent = new Agent({
        connect: { ca: caCert }
      })
      dispatcher(agent)
    }
  }
} catch (e) {
  // Ignore errors
}


