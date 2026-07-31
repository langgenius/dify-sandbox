import ctypes
import os
import sys
import traceback


def _preload_supported_runtime_modules(enable_network):
    # These supported features load native extension modules. Import them while
    # the bootstrap is still trusted so later user imports do not need a new
    # executable mapping after seccomp is installed.
    import base64
    import datetime
    import json
    import zoneinfo

    if enable_network:
        try:
            import httpx
        except ModuleNotFoundError:
            pass

        try:
            import requests
        except ModuleNotFoundError:
            pass


_preload_supported_runtime_modules(bool({{enable_network}}))
del _preload_supported_runtime_modules


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

os.chdir(running_path)

{{preload}}

lib.DifySeccomp({{uid}}, {{gid}}, {{enable_network}})
os.environ.pop("GODEBUG", None)

with os.fdopen(3, "rb") as code_fd:
    code = code_fd.read().decode("utf-8")

exec(compile(code, "<fd3>", "exec"))
