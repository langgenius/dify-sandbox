#!/usr/bin/env python3
"""Authenticate a candidate using GitHub's maintained attestation verifier.

Policy is supplied by the protected CI/release owner, never by a workload or
qualification document. The JSON output of gh is retained for audit; its mere
presence is never accepted in place of executing the verifier.
"""
import json
from pathlib import Path
import re
import subprocess
import sys


def verify_build(policy, image, revision, architecture):
    if not re.fullmatch(r'[^\s]+@sha256:[0-9a-f]{64}', image):
        raise ValueError('immutable image index required')
    if not re.fullmatch(r'[0-9a-f]{40}', revision) or architecture not in ('amd64','arm64'):
        raise ValueError('source revision/native architecture required')
    if not re.fullmatch(r'[0-9a-f]{40}', policy['signer_revision']):
        raise ValueError('protected signer workflow revision required')
    command = ['gh','attestation','verify','oci://'+image,
               '--repo',policy['repository'],
               '--signer-workflow',policy['signer_workflow'],
               '--signer-digest',policy['signer_revision'],
               '--source-digest',revision,'--deny-self-hosted-runners',
               '--predicate-type','https://slsa.dev/provenance/v1','--format','json']
    if policy.get('bundle'):
        command += ['--bundle',policy['bundle']]
    completed = subprocess.run(command, check=True, timeout=60, capture_output=True)
    verified = json.loads(completed.stdout)
    if not isinstance(verified,list) or not verified:
        raise ValueError('no authenticated build attestations')
    return verified


if __name__ == '__main__':
    policy,image,revision,architecture = sys.argv[1:]
    evidence = verify_build(json.loads(Path(policy).read_text()),image,revision,architecture)
    print(json.dumps(evidence,sort_keys=True,separators=(',',':')))
