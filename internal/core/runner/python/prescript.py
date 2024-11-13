import ctypes
import os
import sys
import traceback
# setup sys.excepthook
def excepthook(type, value, tb):
    sys.stderr.write("".join(traceback.format_exception(type, value, tb)))
    sys.stderr.flush()
    sys.exit(-1)

sys.excepthook = excepthook

lib = ctypes.CDLL("./python.so")
lib.DifySeccomp.argtypes = [ctypes.c_uint32, ctypes.c_uint32, ctypes.c_bool]
lib.DifySeccomp.restype = None

# get running path
running_path = sys.argv[1]
if not running_path:
    exit(-1)

# get decrypt key
key = sys.argv[2]
if not key:
    exit(-1)

from base64 import b64decode
key = b64decode(key)

os.chdir(running_path)

{{preload}}

lib.DifySeccomp({{uid}}, {{gid}}, {{enable_network}})

code = b64decode("{{code}}")

def decrypt(code, key):
    key_len = len(key)
    code_len = len(code)
    code = bytearray(code)
    for i in range(code_len):
        code[i] = code[i] ^ key[i % key_len]
    return bytes(code)

code = decrypt(code, key)
exec(code)