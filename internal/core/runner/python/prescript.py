import ctypes
import os
import sys
import traceback
from base64 import b64decode

# setup sys.excepthook
def excepthook(type, value, tb):
    sys.stderr.write("".join(traceback.format_exception(type, value, tb)))
    sys.stderr.flush()
    sys.exit(-1)

sys.excepthook = excepthook

lib = ctypes.CDLL("./python.so")
lib.DifySeccomp.argtypes = [ctypes.c_uint32, ctypes.c_uint32, ctypes.c_bool]
lib.DifySeccomp.restype = None

# argv: [script, running_path, key, uid, gid]
running_path = sys.argv[1]
if not running_path:
    exit(-1)

key = sys.argv[2]
if not key:
    exit(-1)
key = b64decode(key)

uid = int(sys.argv[3])
gid = int(sys.argv[4])

os.chdir(running_path)

# Read payload from stdin: 3 base64-encoded lines
# 1. enable_network ("0" or "1", base64-encoded)
# 2. preload code (base64-encoded, empty string if none)
# 3. XOR-encrypted user code (base64-encoded)
raw_input = sys.stdin.read()
sys.stdin.close()

lines = raw_input.split('\n')
enable_network_b64 = lines[0] if len(lines) > 0 else ''
preload_b64 = lines[1] if len(lines) > 1 else ''
code_b64 = lines[2] if len(lines) > 2 else ''

enable_network = b64decode(enable_network_b64).decode('utf-8') == '1'

# Execute preload BEFORE seccomp
preload_code = b64decode(preload_b64).decode('utf-8') if preload_b64 else ''
if preload_code:
    exec(preload_code)

lib.DifySeccomp(uid, gid, enable_network)

# Decrypt and execute user code AFTER seccomp
code = b64decode(code_b64)

def decrypt(code, key):
    key_len = len(key)
    code_len = len(code)
    code = bytearray(code)
    for i in range(code_len):
        code[i] = code[i] ^ key[i % key_len]
    return bytes(code)

code = decrypt(code, key)
exec(code)
