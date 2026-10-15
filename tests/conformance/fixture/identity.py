"""Bind executed native config to the verified OCI index and platform manifest."""
import hashlib
import json
import pathlib
import sys

root=pathlib.Path('/var/lib/dify-builder-ci')
image,arch,revision,run_id,runtime,config_id=sys.argv[1:]
index_digest=image.rsplit('@',1)[1]
def load_bound(path,digest):
    raw=path.read_bytes()
    if 'sha256:'+hashlib.sha256(raw).hexdigest()!=digest:
        raw=raw.removesuffix(b'\n') # imagetools appends one presentation newline
    assert 'sha256:'+hashlib.sha256(raw).hexdigest()==digest,'OCI byte digest mismatch'
    return json.loads(raw)
index=load_bound(root/'index.json',index_digest)
platforms=[m for m in index['manifests'] if m.get('platform',{}).get('os')=='linux' and m['platform'].get('architecture')==arch]
assert len(platforms)==1,'exact native manifest required'
platform_digest=platforms[0]['digest']
manifest=load_bound(root/'platform.json',platform_digest)
assert manifest['config']['digest']==config_id,'executed config mismatch'
identity=dict(architecture=arch,source_revision=revision,ci_run=run_id,runtime=runtime,
              image=dict(index_digest=index_digest,platform_digest=platform_digest,config_digest=config_id))
(root/'identity.json').write_text(json.dumps(identity,sort_keys=True))
