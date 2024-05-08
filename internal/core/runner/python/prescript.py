if __name__ == "__main__":
    import ctypes
    import os
    import sys
    import json
    import time
    import traceback

    # setup sys.excepthook
    def excepthook(type, value, tb):
        sys.stderr.write("".join(traceback.format_exception(type, value, tb)))
        sys.stderr.flush()
        sys.exit(-1)
    
    sys.excepthook = excepthook

    if len(sys.argv) != 5:
        sys.exit(-1)

    lib = ctypes.CDLL("/tmp/sandbox-python/python.so")
    module = sys.argv[1]
    code = open(module).read()

    def sandbox(uid, gid, enable_network):
        lib.DifySeccomp.argtypes = [ctypes.c_uint32, ctypes.c_uint32, ctypes.c_bool]
        lib.DifySeccomp.restype = None
        lib.DifySeccomp(uid, gid, enable_network)
    
    uid = int(sys.argv[2])
    gid = int(sys.argv[3])

    if not uid or not gid:
        sys.exit(-1)

    options = json.loads(sys.argv[4])

    sandbox(uid, gid, options.get("enable_network", False))

    exec(code)
