"""Disposable image probe of the unchanged ordinary HTTP execution owner.
No published port, external request, host mount or Builder qualification claim.
"""
import json, os, pathlib, subprocess, time, urllib.request
pathlib.Path('/conf').mkdir(exist_ok=True)
pathlib.Path('/dependencies').mkdir(exist_ok=True)
pathlib.Path('/dependencies/python-requirements.txt').write_text('')
pathlib.Path('/conf/config.yaml').write_text('''app:
  port: 8194
  debug: false
  key: cs3-local-fixture
max_workers: 4
max_requests: 50
worker_timeout: 15
python_path: /usr/local/bin/python3
nodejs_path: /usr/local/bin/node
enable_network: false
enable_preload: true
python_deps_update_interval: 30m
''')
libdir = '/lib/' + ('aarch64-linux-gnu' if os.uname().machine == 'aarch64' else 'x86_64-linux-gnu')
env = dict(os.environ, SANDBOX_PROFILE='ordinary', PYTHON_PATH='/usr/local/bin/python3', NODEJS_PATH='/usr/local/bin/node', PYTHON_LIB_PATH=','.join(['/usr/local/lib/python3.12',libdir,'/usr/local/lib/libpython3.12.so.1.0','/etc/ssl/certs/ca-certificates.crt','/usr/lib/ssl/cert.pem']))
log = open('/tmp/ordinary.log','w+')
server = subprocess.Popen(['/main'],env=env,stdout=log,stderr=log,cwd='/')
try:
    for _ in range(150):
        if server.poll() is not None: raise RuntimeError('ordinary server exited')
        log.flush();log.seek(0)
        if 'python dependencies sandbox initialized' in log.read(): break
        time.sleep(.1)
    else: raise RuntimeError('ordinary preparation did not finish')
    scripts = {
      'python3': "import requests,httpx,socks,json\nfrom jinja2 import Environment\nassert Environment().from_string('{% for x in xs %}{{x|upper}}{% endfor %}').render(xs=['a','é'])=='AÉ'\nprint(json.dumps({'sum':sum([1,2,3]),'unicode':'你好','array':[1,True,None],'object':{'x':2}},ensure_ascii=False,separators=(',',':')))",
      'nodejs': "const assert=require('assert'),koffi=require('koffi');assert.strictEqual(koffi.load('libc.so.6').func('int abs(int)')(-42),42);console.log(JSON.stringify({sum:6,unicode:'你好',array:[1,true,null],object:{x:2}}));"
    }
    expected = {'sum':6,'unicode':'你好','array':[1,True,None],'object':{'x':2}}
    failures=[]
    if os.environ.get("CS3_DIAGNOSTIC"):
        scripts={"python3":os.environ["CS3_DIAGNOSTIC"]}
    for language,code in scripts.items():
        body=json.dumps(dict(language=language,code=code,preload='',enable_network=False)).encode()
        request=urllib.request.Request('http://127.0.0.1:8194/v1/sandbox/run',body,{'Content-Type':'application/json','X-Api-Key':'cs3-local-fixture'})
        with urllib.request.urlopen(request,timeout=20) as response: result=json.load(response)
        print(language, result, flush=True)
        if os.environ.get("CS3_DIAGNOSTIC"): continue
        if result['code']!=0 or result['data']['error'] or json.loads(result['data']['stdout'])!=expected:
            failures.append(language)
            continue
        print(language+': ordinary native imports/output matched',flush=True)
    assert not failures,failures
finally:
    server.terminate()
    try: server.wait(5)
    except subprocess.TimeoutExpired: server.kill();server.wait()
    log.seek(0);print(log.read())
