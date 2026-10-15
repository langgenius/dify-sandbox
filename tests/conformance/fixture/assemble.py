"""Assemble observed qualification data. Does not sign or grant capability."""
import hashlib
import json
import os
import pathlib
import platform
import subprocess
import time

root=pathlib.Path('/var/lib/dify-builder-ci')
def canonical(v):
    return json.dumps(v,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()
def digest(v):
    return 'sha256:'+hashlib.sha256(canonical(v)).hexdigest()
identity=json.loads((root/'identity.json').read_text())
profile=json.loads((root/'qualification/profile.json').read_text())
report=json.loads((root/'artifacts/case-report.json').read_text())
measured=json.loads((root/'artifacts/budget-candidate.json').read_text())
assert report['failed']==report['skipped']==report['xfailed']==0
assert measured['budget']==profile['Limits']
arch={'x86_64':'amd64','aarch64':'arm64'}[platform.machine()]
assert report['architecture']==arch==identity['architecture']
unit=pathlib.Path('/etc/systemd/system/dify-builder-candidate.service').read_text()
lsm=pathlib.Path('/etc/apparmor.d/dify-builder-ci').read_text()
pid=int(subprocess.run(['systemctl','show','dify-builder-candidate.service','--property=MainPID','--value'],capture_output=True,text=True,check=True).stdout)
assert pid>1
assert pathlib.Path(f'/proc/{pid}/attr/current').read_text().strip()=='dify-builder-ci (enforce)'
status=pathlib.Path(f'/proc/{pid}/status').read_text()
assert '\nNoNewPrivs:\t1\n' in status and '\nSeccomp:\t2\n' in status
mounts=[]
for line in pathlib.Path(f'/proc/{pid}/mountinfo').read_text().splitlines():
    fields=line.split()
    if fields[4] in ('/','/environment-root','/qualification','/state','/observations','/delegated'):
        mounts.append(dict(path=fields[4],vfs_options=fields[5]))
assert {'/qualification','/state','/observations','/delegated'} <= {m['path'] for m in mounts}
assert any(m['path']=='/qualification' and 'ro' in m['vfs_options'].split(',') for m in mounts)
now=int(time.time())
result=dict(schema_version=1,profile_version='builder-native-test-v1',
    qualification_id=identity['ci_run']+'/'+arch,revocation_id=identity['ci_run']+'/'+arch+'/revocation',
    issued_at=now,expires_at=now+86400,source_revision=identity['source_revision'],
    image=identity['image'],runtime=dict(os='linux',architecture=arch,host_architecture=arch,
      kernel=platform.release(),runtime=identity['runtime'],seccomp_digest=digest(dict(unit=unit,apparmor=lsm)),
      cgroup_version=2,environment_digest='sha256:'+profile['EnvironmentDigest'],profile_digest='sha256:'+profile['Digest']),
    effective=dict(configuration=digest(dict(profile=profile,unit=unit)),mounts=digest(mounts),
      network=digest(json.loads((root/'qualification/network.json').read_text())),resources=digest(profile['Limits'])),
    budget=measured['budget'],measurements=measured['measurements'],
    required_case_ids=report['case_ids'],case_results=report['case_results'],skipped=0,failed=0,xfailed=0,
    ci=dict(run_id=identity['ci_run'],test_revision=identity['source_revision']))
print(canonical(result).decode(),end='')
