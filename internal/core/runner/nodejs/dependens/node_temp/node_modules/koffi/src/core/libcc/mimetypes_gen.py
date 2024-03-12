#!/usr/bin/env python3

# Copyright 2023 Niels Martignène <niels.martignene@protonmail.com>
#
# Permission is hereby granted, free of charge, to any person obtaining a copy of
# this software and associated documentation files (the “Software”), to deal in 
# the Software without restriction, including without limitation the rights to use,
# copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the
# Software, and to permit persons to whom the Software is furnished to do so,
# subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND,
# EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
# OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
# NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
# HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
# WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
# FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
# OTHER DEALINGS IN THE SOFTWARE.

# This script uses the database of mimetypes distributed here: https://github.com/jshttp/mime-db
# to produce the X-header file mimetypes.inc

import os
import argparse
import json

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description = 'Update mimetypes include file')
    parser.add_argument('-O', '--output_file', dest = 'output_file', action = 'store', help = 'Output file')
    parser.add_argument('json', help = 'Source JSON database')
    args = parser.parse_args()

    if args.output_file is None:
        output_file = os.path.join(os.path.dirname(__file__), 'mimetypes.inc')
    else:
        output_file = args.output_file

    with open(args.json) as f:
        db = json.load(f)

    extensions = {}
    for (k, v) in db.items():
        if 'extensions' not in v:
            continue

        for ext in v['extensions']:
            if not ext in extensions:
                extensions[ext] = 'application/x-'
            if extensions[ext].startswith('application/x-'):
                extensions[ext] = k

    extensions = [(k, extensions[k]) for k in sorted(extensions.keys())]
    extensions.sort()

    with open(output_file) as f:
        lines = f.readlines()
    with open(output_file, 'w') as f:
        for line in lines:
            if not line.startswith('//'):
                break
            f.write(line)

        print('', file = f)
        print('#ifndef MIMETYPE', file = f)
        print('    #error Please define MIMETYPE() before including mimetypes.inc', file = f)
        print('#endif', file = f)
        print('', file = f)

        for k, v in extensions:
            print(f'MIMETYPE(".{k}", "{v}")', file = f)

        print('', file = f)
        print('#undef MIMETYPE', file = f)
