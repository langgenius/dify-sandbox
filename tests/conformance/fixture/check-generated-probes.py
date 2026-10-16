"""Disposable, filter-only diagnostic. Executed exclusively inside Linux.

The host orchestrates this container and execs the attacker as initial UID 65537.
No privileges, namespace claims, production entrypoints or filter overrides.
"""
import hashlib
import json
import os
import select
import signal
import stat
import subprocess
import sys
import time

ROOT = "/diagnostic"
FILTER = ROOT + "/probe-filter.so"
BUNDLE = json.load(open(ROOT + "/generated.json"))


def identity(pid, uid):
    stat = open(f"/proc/{pid}/stat").read().rsplit(") ", 1)[1].split()
    status = dict(line.split(":", 1) for line in open(f"/proc/{pid}/status") if ":" in line)
    assert [int(x) for x in status["Uid"].split()] == [uid] * 4
    assert all(int(status[k], 16) == 0 for k in ("CapEff", "CapPrm", "CapAmb"))
    assert int(status["NoNewPrivs"]) == 1
    return dict(pid=pid, start=stat[19], state=stat[0], uid=uid,
                capabilities={k: status[k].strip() for k in ("CapEff", "CapPrm", "CapAmb")},
                nnp=1)


def launch(language, mode, code, name):
    path = f"/tmp/{name}-{os.getuid()}.code"
    with open(path, "w") as f:
        f.write(code)
    if language == "python3":
        wrapper = ("import ctypes\n"
                   f"f=ctypes.CDLL({FILTER!r}).CS5Filter\n"
                   "f.argtypes=[ctypes.c_int,ctypes.c_int];f.restype=ctypes.c_int\n"
                   f"assert f(1,{int(mode == 'scoped')})==0\n"
                   "print('CS5_FILTER_LOADED',flush=True)\n"
                   f"exec(compile(open({path!r}).read(),'<generated>','exec'))\n")
        command = ["python3", "-u", "-c", wrapper]
    else:
        wrapper = ("const k=require('koffi');const f=k.load(" + json.dumps(FILTER) + ").func('int CS5Filter(int,int)');"
                   f"if(f(2,{int(mode == 'scoped')})!==0)throw Error('filter unavailable');"
                   "console.log('CS5_FILTER_LOADED');eval(require('fs').readFileSync(" + json.dumps(path) + ",'utf8'));")
        command = ["node", "-e", wrapper]
    env = dict(os.environ, NODE_PATH="/environment-root/opt/node/node_modules")
    return subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env), hashlib.sha256(code.encode()).hexdigest()


def ready(child, uid, marker):
    lines = []
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if select.select([child.stdout], [], [], .05)[0]:
            line = child.stdout.readline()
            if line:
                lines.append(line.strip())
                if line.strip() == marker:
                    return identity(child.pid, uid), lines
        if child.poll() is not None:
            raise RuntimeError(f"generated child exit={child.returncode}, stdout={lines}, stderr={child.stderr.read()[:2048]}")
    raise RuntimeError("generated child readiness deadline exceeded")


def terminate(child):
    # Popen owns this exact unreaped child, so its PID cannot be reused here.
    if child.poll() is None:
        child.terminate()
        try:
            child.wait(timeout=1)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait(timeout=1)


def serve(language, mode, control):
    assert os.geteuid() == 0
    code = BUNDLE[language+"/"+mode]["victim"]
    if control:
        code = "import signal,time\nopen('/work/custody-private','w').write('component-control')\ncount=0\nopen('/work/custody-signals','w').write('0')\ndef s(*a):\n global count\n count+=1\n open('/work/custody-signals','w').write(str(count))\nsignal.signal(signal.SIGUSR1,s)\nprint('CS5_CUSTODY_VICTIM_READY',flush=True)\nwhile True: time.sleep(.05)"
        # A clearly labelled unfiltered control victim permits pre-repair
        # attacker SIGSYS attribution independently of the broken victim code.
        child = subprocess.Popen(["python3", "-u", "-c", code], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        digest = hashlib.sha256(code.encode()).hexdigest()
    else:
        child, digest = launch(language, mode, code, "victim")
    try:
        before, lines = ready(child, 0, "CS5_CUSTODY_VICTIM_READY")
        before["userns"] = os.readlink(f"/proc/{child.pid}/ns/user")
        before["pidns"] = os.readlink(f"/proc/{child.pid}/ns/pid")
        private = open('/work/custody-private').read()
        private_mode=stat.S_IMODE(os.stat('/work/custody-private').st_mode)
        if not control:
            assert private_mode==0o400
        assert identity(child.pid, 0)["start"] == before["start"]
        os.kill(child.pid, signal.SIGUSR1)
        until = time.monotonic() + 2
        while time.monotonic() < until and open('/work/custody-signals').read() != '1':
            time.sleep(.01)
        assert open('/work/custody-signals').read() == '1'
        record = dict(before=before, mode=mode, victim_language=language, victim_code_sha256=digest,
                      filter_sha256=hashlib.sha256(open(FILTER,'rb').read()).hexdigest(), control_victim=control,
                      private_mode=oct(private_mode), bundle_sha256=hashlib.sha256(open(ROOT+'/generated.json','rb').read()).hexdigest())
        with open('/tmp/owned-victim.json','w') as f:
            json.dump(record,f)
        os.chmod('/tmp/owned-victim.json',0o444)
        print(json.dumps(dict(ready=True, **record)),flush=True)
        until = time.monotonic()+12
        while time.monotonic()<until and not os.path.exists('/tmp/attack-result.json'):
            assert child.poll() is None
            time.sleep(.02)
        attack=json.load(open('/tmp/attack-result.json'))
        after=identity(child.pid,0)
        assert after['start']==before['start'] and after['state']!='Z'
        assert open('/work/custody-signals').read()=='1'
        assert open('/work/custody-private').read()==private
        print(json.dumps(dict(before=before,after=after,attack=attack,control_victim=control)),flush=True)
        assert attack['exit']==0
        assert attack['report']['self_signals']==1 and attack['report']['self_read']=='CS5 self-positive'
        assert attack['report']['signal_errno']==1
        report=attack['report']
        assert report['signal_pid']==report['signal_tid']==before['pid']
        assert report['signal_start']==before['start']
        assert report['signal_userns']==before['userns'] and report['signal_pidns']==before['pidns']
    finally:
        terminate(child)


def attack(language):
    assert os.geteuid()==65537
    owned=json.load(open('/tmp/owned-victim.json')); target=owned['before']
    code=BUNDLE[language+"/"+owned['mode']]['attacker'].replace('2147483000',str(target['pid']))
    code=code.replace('CS5_COMPONENT_START',target['start']).replace('CS5_COMPONENT_USERNS',target['userns']).replace('CS5_COMPONENT_PIDNS',target['pidns'])
    child,digest=launch(language,owned['mode'],code,'attacker')
    result=dict(code_sha256=digest,language=language,mode=owned['mode'],target=target)
    try:
        seen,lines=ready(child,65537,'CS1_READY');result['identity']=seen
        out,err=child.communicate(timeout=3);result.update(exit=child.returncode,stdout=lines+[out],stderr=err[:2048])
        result['report']=json.loads(out.strip())
    except Exception as exc:
        result.update(exit=child.poll(),error=str(exc))
    finally:
        terminate(child)
        with open('/tmp/attack-result.json.tmp','w') as f:json.dump(result,f)
        os.rename('/tmp/attack-result.json.tmp','/tmp/attack-result.json')
    print(json.dumps(result),flush=True)
    assert result.get('exit')==0 and 'report' in result


if sys.argv[1]=='serve':serve(sys.argv[2],sys.argv[3],len(sys.argv)>4 and sys.argv[4]=='control')
elif sys.argv[1]=='attack':attack(sys.argv[2])
else:raise RuntimeError('fixed diagnostic mode required')
