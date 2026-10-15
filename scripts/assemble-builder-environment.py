"""Build-only assembly. Copies an allowlisted runtime, never the service root."""
import importlib.metadata, json, os, pathlib, shutil, subprocess
root = pathlib.Path('/environment-root')
root.mkdir(mode=0o755)
def copy(src, dest=None):
    target = root / (dest or src).lstrip('/')
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(src, target, follow_symlinks=True)
copy('/usr/local/bin/python3.12', '/usr/bin/python3')
copy('/usr/local/bin/node', '/usr/bin/node')
shutil.copytree('/usr/local/lib/python3.12', root/'usr/local/lib/python3.12', ignore=shutil.ignore_patterns('__pycache__','pip','pip-*.dist-info','ensurepip'))
shutil.copytree('/build/node', root/'opt/node', ignore=shutil.ignore_patterns('.package-lock.json'))
copy('/build/python.so','/var/sandbox/sandbox-python/python.so')
copy('/build/nodejs.so','/var/sandbox/sandbox-nodejs/nodejs.so')
p = root/'var/sandbox/sandbox-nodejs/nodejs-project'
p.mkdir()
(p/'node_temp').symlink_to('/opt/node')
copy('/etc/ssl/certs/ca-certificates.crt')
(root/'usr/lib/ssl').mkdir(parents=True)
(root/'usr/lib/ssl/cert.pem').symlink_to('/etc/ssl/certs/ca-certificates.crt')
# ldd resolves transitive ELF requirements on this exact architecture. Every
# resolved path is copied as a regular file (no host-facing loader symlinks).
for p in list(root.rglob('*')):
    if not p.is_file() or p.is_symlink(): continue
    with p.open('rb') as f: elf = f.read(4) == b'\x7fELF'
    if not elf: continue
    output = subprocess.run(['ldd', str(p)], text=True, capture_output=True)
    if 'not found' in output.stdout: raise RuntimeError(output.stdout)
    for line in output.stdout.splitlines():
        for word in line.split():
            if word.startswith('/') and pathlib.Path(word).is_file(): copy(word)
(root/'work').mkdir()
(root/'provenance').mkdir()
shutil.copytree('/usr/share/doc',root/'provenance/debian-licenses')
copy('/build/node-LICENSE','/provenance/node-LICENSE')
for name in ['python.lock','python.versions','node-package.json','node-package-lock.json','sources.json']:
    copy('/build/'+name, '/provenance/'+name)
metadata = []
for dist in importlib.metadata.distributions():
    if dist.metadata['Name'].lower() in ('pip','setuptools'): continue
    metadata.append({'name':dist.metadata['Name'],'version':dist.version,'license':dist.metadata.get('License-Expression') or dist.metadata.get('License'),'license_files':[str(f) for f in (dist.files or []) if 'license' in str(f).lower() or 'copying' in str(f).lower()]})
(root/'provenance/python-distributions.json').write_text(json.dumps(sorted(metadata,key=lambda x:x['name']),indent=2)+'\n')
(root/'provenance/debian-packages.txt').write_text(subprocess.check_output(['dpkg-query','-W','-f=${Package}\t${Version}\t${Architecture}\n'],text=True))
(root/'provenance/versions.json').write_text(json.dumps({'python':subprocess.check_output(['/usr/local/bin/python3','--version'],text=True).strip(),'node':subprocess.check_output(['node','--version'],text=True).strip(),'machine':os.uname().machine},indent=2)+'\n')
# Remove write permission for every runtime file, preserving executable bits.
for p in root.rglob('*'):
    if not p.is_symlink(): p.chmod(p.stat().st_mode & ~0o222)
root.chmod(0o555)
