import ctypes
import os
import sys
import json
import traceback

# setup sys.excepthook
def excepthook(type, value, tb):
    sys.stderr.write("".join(traceback.format_exception(type, value, tb)))
    sys.stderr.flush()
    sys.exit(-1)

sys.excepthook = excepthook

lib = ctypes.CDLL("/var/sandbox/sandbox-python/python.so")
lib.DifySeccomp.argtypes = [ctypes.c_uint32, ctypes.c_uint32, ctypes.c_bool]
lib.DifySeccomp.restype = None


import json
import os

import json
import sys
import traceback
import os

os.chdir("/var/sandbox/sandbox-python")

lib.DifySeccomp(65537, 1001, 1)

# declare main function here
def main() -> dict:
    original_strings_with_empty = ["apple", "", "cherry", "date", "", "fig", "grape", "honeydew", "kiwi", "", "mango", "nectarine", "orange", "papaya", "quince", "raspberry", "strawberry", "tangerine", "ugli fruit", "vanilla bean", "watermelon", "xigua", "yellow passionfruit", "zucchini"] * 5

    extended_strings = []

    for s in original_strings_with_empty:
        if s: 
            repeat_times = 600
            extended_s = (s * repeat_times)[:3000]
            extended_strings.append(extended_s)
        else:
            extended_strings.append(s)
    
    return {
        "result": extended_strings,
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
