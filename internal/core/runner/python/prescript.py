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
lib.DifySeccomp.restype = ctypes.c_int

# get running path
running_path = sys.argv[1]
if not running_path:
    exit(-1)

os.chdir(running_path)

{{preload}}

'''
	SUCCESS = iota
	ERR_CHROOT
	ERR_CHDIR
	ERR_SETNONEWPRIVS
	ERR_SECCOMP
	ERR_SETUID
	ERR_SETGID
	ERR_UNKNOWN
'''

ret = lib.DifySeccomp({{uid}}, {{gid}}, {{enable_network}})
if ret != 0:
    error_messages = {
        1: "Chroot failed",
        2: "Chdir failed", 
        3: "Set no new privs failed",
        4: "Seccomp failed",
        5: "Setuid failed",
        6: "Setgid failed",
        7: "Unknown error",
    }
    error_msg = error_messages.get(ret, f"Unknown error code: {ret}")
    sys.stderr.write(f"DifySeccomp failed: {error_msg}\n")
    sys.stderr.flush()
    sys.exit(-1)

with os.fdopen(3, "rb") as code_fd:
    code = code_fd.read().decode("utf-8")

exec(compile(code, "<fd3>", "exec"))
