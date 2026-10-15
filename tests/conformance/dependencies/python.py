# Runs inside the actual baked root, as the native sandbox UID, without network.
import ctypes, hashlib, httpx, importlib.util, json, os, requests, socks, ssl
from jinja2 import Environment
assert requests.__version__ == '2.32.3'
assert httpx.__version__ == '0.27.2'
with httpx.Client(proxy='socks5://127.0.0.1:9'):
    pass  # Constructing a client tests socksio; never makes a request.
assert Environment().from_string('{% for x in xs %}{{ x|upper }}{% endfor %}').render(xs=['a', 'é']) == 'AÉ'
assert hashlib.sha256('你好'.encode()).hexdigest() == '670d9743542cae3ea7ebe36af56bd53648b0a1126162e78d81a32934a711302e'
assert ssl.create_default_context().cert_store_stats()['x509_ca'] > 0
ctypes.CDLL('/var/sandbox/sandbox-python/python.so')
assert importlib.util.find_spec('pip') is None
assert not os.path.exists('/conf') and not os.path.exists('/qualification')
assert not os.path.exists('/var/run/docker.sock')
for name in ['/usr/local/lib/python3.12/site-packages/requests/__init__.py', '/usr/bin/python3']:
    try:
        open(name, 'ab')
    except PermissionError:
        pass
    else:
        raise AssertionError('writable dependency: ' + name)
print(json.dumps({'sum':sum([1,2,3]),'unicode':'你好','array':[1,True,None],'object':{'x':2}},ensure_ascii=False,separators=(',',':')))
