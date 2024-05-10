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

lib = ctypes.CDLL("/tmp/sandbox-python/python.so")
lib.DifySeccomp.argtypes = [ctypes.c_uint32, ctypes.c_uint32, ctypes.c_bool]
lib.DifySeccomp.restype = None


import json
import base64
import subprocess
import os

import requests
from netrc import netrc, NetrcParseError
import urllib3
import socket
import json
import datetime
from datetime import datetime
datetime.strptime('2021-01-01', '%Y-%m-%d')
import math
import random
import re
import string
import sys
import time
import traceback
import uuid
import os
import base64
import hashlib
import hmac
import binascii
import collections
import functools
import operator
import itertools

os.chdir("/")

lib.DifySeccomp(65537, 1001, 1)

# declare main function here
def main() -> dict:
    import requests
    return {
        "result": requests.get("https://bilibili.com").text,
    }

from json import loads, dumps
from base64 import b64decode

# execute main function, and return the result
# inputs is a dict, and it
inputs = b64decode('e30=').decode('utf-8')
output = main(**json.loads(inputs))

# convert output to json and print
output = dumps(output, indent=4)

result = f'''<<RESULT>>
{output}
<<RESULT>>'''

print(result)
