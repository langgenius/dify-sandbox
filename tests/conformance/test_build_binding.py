"""Trust-boundary decisions with synthetic verifier responses, not provenance."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SHA = '1' * 40
HARNESS = '2' * 40
IMAGE = 'ghcr.io/langgenius/dify-sandbox-builder-candidate'


def module(name):
    path = ROOT / 'scripts' / name
    assert path.exists(), f'{name} missing'
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def encoded(value):
    return json.dumps(value, separators=(',', ':')).encode()


def digest(raw):
    return 'sha256:' + hashlib.sha256(raw).hexdigest()


@pytest.fixture
def binding():
    policy = dict(repository='langgenius/dify-sandbox', signer_workflow='langgenius/dify-sandbox/.github/workflows/builder-candidate-build.yml',
                  signer_revision=SHA, source_revision=SHA, harness_revision=HARNESS,
                  candidate_image=IMAGE)
    manifests = {arch: encoded(dict(schemaVersion=2, mediaType='application/vnd.docker.distribution.manifest.v2+json', config=dict(digest='sha256:' + c * 64)))
                 for arch, c in [('amd64', 'a'), ('arm64', 'b')]}
    index = encoded(dict(schemaVersion=2, mediaType='application/vnd.oci.image.index.v1+json', manifests=[dict(digest=digest(raw), platform=dict(os='linux', architecture=arch))
                                                   for arch, raw in manifests.items()]))
    image = IMAGE + '@' + digest(index)
    calls = []

    def run(command, **kwargs):
        calls.append(command)
        if command[:3] == ['gh', 'attestation', 'verify']:
            subject = command[3].removeprefix('oci://').rsplit('@', 1)[1]
            # Successful maintained verifier result is synthetic, supplied by
            # the test adapter. Production must execute gh with pinned policy.
            statement = dict(subject=[dict(name=IMAGE, digest=dict(sha256=subject.removeprefix('sha256:')))],
                             predicateType='https://slsa.dev/provenance/v1')
            raw = encoded([dict(verificationResult=dict(statement=statement))])
        else:
            ref = command[-1]
            raw = index if ref == image else next(raw for raw in manifests.values() if ref.endswith(digest(raw)))
        return subprocess.CompletedProcess(command, 0, stdout=raw)

    return policy, image, manifests, index, calls, run


def test_authenticates_index_and_both_native_manifests(binding):
    policy, image, manifests, _, calls, run = binding
    result = module('verify-builder-build-binding.py').verify_build(policy, image, SHA, 'arm64', run=run)
    assert result['source_revision'] == SHA
    assert result['harness_revision'] == HARNESS
    assert set(result['platforms']) == {'amd64', 'arm64'}
    assert result['platforms']['arm64']['manifest_digest'] == digest(manifests['arm64'])
    attested = [c for c in calls if c[0] == 'gh']
    assert len(attested) == 3
    for command in attested:
        assert command[command.index('--source-digest') + 1] == SHA
        assert command[command.index('--signer-digest') + 1] == policy['signer_revision']
        assert '--deny-self-hosted-runners' in command


@pytest.mark.parametrize('field,value', [('source_revision', '3' * 40), ('signer_revision', ''),
                                        ('signer_workflow', ''), ('harness_revision', ''),
                                        ('candidate_image', 'ghcr.io/attacker/image')])
def test_wrong_protected_policy_rejected_before_any_external_execution(binding, field, value):
    policy, image, _, _, calls, run = binding
    policy[field] = value
    with pytest.raises(ValueError):
        module('verify-builder-build-binding.py').verify_build(policy, image, SHA, 'amd64', run=run)
    assert calls == []


def test_mutable_tag_rejected_before_execution(binding):
    policy, _, _, _, calls, run = binding
    with pytest.raises(ValueError, match='immutable'):
        module('verify-builder-build-binding.py').verify_build(policy, IMAGE + ':main', SHA, 'amd64', run=run)
    assert not calls


def test_failed_cryptographic_verifier_cannot_reach_manifest_or_setup(binding):
    policy, image, _, _, _, _ = binding
    calls = []
    def reject(command, **kwargs):
        calls.append(command)
        raise subprocess.CalledProcessError(1, command, stderr=b'wrong signer/source')
    with pytest.raises(subprocess.CalledProcessError):
        module('verify-builder-build-binding.py').verify_build(policy, image, SHA, 'amd64', run=reject)
    assert len(calls) == 1 and calls[0][:3] == ['gh', 'attestation', 'verify']


@pytest.mark.parametrize('kind', ['missing-platform', 'duplicate-platform', 'bad-index', 'bad-platform', 'wrong-subject', 'non-oci-index'])
def test_bad_index_platform_or_attested_subject_rejected(binding, kind):
    policy, image, manifests, index, _, original = binding
    if kind in ('missing-platform', 'duplicate-platform', 'non-oci-index'):
        obj = json.loads(index)
        if kind == 'non-oci-index':
            obj['mediaType'] = 'application/vnd.docker.distribution.manifest.list.v2+json'
        else:
            obj['manifests'] = obj['manifests'][:1] if kind == 'missing-platform' else obj['manifests'] + obj['manifests'][:1]
        index = encoded(obj)
        image = IMAGE + '@' + digest(index)
    def run(command, **kwargs):
        if command[0] == 'gh':
            result = original(command, **kwargs)
            if kind == 'wrong-subject':
                data = json.loads(result.stdout)
                data[0]['verificationResult']['statement']['subject'][0]['digest']['sha256'] = '0' * 64
                result.stdout = encoded(data)
            return result
        if command[-1] == image:
            return subprocess.CompletedProcess(command, 0, stdout=index + (b' ' if kind == 'bad-index' else b''))
        result = original(command, **kwargs)
        if kind == 'bad-platform':
            result.stdout += b' '
        return result
    with pytest.raises(ValueError):
        module('verify-builder-build-binding.py').verify_build(policy, image, SHA, 'amd64', run=run)


def test_wrong_executed_config_rejected(binding):
    policy, image, _, _, _, run = binding
    verifier = module('verify-builder-build-binding.py')
    report = verifier.verify_build(policy, image, SHA, 'arm64', run=run)
    with pytest.raises(ValueError, match='config'):
        verifier.verify_executed_config(report, 'arm64', 'sha256:' + 'c' * 64)


@pytest.mark.parametrize('field', [None, 'image', 'source_revision', 'harness_revision', 'run_id', 'run_attempt', 'platforms'])
def test_current_job_handoff_rejects_substituted_identity_or_generation(binding, field):
    policy, image, _, _, _, run = binding
    verifier = module('verify-builder-build-binding.py')
    report = verifier.verify_build(policy, image, SHA, 'arm64', run=run)
    report.update(run_id='12', run_attempt='1')
    if field:
        report[field] = {} if field == 'platforms' else 'wrong'
        with pytest.raises(ValueError):
            verifier.verify_handoff(report, image, SHA, HARNESS, '12', '1', 'arm64')
    else:
        verifier.verify_handoff(report, image, SHA, HARNESS, '12', '1', 'arm64')


def test_untrusted_candidate_replacement_never_selected_as_harness(tmp_path):
    candidate = tmp_path / 'candidate'
    candidate.mkdir()
    sentinel = tmp_path / 'executed'
    for name in ('verify-builder-build-binding.py', 'builder-conformance-ci.sh'):
        (candidate / name).write_text(f'open({str(sentinel)!r}, "w").write("unsafe")')
    verifier = module('verify-builder-build-binding.py')
    with pytest.raises(ValueError, match='approved source'):
        verifier.validate_policy(dict(repository='langgenius/dify-sandbox', signer_workflow='x', signer_revision=SHA,
                                      source_revision=SHA, harness_revision=HARNESS, candidate_image=IMAGE), '3' * 40)
    assert not sentinel.exists()
    policy = tmp_path / 'policy.json'
    policy.write_text(json.dumps(dict(repository='langgenius/dify-sandbox', signer_workflow='x', signer_revision=SHA,
                                      source_revision=SHA, harness_revision=HARNESS, candidate_image=IMAGE)))
    completed = subprocess.run([sys.executable, str(ROOT / 'scripts/verify-builder-build-binding.py'),
                                'authenticate', str(policy), IMAGE + '@sha256:' + 'a' * 64, '3' * 40,
                                'arm64', str(tmp_path / 'receipt.json')], cwd=candidate, capture_output=True, text=True)
    assert completed.returncode != 0 and 'protected approved source' in completed.stderr
    assert not sentinel.exists()
    workflow = (ROOT / '.github/workflows/builder-conformance.yml').read_text()
    assert 'ref: ${{ vars.BUILDER_TRUSTED_HARNESS_REVISION }}' in workflow
    assert 'ref: ${{ inputs.source_revision }}' not in workflow
    assert 'working-directory: harness' in workflow
    assert workflow.index('authenticate') < workflow.index('provision')


def test_wrong_archive_generation_or_content_rejected(tmp_path):
    prepare = module('prepare-builder-build.py')
    archive = tmp_path / 'image.tar'
    archive.write_bytes(b'archive fixture')
    receipt = dict(source_revision=SHA, architecture='arm64', run_id='12', run_attempt='1',
                   archive_digest=digest(archive.read_bytes()), config_digest='sha256:' + 'a' * 64)
    prepare.verify_archive(receipt, archive, SHA, 'arm64', '12', '1')
    for field in ('source_revision', 'architecture', 'run_id', 'run_attempt', 'archive_digest', 'config_digest'):
        wrong = copy.deepcopy(receipt)
        wrong[field] = 'wrong'
        with pytest.raises(ValueError):
            prepare.verify_archive(wrong, archive, SHA, 'arm64', '12', '1')
    archive.write_bytes(b'corrupted')
    with pytest.raises(ValueError):
        prepare.verify_archive(receipt, archive, SHA, 'arm64', '12', '1')


def test_publisher_rejects_second_wrong_generation_before_loading_first_archive(tmp_path, monkeypatch):
    prepare = module('prepare-builder-build.py')
    for arch in ('amd64', 'arm64'):
        directory = tmp_path / ('builder-native-' + arch)
        directory.mkdir()
        archive = directory / 'image.tar'
        archive.write_bytes(b'archive fixture')
        receipt = dict(source_revision=SHA, architecture=arch, run_id='12', run_attempt='1' if arch == 'amd64' else '0',
                       archive_digest=digest(archive.read_bytes()), config_digest='sha256:' + 'a' * 64)
        (directory / 'receipt.json').write_text(json.dumps(receipt))
    monkeypatch.setenv('GITHUB_REPOSITORY', 'langgenius/dify-sandbox')
    calls = []
    def external(command, **kwargs):
        calls.append(command)
        raise AssertionError('external tool executed before all archives preflighted')
    monkeypatch.setattr(prepare.subprocess, 'run', external)
    with pytest.raises(ValueError, match='generation'):
        prepare.publish(tmp_path, SHA, '12', '1', IMAGE)
    assert calls == []


def test_assembled_oci_index_binds_actual_native_descriptors(binding):
    _, _, manifests, _, _, _ = binding
    platforms = {arch: dict(manifest_digest=digest(raw), config_digest=json.loads(raw)['config']['digest'],
                            media_type=json.loads(raw)['mediaType'], size=len(raw)) for arch, raw in manifests.items()}
    raw = module('prepare-builder-build.py').make_index(platforms)
    index = json.loads(raw)
    assert index['mediaType'] == 'application/vnd.oci.image.index.v1+json'
    assert index['schemaVersion'] == 2
    assert {m['platform']['architecture'] for m in index['manifests']} == {'amd64', 'arm64'}
    for descriptor in index['manifests']:
        actual = manifests[descriptor['platform']['architecture']]
        assert descriptor['digest'] == digest(actual)
        assert descriptor['size'] == len(actual)
        assert descriptor['mediaType'] == json.loads(actual)['mediaType']


def test_build_rejects_unreviewed_build_tool_before_dockerfile(tmp_path):
    calls = tmp_path / 'calls'
    env = dict(os.environ, GITHUB_ACTIONS='true',
               RUNNER_ENVIRONMENT='github-hosted', BUILDER_APPROVED_SOURCE_REVISION=SHA,
               GITHUB_RUN_ID='12', GITHUB_RUN_ATTEMPT='1')
    # /tmp remains noexec in the qualified fixture. Export Bash adapters instead
    # of relaxing its mount policy to execute synthetic tool binaries.
    env.update({'BASH_FUNC_git%%': '() { if [[ "$*" == *rev-parse* ]]; then echo ' + SHA + '; fi; }',
                'BASH_FUNC_uname%%': '() { echo aarch64; }',
                'BASH_FUNC_docker%%': '() { echo "$*" >> "' + str(calls) + '"; echo unreviewed-tool; }'})
    result = subprocess.run(['bash', str(ROOT / 'scripts/build-builder-candidate.sh'), str(tmp_path), 'arm64', str(tmp_path / 'out')],
                            env=env, capture_output=True)
    assert result.returncode != 0
    assert calls.exists(), result.stderr.decode()
    assert not any(line.startswith(('build ', 'buildx build')) for line in calls.read_text().splitlines())


def workflow_run(filename, step_name):
    """Extract the literal reviewed bootstrap script, without a YAML dependency."""
    text = (ROOT / '.github/workflows' / filename).read_text()
    step = text.split('name: ' + step_name + '\n', 1)[1].split('\n      - ', 1)[0]
    script = step.split('        run: |\n', 1)[1]
    return '\n'.join(line[10:] for line in script.splitlines())


@pytest.mark.parametrize('field', [None, 'APPROVED_SOURCE', 'ENTRYPOINT_REVISION', 'CS5_HARNESS_REVISION',
                                 'TRUSTED_REVISION', 'TRUSTED_WORKFLOW', 'CANDIDATE_IMAGE'])
def test_actual_conformance_bootstrap_enforces_independent_policy_before_checkout(tmp_path, field):
    env = dict(os.environ, APPROVED_SOURCE=SHA, CS5_SOURCE_REVISION=SHA, ENTRYPOINT_REVISION=SHA,
               ACTUAL_ENTRYPOINT=SHA, ACTUAL_ENTRYPOINT_REPOSITORY='langgenius/dify-sandbox',
               ACTUAL_ENTRYPOINT_FILE='.github/workflows/builder-conformance.yml',
               CS5_HARNESS_REVISION=HARNESS, TRUSTED_REPOSITORY='langgenius/dify-sandbox',
               TRUSTED_WORKFLOW='langgenius/dify-sandbox/.github/workflows/builder-candidate-build.yml',
               TRUSTED_REVISION=SHA, CANDIDATE_IMAGE=IMAGE, RUNNER_TEMP=str(tmp_path), GITHUB_ENV=str(tmp_path / 'env'))
    if field:
        env[field] = '' if field != 'APPROVED_SOURCE' else '3' * 40
    script = workflow_run('builder-conformance.yml', 'Validate protected entrypoint, harness and approved source policy')
    result = subprocess.run(['bash', '-e', '-c', script], env=env, capture_output=True)
    assert (result.returncode == 0) == (field is None)
    if field:
        assert not (tmp_path / 'builder-build-policy.json').exists()
    else:
        policy = json.loads((tmp_path / 'builder-build-policy.json').read_text())
        assert policy['source_revision'] == SHA and policy['harness_revision'] == HARNESS


@pytest.mark.parametrize('caller,called,called_repository,called_file,accepted', [
    ('3' * 40, '4' * 40, 'langgenius/dify-sandbox', '.github/workflows/builder-conformance.yml', True),
    ('4' * 40, '4' * 40, 'langgenius/dify-sandbox', '.github/workflows/builder-conformance.yml', True),
    ('4' * 40, '5' * 40, 'langgenius/dify-sandbox', '.github/workflows/builder-conformance.yml', False),
    ('4' * 40, '', 'langgenius/dify-sandbox', '.github/workflows/builder-conformance.yml', False),
    ('4' * 40, 'invalid', 'langgenius/dify-sandbox', '.github/workflows/builder-conformance.yml', False),
    ('4' * 40, '4' * 40, 'other/repository', '.github/workflows/builder-conformance.yml', False),
    ('4' * 40, '4' * 40, 'langgenius/dify-sandbox', '.github/workflows/other.yml', False),
])
def test_conformance_expression_binds_called_job_identity_before_policy(tmp_path, caller, called,
                                                                       called_repository, called_file, accepted):
    # GitHub.com's documented model: github workflow identity belongs to the
    # caller; job workflow identity belongs to the defining (called) workflow.
    # Resolve the ACTUAL_* expressions from the production step, rather than
    # assigning ACTUAL_ENTRYPOINT to the desired value as the old test did.
    import re
    text = (ROOT / '.github/workflows/builder-conformance.yml').read_text()
    step = text.split('name: Validate protected entrypoint, harness and approved source policy\n', 1)[1].split('\n      - ', 1)[0]
    context = {'github.workflow_sha': caller, 'job.workflow_sha': called,
               'job.workflow_repository': called_repository, 'job.workflow_file_path': called_file}
    env = dict(os.environ, APPROVED_SOURCE=SHA, CS5_SOURCE_REVISION=SHA, ENTRYPOINT_REVISION='4' * 40,
               CS5_HARNESS_REVISION=HARNESS, TRUSTED_REPOSITORY='langgenius/dify-sandbox',
               TRUSTED_WORKFLOW='langgenius/dify-sandbox/.github/workflows/builder-candidate-build.yml',
               TRUSTED_REVISION=SHA, CANDIDATE_IMAGE=IMAGE, RUNNER_TEMP=str(tmp_path), GITHUB_ENV=str(tmp_path / 'env'))
    for name, expression in re.findall(r'^          (ACTUAL_ENTRYPOINT\w*): (.+)$', step, re.MULTILINE):
        match = re.fullmatch(r'\$\{\{\s*([a-z_.]+)\s*\}\}', expression)
        assert match, 'entrypoint identity must use a platform context property'
        env[name] = context.get(match.group(1), '')
    script = workflow_run('builder-conformance.yml', 'Validate protected entrypoint, harness and approved source policy')
    result = subprocess.run(['bash', '-e', '-c', script], env=env, capture_output=True)
    assert (result.returncode == 0) == accepted, result.stderr.decode()
    policy_path = tmp_path / 'builder-build-policy.json'
    assert policy_path.exists() == accepted
    if accepted:
        assert env['ACTUAL_ENTRYPOINT'] == called == '4' * 40
        policy = json.loads(policy_path.read_text())
        assert policy['harness_revision'] == HARNESS and policy['source_revision'] == SHA


@pytest.mark.parametrize('actual_source,actual_workflow', [(SHA, SHA), ('3' * 40, SHA), (SHA, '3' * 40)])
def test_actual_build_entrypoint_binds_oidc_source_and_signer_before_checkout(actual_source, actual_workflow):
    env = dict(os.environ, BUILDER_APPROVED_SOURCE_REVISION=SHA, GITHUB_SHA=actual_source,
               TRUSTED_BUILD=SHA, ACTUAL_WORKFLOW=actual_workflow, TRUSTED_HARNESS=HARNESS,
               GITHUB_REPOSITORY='langgenius/dify-sandbox',
               TRUSTED_WORKFLOW='langgenius/dify-sandbox/.github/workflows/builder-candidate-build.yml')
    script = workflow_run('builder-candidate-build.yml', 'Gate source, trusted workflow and harness before checkout')
    result = subprocess.run(['bash', '-e', '-c', script], env=env, capture_output=True)
    assert (result.returncode == 0) == (actual_source == actual_workflow == SHA)
