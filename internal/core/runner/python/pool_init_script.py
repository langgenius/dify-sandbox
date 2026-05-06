#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
dify-sandbox Python pool worker script.

This script is started once and kept alive. It reads JSON-encoded execution
requests line-by-line from stdin, runs each piece of user code in an isolated
namespace, then writes a single-line JSON result to stdout.

Protocol
--------
stdin  (one line per request):
  {"code": "<b64-xor-encrypted>", "key": "<b64-key>", "preload": "<b64-preload>",
   "enable_network": false}

stdout (one line per response):
  {"stdout": "...", "stderr": "...", "error": null|"<msg>"}

Security note
-------------
DifySeccomp(uid, gid, enable_network=False) is called ONCE at process startup
via the SANDBOX_UID / SANDBOX_GID environment variables set by the Go pool
runner.  seccomp filters are one-way; arming them multiple times is not allowed.
"""

import sys
import json
import os
import traceback
import builtins
from base64 import b64decode
from contextlib import redirect_stdout, redirect_stderr
from io import StringIO


# ---------------------------------------------------------------------------
# Optional seccomp support — called ONCE at process startup, not per-request.
# ---------------------------------------------------------------------------
def _arm_seccomp() -> None:
    uid = int(os.environ.get('SANDBOX_UID', '65537'))
    gid = int(os.environ.get('SANDBOX_GID', '0'))
    try:
        import ctypes
        lib = ctypes.CDLL("./python.so")
        lib.DifySeccomp.argtypes = [ctypes.c_uint32, ctypes.c_uint32, ctypes.c_bool]
        lib.DifySeccomp.restype = None
        lib.DifySeccomp(ctypes.c_uint32(uid), ctypes.c_uint32(gid), ctypes.c_bool(False))
    except Exception:
        pass  # seccomp not available in this environment


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _decrypt(buf: bytes, key: bytes) -> bytes:
    out = bytearray(buf)
    klen = len(key)
    for i in range(len(out)):
        out[i] ^= key[i % klen]
    return bytes(out)


def _restricted_import(name, globals=None, locals=None, fromlist=(), level=0):
    forbidden = {"subprocess", "fcntl", "ctypes"}
    if name.split(".")[0] in forbidden:
        raise ImportError(f"import of '{name.split('.')[0]}' is blocked")
    return __import__(name, globals, locals, fromlist, level)


# ---------------------------------------------------------------------------
# Safe builtins namespace for user code
# ---------------------------------------------------------------------------
_SAFE_BUILTINS = {
    # constants
    "True": True, "False": False, "None": None,
    "NotImplemented": NotImplemented, "Ellipsis": Ellipsis,
    # types
    "bool": bool, "int": int, "float": float, "complex": complex,
    "str": str, "bytes": bytes, "bytearray": bytearray,
    "list": list, "tuple": tuple, "set": set, "frozenset": frozenset,
    "dict": dict, "type": type, "object": object,
    # functions
    "abs": abs, "min": min, "max": max, "sum": sum, "round": round,
    "pow": pow, "divmod": divmod, "len": len, "range": range,
    "enumerate": enumerate, "zip": zip, "all": all, "any": any,
    "sorted": sorted, "reversed": reversed, "iter": iter, "next": next,
    "map": map, "filter": filter, "isinstance": isinstance,
    "issubclass": issubclass, "callable": callable, "hash": hash,
    "id": builtins.id, "repr": repr, "str": str, "print": print,
    "open": open,
    # OOP
    "__build_class__": builtins.__build_class__,
    "super": super, "property": property,
    "staticmethod": staticmethod, "classmethod": classmethod,
    # exceptions
    **{name: getattr(builtins, name) for name in dir(builtins)
       if isinstance(getattr(builtins, name), type) and
       issubclass(getattr(builtins, name), BaseException)},
    # import
    "__import__": _restricted_import,
}


# ---------------------------------------------------------------------------
# User-code executor
# ---------------------------------------------------------------------------

def _execute(code_str: str, preload: str, enable_network: bool) -> dict:
    stdout_buf = StringIO()
    stderr_buf = StringIO()
    error = None

    try:
        with redirect_stdout(stdout_buf), redirect_stderr(stderr_buf):
            g = {"__name__": "__user__", "__builtins__": _SAFE_BUILTINS}

            if preload:
                exec(preload, g)  # noqa: S102

            exec(code_str, g)  # noqa: S102

    except Exception:
        error = traceback.format_exc()

    return {
        "stdout": stdout_buf.getvalue(),
        "stderr": stderr_buf.getvalue(),
        "error": error,
    }


# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------

def main():
    # Arm seccomp once before accepting any requests.
    _arm_seccomp()

    # Signal to the Go pool runner that this process is ready.
    sys.stderr.write("PYTHON_POOL_READY\n")
    sys.stderr.flush()

    for raw_line in sys.stdin:
        line = raw_line.strip()
        if not line:
            continue

        try:
            data = json.loads(line)

            key = b64decode(data["key"])
            code = _decrypt(b64decode(data["code"]), key).decode("utf-8")

            preload = ""
            if data.get("preload"):
                preload = _decrypt(b64decode(data["preload"]), key).decode("utf-8")

            enable_network = bool(data.get("enable_network", False))

            response = _execute(code, preload, enable_network)

        except Exception:
            response = {
                "stdout": "",
                "stderr": traceback.format_exc(),
                "error": "protocol error",
            }

        sys.stdout.write(json.dumps(response) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
