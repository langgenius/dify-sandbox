#!/usr/bin/env python3
"""Trusted-harness entrypoint. Input documents never establish build authority."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


SHA = re.compile(r'[0-9a-f]{40}')
DIGEST = re.compile(r'sha256:[0-9a-f]{64}')


def validate_policy(policy, revision):
    if not SHA.fullmatch(revision) or revision != policy.get('source_revision'):
        raise ValueError('protected approved source revision required')
    repository = policy.get('repository', '')
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repository):
        raise ValueError('protected repository required')
    for field in ('signer_revision', 'harness_revision'):
        if not SHA.fullmatch(policy.get(field, '')):
            raise ValueError('protected ' + field + ' required')
    if not re.fullmatch(re.escape(repository) + r'/\.github/workflows/[A-Za-z0-9_-]+\.yml', policy.get('signer_workflow', '')):
        raise ValueError('protected signer workflow required')
    # Explicit owner authorization is required for this isolated package.
    if policy.get('candidate_image') != 'ghcr.io/' + repository.lower() + '-builder-candidate':
        raise ValueError('protected isolated candidate image required')


def bound_json(raw, expected):
    if not DIGEST.fullmatch(expected):
        raise ValueError('immutable OCI digest required')
    if 'sha256:' + hashlib.sha256(raw).hexdigest() != expected:
        raw = raw.removesuffix(b'\n')  # imagetools presentation newline only
    if 'sha256:' + hashlib.sha256(raw).hexdigest() != expected:
        raise ValueError('OCI byte digest mismatch')
    return json.loads(raw), raw.decode()


def verify_build(policy, image, revision, architecture, *, run=subprocess.run):
    validate_policy(policy, revision)
    if not re.fullmatch(re.escape(policy['candidate_image']) + r'@sha256:[0-9a-f]{64}', image):
        raise ValueError('immutable image index in protected candidate package required')
    if architecture not in ('amd64', 'arm64'):
        raise ValueError('native architecture required')
    attestations = {}

    def attest(ref):
        command = ['gh', 'attestation', 'verify', 'oci://' + ref,
                   '--repo', policy['repository'], '--signer-workflow', policy['signer_workflow'],
                   '--signer-digest', policy['signer_revision'], '--source-digest', revision,
                   '--deny-self-hosted-runners', '--predicate-type', 'https://slsa.dev/provenance/v1',
                   '--format', 'json']
        completed = run(command, check=True, timeout=120, capture_output=True)
        verified = json.loads(completed.stdout)
        if not isinstance(verified, list) or not verified:
            raise ValueError('no authenticated build attestations')
        subject_digest = ref.rsplit('@', 1)[1].removeprefix('sha256:')
        if not any(item.get('verificationResult', {}).get('statement', {}).get('predicateType') == 'https://slsa.dev/provenance/v1'
                   and any(subject.get('name') == policy['candidate_image'] and subject.get('digest') == {'sha256': subject_digest}
                           for subject in item['verificationResult']['statement'].get('subject', [])) for item in verified):
            raise ValueError('authenticated attestation subject mismatch')
        attestations[ref] = verified

    def manifest(ref):
        raw = run(['docker', 'buildx', 'imagetools', 'inspect', '--raw', ref],
                  check=True, timeout=120, capture_output=True).stdout
        return bound_json(raw, ref.rsplit('@', 1)[1])

    attest(image)  # No candidate code, setup or pull precedes authentication.
    index, index_raw = manifest(image)
    descriptors = index.get('manifests', [])
    if index.get('schemaVersion') != 2 or index.get('mediaType') != 'application/vnd.oci.image.index.v1+json' or len(descriptors) != 2 or sorted(
            (m.get('platform', {}).get('os', ''), m.get('platform', {}).get('architecture', ''))
            for m in descriptors) != [('linux', 'amd64'), ('linux', 'arm64')]:
        raise ValueError('exact amd64 and arm64 native manifest inventory required')
    platforms = {}
    for descriptor in descriptors:
        platform_digest = descriptor['digest']
        if not DIGEST.fullmatch(platform_digest):
            raise ValueError('immutable platform manifest required')
        ref = policy['candidate_image'] + '@' + platform_digest
        attest(ref)
        platform, raw = manifest(ref)
        config = platform.get('config', {}).get('digest', '')
        if platform.get('schemaVersion') != 2 or not DIGEST.fullmatch(config):
            raise ValueError('immutable config digest required')
        platforms[descriptor['platform']['architecture']] = dict(manifest_digest=platform_digest, config_digest=config, manifest_raw=raw)
    return dict(source_revision=revision, harness_revision=policy['harness_revision'],
                signer_workflow=policy['signer_workflow'], signer_revision=policy['signer_revision'],
                image=image, image_index_digest=image.rsplit('@', 1)[1], index_raw=index_raw,
                platforms=platforms, attestations=attestations)


def verify_executed_config(report, architecture, config_id):
    if report['platforms'][architecture]['config_digest'] != config_id:
        raise ValueError('executed config mismatch')


def verify_handoff(report, image, revision, harness, run_id, attempt, architecture):
    """Check a private root-owned result from this job's authenticating step.

    The caller must enforce custody of the file; this function never treats an
    arbitrary candidate report as cryptographic authority.
    """
    expected = dict(image=image, source_revision=revision, harness_revision=harness,
                    run_id=run_id, run_attempt=attempt)
    if any(report.get(key) != value for key, value in expected.items()):
        raise ValueError('authenticated handoff identity/job generation mismatch')
    if set(report.get('platforms', {})) != {'amd64', 'arm64'} or architecture not in report['platforms']:
        raise ValueError('authenticated handoff native inventory mismatch')
    index, _ = bound_json(report['index_raw'].encode(), image.rsplit('@', 1)[1])
    expected_descriptors = [('linux', arch, value['manifest_digest']) for arch, value in report['platforms'].items()]
    actual = [(m.get('platform', {}).get('os'), m.get('platform', {}).get('architecture'), m.get('digest'))
              for m in index.get('manifests', [])]
    if sorted(actual) != sorted(expected_descriptors):
        raise ValueError('authenticated handoff index/platform mismatch')
    for platform in report['platforms'].values():
        manifest, _ = bound_json(platform['manifest_raw'].encode(), platform['manifest_digest'])
        if manifest['config']['digest'] != platform['config_digest']:
            raise ValueError('authenticated handoff platform/config mismatch')


def main():
    mode, policy_path, image, revision, architecture = sys.argv[1:6]
    policy = json.loads(Path(policy_path).read_text())
    validate_policy(policy, revision)
    harness = subprocess.run(['git', '-C', str(Path(__file__).resolve().parents[1]), 'rev-parse', 'HEAD'],
                             check=True, capture_output=True, text=True).stdout.strip()
    if harness != policy['harness_revision']:
        raise ValueError('checked-out trusted harness revision mismatch')
    if mode != 'authenticate':
        raise ValueError('authenticate mode required')
    report = verify_build(policy, image, revision, architecture)
    subprocess.run(['docker', 'pull', '--platform', 'linux/' + architecture, image], check=True)
    inspected = subprocess.run(['docker', 'image', 'inspect', '--format', '{{json .}}', image],
                               check=True, capture_output=True, text=True)
    actual = json.loads(inspected.stdout)
    if actual['Architecture'] != architecture or actual['Os'] != 'linux':
        raise ValueError('native pulled image architecture mismatch')
    verify_executed_config(report, architecture, actual['Id'])
    report.update(run_id=os.environ['GITHUB_RUN_ID'], run_attempt=os.environ['GITHUB_RUN_ATTEMPT'])
    Path(sys.argv[6]).write_text(json.dumps(report, sort_keys=True, separators=(',', ':')))


if __name__ == '__main__':
    main()
