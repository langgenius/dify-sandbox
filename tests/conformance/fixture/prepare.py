"""Protected fixture material only. Run by normal disposable Linux CI owner."""
import hashlib
import json
import pathlib
import secrets
import sys

root = pathlib.Path('/var/lib/dify-builder-ci')
manifest = json.loads((root / 'image/environment-root/environment-manifest.json').read_text())
key = secrets.token_hex(32)
budget = dict(input_bytes=2<<20, code_bytes=1<<20, stdout_bytes=1<<20,
              stderr_bytes=1<<20, disk_bytes=64<<20, memory_bytes=512<<20,
              pids=64, fds=256, concurrent=4, queue_depth=8,
              cpu_quota_micros=100000, cpu_period_micros=100000,
              cpu_time_millis=30000, wall_millis=120000,
              queue_millis=120000, cleanup_millis=3000)
profile = dict(EnvironmentDigest=manifest['Digest'], EnvironmentRoot='/environment-root',
               WorkRoot='/state/work', CgroupRoot='/delegated/scopes',
               JournalRoot='/state/journal', WorkerPath='/request-worker',
               Languages=['python3','nodejs'], Limits=budget, MaxConcurrent=4,
               MaxQueueDepth=8, MaxQueuedBytes=16<<20, MaxExecutions=512,
               HTTPProxyQualified=True, HostUIDMin=20000, HostUIDMax=20512,
               MaxNetworkEgressBytes=1<<20)
profile['Digest'] = hashlib.sha256(json.dumps(profile,sort_keys=True,separators=(',',':')).encode()).hexdigest()
(root / 'qualification/profile.json').write_text(json.dumps(profile,sort_keys=True))
(root / 'client.env').write_text('SANDBOX_API_KEY='+key+'\n')
(root / 'qualification/server.yaml').write_text(f'app:\n  port: 8194\n  debug: false\n  key: {key}\n')
(root / 'qualification/server.yaml').chmod(0o600)
(root / 'client.env').chmod(0o600)
(root / 'ordinary-conf/config.yaml').write_text(f'''app:
  port: 8195
  debug: false
  key: {key}
max_workers: 4
max_requests: 256
worker_timeout: 20
python_path: /usr/local/bin/python3
nodejs_path: /usr/local/bin/node
enable_network: true
enable_preload: true
python_deps_update_interval: 30m
log_path: /tmp/logs
''')
(root / 'ordinary-conf/config.yaml').chmod(0o600)
arch = sys.argv[1]
lib = 'aarch64-linux-gnu' if arch == 'arm64' else 'x86_64-linux-gnu'
(root / 'ordinary.env').write_text('PYTHON_LIB_PATH='+','.join(['/usr/local/lib/python3.12','/lib/'+lib,'/usr/local/lib/libpython3.12.so.1.0','/etc/ssl/certs/ca-certificates.crt','/usr/lib/ssl/cert.pem'])+'\n')
(root / 'ordinary.env').chmod(0o600)
(root / 'ordinary-dependencies/python-requirements.txt').write_text('')
targets=[]
for index, protocol in enumerate(('tcp','udp','dns','http','https'),1):
    for address in ('192.0.2.10','2001:db8::10'):
        targets.append(dict(Protocol=protocol,Address=address,Host=address,Port=19000+index))
alternates=[]
for protocol,port in (('http',19004),('https',19005)):
    for address in ('192.0.2.20','2001:db8::20'):
        alternates.append(dict(Protocol=protocol,Address=address,Host=address,Port=port))
(root / 'qualification/network.json').write_text(json.dumps(dict(CollectorURL='http://127.0.0.1:19000/',Targets=targets,AlternateTargets=alternates),sort_keys=True))
for name in ('profile.json','network.json'):
    (root / 'qualification' / name).chmod(0o444)
