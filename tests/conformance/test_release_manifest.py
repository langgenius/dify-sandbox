"""Synthetic decision tests; the fixture trust is NOT production crypto."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/verify-builder-release.py'
DIGEST = 'sha256:' + 'a' * 64
OTHER = 'sha256:' + 'b' * 64
FIELDS = ('input_bytes code_bytes stdout_bytes stderr_bytes disk_bytes memory_bytes '
          'pids fds concurrent queue_depth cpu_quota_micros cpu_period_micros '
          'cpu_time_millis wall_millis queue_millis cleanup_millis').split()
GROUPS = ('roots-and-budgets lifecycle dependencies network qualification ordinary-regression').split()


def verifier():
    assert SCRIPT.exists(), 'release verifier implementation missing'
    spec = importlib.util.spec_from_file_location('release_verifier', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()


class SyntheticTrust:
    """Out-of-band immutable payload allowlist exercises decision logic only."""
    def __init__(self, document, provenance, receipt):
        self.document = canonical(document)
        self.build = copy.deepcopy(provenance)
        self.receipt = copy.deepcopy(receipt)

    def authenticate_qualification(self, payload):
        if canonical(payload) != self.document:
            raise ValueError('qualification signature mismatch')

    def build_provenance(self):
        return copy.deepcopy(self.build)

    def endpoint_receipt(self):
        return copy.deepcopy(self.receipt)


@pytest.fixture
def release():
    ids = [f'linux/arm64/{lang}/{group}' for group in GROUPS for lang in ('python3', 'nodejs')]
    expected = dict(architecture='arm64', source_revision='1' * 40, harness_revision='1' * 40,
                    image_index_digest=DIGEST, image_platform_digest=DIGEST,
                    image_config_digest=DIGEST, environment_digest=DIGEST,
                    profile_digest=DIGEST, endpoint_identity='fixture-endpoint',
                    kernel='fixture-kernel', runtime='fixture-runtime', seccomp_digest=DIGEST,
                    deployment_generation='fixture-generation',
                    now=2000, revoked_ids=set())
    effective = {name: DIGEST for name in ('configuration', 'mounts', 'network', 'resources')}
    expected['effective'] = copy.deepcopy(effective)
    budget = {name: 1000000 for name in FIELDS}
    budget.update(concurrent=4, queue_depth=8, pids=64, fds=128,
                  cpu_period_micros=100000, cpu_quota_micros=100000)
    measurements = [dict(language=lang, mode=mode, sample_count=3,
                         memory_bytes=100, cpu_time_millis=10, pids=2, fds=8,
                         disk_bytes=100, stdout_bytes=10, stderr_bytes=0,
                         wall_millis=20, sample_interval_micros=1000,
                         source='supervisor-cgroup-proc', cold_start=True)
                    for lang in ('python3', 'nodejs', 'jinja2') for mode in ('serial', 'four-way')]
    document = dict(schema_version=1, profile_version='builder-native-test-v1',
                    qualification_id='fixture-q', revocation_id='fixture-r',
                    issued_at=1000, expires_at=3000,
                    source_revision='1' * 40,
                    image={name: DIGEST for name in ('index_digest', 'platform_digest', 'config_digest')},
                    runtime=dict(os='linux', architecture='arm64', host_architecture='arm64',
                                 kernel='fixture-kernel', runtime='fixture-runtime',
                                 seccomp_digest=DIGEST, cgroup_version=2,
                                 environment_digest=DIGEST, profile_digest=DIGEST),
                    effective=effective, budget=budget, measurements=measurements,
                    required_case_ids=ids,
                    case_results=[dict(case_id=i, status='passed') for i in ids],
                    skipped=0, failed=0, xfailed=0,
                    ci=dict(run_id='fixture-run', test_revision='1' * 40))
    provenance = dict(source_revision=document['source_revision'],
                      image=copy.deepcopy(document['image']), ci=copy.deepcopy(document['ci']))
    receipt = dict(endpoint_identity=expected['endpoint_identity'],
                   qualification_digest='sha256:' + hashlib.sha256(canonical(document)).hexdigest(),
                   deployment_generation=expected['deployment_generation'],
                   effective_profile_digest=DIGEST, expires_at=3000,
                   effective=copy.deepcopy(effective), image=copy.deepcopy(document['image']),
                   revocation_id='fixture-endpoint-revocation')
    return document, expected, provenance, receipt


def check(release, mutate=None, mutate_trust=None):
    doc, expected, provenance, receipt = copy.deepcopy(release)
    if mutate:
        mutate(doc)
    # Authority agrees with the mutated document here: structural/binding checks
    # must independently reject it even when authentic. This is not signature testing.
    receipt['qualification_digest'] = 'sha256:' + hashlib.sha256(canonical(doc)).hexdigest()
    if mutate_trust:
        mutate_trust(provenance, receipt, expected)
    return verifier().verify_release(doc, trust=SyntheticTrust(doc, provenance, receipt), expected=expected)


def test_valid_immutable_fixture(release):
    assert check(release)['qualification_id'] == 'fixture-q'


def test_accepts_independently_pinned_harness_distinct_from_candidate(release):
    doc, expected, provenance, _ = release
    expected['harness_revision'] = '2' * 40
    doc['ci']['test_revision'] = expected['harness_revision']
    provenance['ci']['test_revision'] = expected['harness_revision']
    assert check(release)['source_revision'] == '1' * 40


@pytest.mark.parametrize('harness', [None, '', 'invalid', '2' * 40])
def test_rejects_missing_malformed_or_wrong_protected_harness(release, harness):
    expected = release[1]
    if harness is None:
        expected.pop('harness_revision')
    else:
        expected['harness_revision'] = harness
    with pytest.raises(ValueError, match='harness'):
        check(release)


def test_default_requires_external_trust(release):
    with pytest.raises(ValueError, match='trust'):
        verifier().verify_release(release[0])


@pytest.mark.parametrize('mutation,error', [
    (lambda d: d['case_results'].pop(), 'missing.*case'),
    (lambda d: d['required_case_ids'].pop(), 'inventory'),
    (lambda d: d['case_results'].append(d['case_results'][0]), 'duplicate'),
    (lambda d: d['case_results'][0].update(status='skipped'), 'case'),
    (lambda d: d['case_results'][0].update(status='xfail'), 'case'),
    (lambda d: d.update(skipped=1), 'skipped'),
    (lambda d: d['runtime'].update(architecture='amd64'), 'architecture'),
    (lambda d: d['runtime'].update(host_architecture='amd64'), 'architecture'),
    (lambda d: d['runtime'].update(environment_digest=OTHER), 'environment'),
    (lambda d: d['runtime'].update(kernel='different-kernel'), 'kernel'),
    (lambda d: d['runtime'].update(runtime='different-runtime'), 'runtime'),
    (lambda d: d['runtime'].update(seccomp_digest=OTHER), 'seccomp'),
    (lambda d: d.update(expires_at=1999), 'expired'),
    (lambda d: d.update(issued_at=2001), 'issued'),
    (lambda d: d['effective'].update(mounts=OTHER), 'effective'),
    (lambda d: d['budget'].update(memory_bytes=0), 'budget'),
    (lambda d: d['budget'].update(pids=True), 'budget'),
    (lambda d: d['budget'].pop('fds'), 'budget'),
    (lambda d: d.update(measurements=[]), 'measurement'),
    (lambda d: d['measurements'][0].update(memory_bytes=1000001), 'exceeds'),
    (lambda d: d['measurements'][0].update(sample_count=0), 'measurement'),
    (lambda d: d['measurements'][0].update(source='user-stdout'), 'measurement'),
    (lambda d: d['measurements'][0].update(cold_start=False), 'cold'),
])
def test_rejects_incomplete_or_changed_document(release, mutation, error):
    with pytest.raises(ValueError, match=error):
        check(release, mutation)


@pytest.mark.parametrize('mutation,error', [
    (lambda p, r, e: p.update(source_revision='2' * 40), 'provenance'),
    (lambda p, r, e: p['image'].update(platform_digest=OTHER), 'provenance'),
    (lambda p, r, e: r.update(endpoint_identity='wrong'), 'endpoint'),
    (lambda p, r, e: r.update(qualification_digest=OTHER), 'qualification digest'),
    (lambda p, r, e: r.update(deployment_generation='old'), 'generation'),
    (lambda p, r, e: r.update(effective_profile_digest=OTHER), 'profile'),
    (lambda p, r, e: r.update(expires_at=1999), 'expired'),
    (lambda p, r, e: e.update(revoked_ids={'fixture-r'}), 'revoked'),
    (lambda p, r, e: e.update(revoked_ids={'fixture-endpoint-revocation'}), 'revoked'),
])
def test_rejects_authority_mismatch(release, mutation, error):
    with pytest.raises(ValueError, match=error):
        check(release, mutate_trust=mutation)


def test_unsigned_tampering_is_rejected(release):
    doc, expected, provenance, receipt = release
    trust = SyntheticTrust(doc, provenance, receipt)
    doc['ci']['run_id'] = 'forged'
    with pytest.raises(ValueError, match='signature'):
        verifier().verify_release(doc, trust=trust, expected=expected)


def test_operator_flags_are_not_authority(release):
    doc = release[0]
    doc.update(verified=True, signed=True)
    with pytest.raises(ValueError, match='trust'):
        verifier().verify_release(doc)


def test_unknown_public_qualification_field_is_rejected(release):
    with pytest.raises(ValueError, match='schema'):
        check(release, lambda d: d.update(service_key='must-not-be-public'))


def test_schema_accepts_immutable_fixture(release):
    import jsonschema
    from referencing import Registry, Resource
    schema=json.loads((ROOT/'tests/conformance/manifest.schema.json').read_text())
    budget=json.loads((ROOT/'tests/conformance/budget.schema.json').read_text())
    jsonschema.Draft202012Validator.check_schema(schema)
    jsonschema.Draft202012Validator.check_schema(budget)
    registry=Registry().with_resource('https://dify.ai/schemas/budget.schema.json',Resource.from_contents(budget))
    jsonschema.Draft202012Validator(schema,registry=registry).validate(release[0])
