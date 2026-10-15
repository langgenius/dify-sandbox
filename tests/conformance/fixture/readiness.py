"""CI-only bounded listener and applied confinement readiness. No user payload."""
import json
import pathlib
import pwd
import subprocess
import time

root=pathlib.Path('/var/lib/dify-builder-ci')
for _ in range(100):
    candidate=subprocess.run(['systemctl','show','dify-builder-candidate.service','--property=MainPID','--value'],capture_output=True,text=True,check=True).stdout.strip()
    if candidate.isdigit() and int(candidate)>1:
        try:
            label=pathlib.Path('/proc/'+candidate+'/attr/current').read_text().strip()
            if label != 'dify-builder-ci (enforce)':
                raise RuntimeError('actual AppArmor identity mismatch: '+label)
            # The route-less outer namespace has only loopback plus documentation
            # addresses. Readiness remains distinct from the positive case probes.
            subprocess.run(['ip','netns','exec','dify-builder-ci','python3','-c',
                "import socket,urllib.request,json; r=json.load(urllib.request.urlopen('http://127.0.0.1:19000/',timeout=1)); assert r['boot_id']; assert len(r['counters'])==14; s=socket.create_connection(('127.0.0.1',8194),1);s.close();s=socket.create_connection(('127.0.0.1',8195),1);s.close()"],check=True,capture_output=True,timeout=5)
            placement=pathlib.Path('/proc/'+candidate+'/cgroup').read_text().strip()
            assert placement=='0::/system.slice/dify-builder-candidate.service/manager',placement
            status=pathlib.Path('/proc/'+candidate+'/status').read_text()
            assert '\nNoNewPrivs:\t1\n' in status and '\nSeccomp:\t2\n' in status
            fields=dict(line.split(':',1) for line in status.splitlines() if ':' in line)
            uid=pwd.getpwnam('dify-builder-ci').pw_uid
            assert uid > 0 and set(map(int,fields['Uid'].split()))=={uid},'dedicated nonroot candidate required'
            expected_caps=sum(1<<n for n in (21,18,7,6,1))
            assert all(int(fields[k].strip(),16)==expected_caps for k in ('CapEff','CapPrm','CapAmb','CapBnd')),'candidate capability set mismatch'
            controllers=pathlib.Path('/sys/fs/cgroup/system.slice/dify-builder-candidate.service/scopes/cgroup.controllers').read_text().split()
            assert all(x in controllers for x in ('cpu','memory','pids'))
            break
        except (OSError,subprocess.CalledProcessError,subprocess.TimeoutExpired):
            pass
    time.sleep(.2)
else:
    raise RuntimeError('qualified fixture readiness did not complete')
