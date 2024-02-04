if __name__ == "__main__":
    import ctypes
    import os
    import sys
    import json
    import typing
    import time
    import traceback

    if len(sys.argv) != 4:
        sys.exit(-1)

    lib = ctypes.CDLL("/tmp/sandbox-python/python.so")
    module = sys.argv[1]
    code = open(module).read()

    def create_sandbox():
        os.chroot(".")
        os.chdir("/")

    def prtcl():
        lib.DifySeccomp.argtypes = []
        lib.DifySeccomp.restype = None
        lib.DifySeccomp()

    def drop_privileges(uid, gid):
        os.setgid(gid)
        os.setuid(uid)
    
    uid = int(sys.argv[2])
    gid = int(sys.argv[3])

    if not uid or not gid:
        sys.exit(-1)

    create_sandbox()
    prtcl()
    drop_privileges(uid, gid)

    # setup sys.excepthook
    def excepthook(type, value, tb):
        sys.stderr.write("".join(traceback.format_exception(type, value, tb)))
        sys.stderr.flush()
        sys.exit(-1)
    
    sys.excepthook = excepthook

    exec(code)