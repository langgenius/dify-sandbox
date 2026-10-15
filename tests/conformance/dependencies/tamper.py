"""Actual-image verifier regression; intentionally mutates only this disposable
container's artifact. No code dispatch and no kernel qualification claim."""
import json, os, pathlib, statistics, subprocess, time
root=pathlib.Path('/environment-root')
manifest=json.loads((root/'environment-manifest.json').read_text())
def verify():
    return subprocess.run(['/environment-manifest','-verify',manifest['Digest']],capture_output=True,text=True)
measure=[]
cases={}
for _ in range(3):
    start=time.monotonic();result=verify();assert result.returncode==0,result.stderr;measure.append(time.monotonic()-start)
print(json.dumps({'digest':manifest['Digest'],'files':manifest['Files'],'bytes':manifest['Bytes'],'verify_seconds':measure,'median_seconds':statistics.median(measure)}),flush=True)
for relative in ['usr/bin/python3','usr/bin/node','usr/local/lib/python3.12/site-packages/requests/__init__.py','opt/node/node_modules/koffi/index.js','var/sandbox/sandbox-python/python.so','var/sandbox/sandbox-nodejs/nodejs.so']:
    target=root/relative;original=target.read_bytes();mode=target.stat().st_mode
    target.chmod(mode|0o200);target.write_bytes(original+b'\nmutated')
    result=verify();assert result.returncode!=0,relative
    target.write_bytes(original);target.chmod(mode)
    assert verify().returncode==0,relative
    print('mutation refused: '+relative,flush=True)
    cases[relative]='rejected-and-restored'
link=root/'usr/lib/ssl/cert.pem';target=os.readlink(link);link.unlink();link.symlink_to('/usr/bin/python3')
assert verify().returncode!=0
link.unlink();link.symlink_to(target)
assert verify().returncode==0
print('symlink drift refused',flush=True)
cases['usr/lib/ssl/cert.pem']='rejected-and-restored'
if os.environ.get('CS5_DRIFT_OUTPUT'):
    pathlib.Path(os.environ['CS5_DRIFT_OUTPUT']).write_text(json.dumps({'environment_digest':manifest['Digest'],'cases':cases},sort_keys=True))
