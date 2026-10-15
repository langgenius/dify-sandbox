"""Native bootstrap dependency component test, not namespace/cgroup qualification.
Run only inside a finite disposable network-none container with ordinary Docker
capabilities. The launch helper uses the actual IsolatedLaunch implementation.
"""
import json, os, pathlib, subprocess
root=pathlib.Path('/environment-root')
manifest=json.loads((root/'environment-manifest.json').read_text())
subprocess.run(['/environment-manifest','-verify',manifest['Digest']],check=True)
(root/'work').chmod(0o777)
failures=[]
for language,filename in [('python3','python.py'),('nodejs','node.js')]:
    launch=json.loads(subprocess.check_output(['/dependency-launch',language]))
    (root/'work/bootstrap').write_text(launch['Bootstrap'])
    script=pathlib.Path('/probes/'+filename).read_bytes()
    if language=="python3":
        script+=b"\nimport socket, errno\nfor family in (socket.AF_INET,socket.AF_INET6,socket.AF_UNIX):\n    try: socket.socket(family,socket.SOCK_STREAM)\n    except OSError as e: assert e.errno == errno.EPERM\n    else: raise AssertionError('socket creation permitted')\n"
    else:
        script+=b"\nfor(const family of [2,10,1]) {assert.strictEqual(koffi.load('libc.so.6').func('int socket(int,int,int)')(family,1,0),-1);assert.strictEqual(koffi.errno(),1); }\n"
    read,write=os.pipe()
    assert read==3,read
    os.write(write,script);os.close(write)
    env={'PATH':'/usr/bin','HOME':'/work','TMPDIR':'/work','LANG':'C.UTF-8','PYTHONDONTWRITEBYTECODE':'1','PYTHONHOME':'/usr/local','LD_LIBRARY_PATH':'/usr/local/lib','NODE_PATH':'/opt/node/node_modules'}
    result=subprocess.run(['/usr/sbin/chroot',str(root),launch['Executable'],*launch['Args']],pass_fds=(read,),env=env,capture_output=True,text=True,timeout=15)
    os.close(read)
    print(language,result.returncode,repr(result.stdout),repr(result.stderr),flush=True)
    if result.returncode!=0 or json.loads(result.stdout)!={"sum":6,"unicode":"你好","array":[1,True,None],"object":{"x":2}}:failures.append(language)
assert not failures,failures
# Direct creation through socketpair, and connection on even an invalid FD,
# remain kill-denied by the original isolated default action. These probes never
# possess a usable socket and never send a request outside this container.
for language in ['python3','nodejs']:
    launch=json.loads(subprocess.check_output(['/dependency-launch',language]))
    (root/'work/bootstrap').write_text(launch['Bootstrap'])
    for operation in ['socketpair','connect']:
        if language=='python3':
            script = ("import ctypes\nlib=ctypes.CDLL('libc.so.6')\n" + ("lib.socketpair(1,1,0,ctypes.create_string_buffer(8))" if operation=='socketpair' else "lib.connect(-1,None,0)")).encode()
        else:
            script = ("const k=require('koffi'),lib=k.load('libc.so.6');" + ("lib.func('int socketpair(int,int,int,void *)')(1,1,0,Buffer.alloc(8));" if operation=='socketpair' else "lib.func('int connect(int,void *,int)')(-1,null,0);")).encode()
        read,write=os.pipe();assert read==3;os.write(write,script);os.close(write)
        result=subprocess.run(['/usr/sbin/chroot',str(root),launch['Executable'],*launch['Args']],pass_fds=(read,),env=env,capture_output=True,text=True,timeout=15)
        os.close(read)
        assert result.returncode==-31,(language,operation,result.returncode,result.stderr)
        print(language+': '+operation+' remains denied',flush=True)
