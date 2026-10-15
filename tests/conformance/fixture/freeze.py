"""Freeze measured finite limits only after owned candidate has fully stopped."""
import hashlib
import json
import pathlib
import shutil
import subprocess

root=pathlib.Path('/var/lib/dify-builder-ci')
state=subprocess.run(['systemctl','show','dify-builder-candidate.service','--property=ActiveState','--value'],capture_output=True,text=True,check=True).stdout.strip()
assert state=='inactive',state
work=root/'state/work'
assert not list(work.iterdir()),'unclean execution roots prevent profile freeze'
# No running processes/cgroups remain: systemd completed stopping the entire unit.
assert not pathlib.Path('/sys/fs/cgroup/system.slice/dify-builder-candidate.service').exists()
artifact=json.loads((root/'artifacts/budget-candidate.json').read_text())
profile=json.loads((root/'qualification/profile.json').read_text())
profile['Limits']=artifact['budget']
for key in ('MaxConcurrent','MaxQueueDepth'):
    profile[key]=artifact['budget']['concurrent' if key=='MaxConcurrent' else 'queue_depth']
profile.pop('Digest')
profile['Digest']=hashlib.sha256(json.dumps(profile,sort_keys=True,separators=(',',':')).encode()).hexdigest()
path=root/'qualification/profile.json'
path.chmod(0o600);path.write_text(json.dumps(profile,sort_keys=True));path.chmod(0o444)
# Only finite private conformance tombstones and leases from the stopped owner.
for name in ('journal','journal.uids'):
    path=root/'state'/name
    if path.exists():
        assert path.is_dir() and not path.is_symlink()
        shutil.rmtree(path)
# Lifecycle probe has fixed IDs. Fresh observation directory is part of the new
# candidate generation; retain measurement observations in a separate artifact.
shutil.copytree(root/'observations',root/'artifacts/measurement-observations')
for path in (root/'observations').iterdir():
    assert path.is_file() and not path.is_symlink()
    path.unlink()
