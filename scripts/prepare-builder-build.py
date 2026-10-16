#!/usr/bin/env python3
"""Trusted build archive/generation checks. Never imports candidate source."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def archive_digest(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return 'sha256:' + digest.hexdigest()


def verify_archive(receipt, archive, revision, architecture, run_id, run_attempt):
    expected = dict(source_revision=revision, architecture=architecture, run_id=run_id, run_attempt=run_attempt)
    if not re.fullmatch(r'[0-9a-f]{40}', revision) or architecture not in ('amd64', 'arm64'):
        raise ValueError('exact source/native architecture required')
    if any(receipt.get(key) != value for key, value in expected.items()):
        raise ValueError('archive source/platform/run generation mismatch')
    if not re.fullmatch(r'sha256:[0-9a-f]{64}', receipt.get('config_digest', '')):
        raise ValueError('archive config digest required')
    if receipt.get('archive_digest') != archive_digest(archive):
        raise ValueError('archive content digest mismatch')


def make_index(platforms):
    if set(platforms) != {'amd64', 'arm64'}:
        raise ValueError('exact two native OCI index platforms required')
    descriptors = []
    for arch in ('amd64', 'arm64'):
        value = platforms[arch]
        if value['media_type'] not in ('application/vnd.oci.image.manifest.v1+json',
                                       'application/vnd.docker.distribution.manifest.v2+json'):
            raise ValueError('native image manifest media type required')
        descriptors.append(dict(mediaType=value['media_type'], digest=value['manifest_digest'], size=value['size'],
                                platform=dict(os='linux', architecture=arch)))
    return json.dumps(dict(schemaVersion=2, mediaType='application/vnd.oci.image.index.v1+json',
                           manifests=descriptors), sort_keys=True, separators=(',', ':')).encode()


def publish(directory, revision, run_id, attempt, image):
    """Publish only current-run native archives; never execute their contents."""
    if image != 'ghcr.io/' + os.environ['GITHUB_REPOSITORY'].lower() + '-builder-candidate':
        raise ValueError('explicit isolated same-repository package required')
    if {p.name for p in directory.iterdir()} != {'builder-native-amd64', 'builder-native-arm64'}:
        raise ValueError('exact two native archive artifact inventory required')
    platforms = {}
    receipts = {}
    # Check the entire transferred generation before loading or publishing any
    # image, including a stale second architecture from a partial rerun.
    for arch in ('amd64', 'arm64'):
        native = directory / ('builder-native-' + arch)
        receipts[arch] = json.loads((native / 'receipt.json').read_text())
        verify_archive(receipts[arch], native / 'image.tar', revision, arch, run_id, attempt)
    for arch in ('amd64', 'arm64'):
        native = directory / ('builder-native-' + arch)
        receipt = receipts[arch]
        subprocess.run(['docker', 'load', '-i', str(native / 'image.tar')], check=True)
        local = 'builder-native:' + arch
        actual = json.loads(subprocess.run(['docker', 'image', 'inspect', '--format', '{{json .}}', local],
                                           check=True, capture_output=True, text=True).stdout)
        if actual['Architecture'] != arch or actual['Os'] != 'linux' or actual['Id'] != receipt['config_digest']:
            raise ValueError('loaded native image/config mismatch')
        tag = image + ':candidate-' + revision + '-' + run_id + '-' + attempt + '-' + arch
        subprocess.run(['docker', 'tag', local, tag], check=True)
        pushed = subprocess.run(['docker', 'push', tag], check=True, capture_output=True, text=True)
        print(pushed.stdout, end='')
        matches = re.findall(r'digest: (sha256:[0-9a-f]{64})', pushed.stdout)
        if len(matches) != 1:
            raise ValueError('actual pushed platform digest required')
        ref = image + '@' + matches[0]
        raw = subprocess.run(['docker', 'buildx', 'imagetools', 'inspect', '--raw', ref],
                             check=True, capture_output=True).stdout
        manifest = load_manifest(raw, matches[0])
        if manifest['config']['digest'] != receipt['config_digest']:
            raise ValueError('published platform/config mismatch')
        canonical_raw = raw if 'sha256:' + hashlib.sha256(raw).hexdigest() == matches[0] else raw.removesuffix(b'\n')
        platforms[arch] = dict(manifest_digest=matches[0], config_digest=actual['Id'],
                               media_type=manifest['mediaType'], size=len(canonical_raw))
    tag = image + ':candidate-' + revision + '-' + run_id + '-' + attempt
    output = directory.parent / 'published'
    output.mkdir()
    index_raw = make_index(platforms)
    (output / 'index.json').write_bytes(index_raw)
    # Pinned maintained ORAS pushes these exact OCI bytes using only the same
    # isolated registry capability. No custom HTTP auth or provenance claims.
    pushed = subprocess.run(['oras', 'manifest', 'push', '--descriptor', '--registry-config',
                             str(Path(os.environ['DOCKER_CONFIG']) / 'config.json'), tag, str(output / 'index.json')],
                            check=True, capture_output=True, text=True)
    descriptor = json.loads(pushed.stdout)
    index_digest = descriptor['digest']
    if (index_digest != 'sha256:' + hashlib.sha256(index_raw).hexdigest()
            or descriptor['mediaType'] != 'application/vnd.oci.image.index.v1+json'
            or descriptor['size'] != len(index_raw)):
        raise ValueError('actual pushed OCI index descriptor mismatch')
    immutable = image + '@' + index_digest
    raw = subprocess.run(['docker', 'buildx', 'imagetools', 'inspect', '--raw', immutable], check=True, capture_output=True).stdout
    index = load_manifest(raw, index_digest)
    observed = [(m.get('platform', {}).get('os'), m.get('platform', {}).get('architecture'), m.get('digest'))
                for m in index.get('manifests', [])]
    expected = [('linux', arch, platforms[arch]['manifest_digest']) for arch in ('amd64', 'arm64')]
    if sorted(observed) != sorted(expected):
        raise ValueError('published index/native manifest mismatch')
    (output / 'published.json').write_text(json.dumps(dict(image=immutable, source_revision=revision,
        run_id=run_id, run_attempt=attempt, platforms=platforms), sort_keys=True))
    # Maintained default SLSA attestation covers the index and both exact native
    # platform subjects. It is not a hand-written provenance predicate.
    subjects = [index_digest] + [platforms[a]['manifest_digest'] for a in ('amd64', 'arm64')]
    (output / 'subjects.sha256').write_text(''.join(d.removeprefix('sha256:') + '  ' + image + '\n' for d in subjects))
    with open(os.environ['GITHUB_OUTPUT'], 'a') as stream:
        stream.write('candidate_index=' + immutable + '\n')


def load_manifest(raw, expected):
    if 'sha256:' + hashlib.sha256(raw).hexdigest() != expected:
        raw = raw.removesuffix(b'\n')
    if 'sha256:' + hashlib.sha256(raw).hexdigest() != expected:
        raise ValueError('published OCI byte digest mismatch')
    return json.loads(raw)


if __name__ == '__main__':
    mode, directory, revision, architecture, run_id, attempt = sys.argv[1:]
    directory = Path(directory)
    if mode == 'publish':
        publish(directory, revision, run_id, attempt, architecture)
        sys.exit(0)
    archive = directory / 'image.tar'
    receipt_path = directory / 'receipt.json'
    if mode == 'record':
        config = subprocess.run(['docker', 'image', 'inspect', '--format', '{{.Id}}', 'builder-native:' + architecture],
                                capture_output=True, check=True, text=True).stdout.strip()
        receipt = dict(source_revision=revision, architecture=architecture, run_id=run_id, run_attempt=attempt,
                       config_digest=config, archive_digest=archive_digest(archive))
        verify_archive(receipt, archive, revision, architecture, run_id, attempt)
        receipt_path.write_text(json.dumps(receipt, sort_keys=True))
    elif mode == 'verify':
        verify_archive(json.loads(receipt_path.read_text()), archive, revision, architecture, run_id, attempt)
    else:
        raise ValueError('record or verify required')
