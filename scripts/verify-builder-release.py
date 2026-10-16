#!/usr/bin/env python3
"""Verify authenticated release evidence against protected deployment expectations.

The verifier is deliberately independent of signature implementation. Production
uses CosignTrust below (standard cosign verify-blob + signed CI provenance);
tests inject an explicit synthetic trusted owner. Inputs never choose their keys.
"""
import argparse
import hashlib
import json
import re
import subprocess
import time
from pathlib import Path

GROUPS = ('roots-and-budgets', 'lifecycle', 'dependencies', 'network',
          'qualification', 'ordinary-regression')
LANGUAGES = ('python3', 'nodejs')
BUDGET_FIELDS = ('input_bytes code_bytes stdout_bytes stderr_bytes disk_bytes '
                 'memory_bytes pids fds concurrent queue_depth cpu_quota_micros '
                 'cpu_period_micros cpu_time_millis wall_millis queue_millis '
                 'cleanup_millis').split()
MEASURE_FIELDS = ('memory_bytes cpu_time_millis pids fds disk_bytes stdout_bytes '
                  'stderr_bytes wall_millis').split()
DIGEST = re.compile(r'^sha256:[0-9a-f]{64}$')
REVISION = re.compile(r'^[0-9a-f]{40}$')


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False,
                      allow_nan=False).encode('utf-8')


def digest(value):
    return 'sha256:' + hashlib.sha256(canonical(value)).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def positive(value):
    return type(value) is int and 0 < value <= 2**53 - 1


def inventory(architecture):
    require(architecture in ('amd64', 'arm64'), 'unsupported architecture')
    return [f'linux/{architecture}/{language}/{group}'
            for group in GROUPS for language in LANGUAGES]


def verify_release(document: dict, *, trust=None, expected=None) -> dict:
    """Expected and trust MUST come from protected deployment/controller policy."""
    require(trust is not None, 'trusted verification owner required')
    require(isinstance(expected, dict), 'protected deployment expectations required')
    require(isinstance(document, dict), 'qualification object required')
    trust.authenticate_qualification(document)
    try:
        return _verify(document, trust, expected)
    except (KeyError, TypeError, AttributeError) as exc:
        raise ValueError(f'missing or malformed qualification field: {exc}') from exc


def _verify(d, trust, expected):
    require(d['schema_version'] == 1 and type(d['schema_version']) is int,
            'unsupported qualification schema')
    require(d['profile_version'] == 'builder-native-test-v1', 'unsupported profile version')
    now = expected['now']
    require(type(now) is int, 'trusted clock required')
    require(type(d['issued_at']) is int and 0 < d['issued_at'] <= now, 'invalid issued time')
    require(type(d['expires_at']) is int and now < d['expires_at'], 'qualification expired')
    for identifier in ('qualification_id', 'revocation_id'):
        require(isinstance(d[identifier], str) and bool(d[identifier]), f'{identifier} required')
        require(d[identifier] not in expected['revoked_ids'], 'qualification revoked')
    require(REVISION.fullmatch(d['source_revision']) is not None, 'invalid source revision')
    require(d['source_revision'] == expected['source_revision'], 'source revision mismatch')
    r = d['runtime']
    require(r['os'] == 'linux' and r['architecture'] == expected['architecture']
            and r['host_architecture'] == r['architecture'], 'native Linux architecture mismatch')
    require(r['cgroup_version'] == 2 and type(r['cgroup_version']) is int, 'cgroup v2 required')
    for field in ('kernel', 'runtime', 'seccomp_digest'):
        require(isinstance(r[field], str) and bool(r[field]) and r[field] == expected[field],
                f'{field} identity mismatch')
    require(DIGEST.fullmatch(r['seccomp_digest']) is not None, 'seccomp digest required')
    for field in ('environment_digest', 'profile_digest'):
        require(DIGEST.fullmatch(r[field]) is not None and r[field] == expected[field],
                f'{field} mismatch')
    for field in ('index_digest', 'platform_digest', 'config_digest'):
        require(DIGEST.fullmatch(d['image'][field]) is not None
                and d['image'][field] == expected['image_' + field], f'image {field} mismatch')
    require(set(d['effective']) == {'configuration', 'mounts', 'network', 'resources'},
            'effective configuration inventory mismatch')
    require(all(isinstance(v, str) and DIGEST.fullmatch(v) for v in d['effective'].values())
            and d['effective'] == expected['effective'], 'effective configuration mismatch')
    required = inventory(r['architecture'])
    require(len(d['required_case_ids']) == len(required)
            and set(d['required_case_ids']) == set(required), 'required case inventory mismatch')
    cases = d['case_results']
    actual = [c['case_id'] for c in cases]
    require(len(actual) == len(set(actual)), 'duplicate case IDs')
    missing, extra = set(required) - set(actual), set(actual) - set(required)
    require(not missing and not extra, f'missing or extra case IDs: missing={sorted(missing)} extra={sorted(extra)}')
    for c in cases:
        require(c['status'] == 'passed', f'case {c["case_id"]}: {c["status"]}')
    for field in ('skipped', 'failed', 'xfailed'):
        require(type(d[field]) is int and d[field] == 0, f'{field} must be zero')
    b = d['budget']
    require(set(b) == set(BUDGET_FIELDS) and all(positive(v) for v in b.values()),
            'finite positive budget fields required')
    require(b['concurrent'] >= 4 and 1000 <= b['cpu_period_micros'] <= 1000000
            and b['cpu_quota_micros'] >= 1000, 'invalid measured budget controls')
    measurements = d['measurements']
    require(isinstance(measurements, list) and bool(measurements), 'measurement artifact required')
    keys = set()
    for m in measurements:
        key = (m['language'], m['mode'])
        require(key not in keys, 'duplicate measurement cohort')
        keys.add(key)
        require(positive(m['sample_count']) and m['sample_count'] >= 3
                and positive(m['sample_interval_micros'])
                and m['source'] == 'supervisor-cgroup-proc', 'independent repeated measurement required')
        require(m['cold_start'] is True, 'cold load measurement required')
        for field in MEASURE_FIELDS:
            require(type(m[field]) is int and 0 <= m[field], f'measurement {field} missing or negative')
            require(m[field] <= b[field], f'normal workload exceeds budget: {m["language"]}/{m["mode"]}/{field}')
        require(m['memory_bytes'] > 0 and m['cpu_time_millis'] > 0 and m['pids'] > 0
                and m['fds'] > 0 and m['wall_millis'] > 0, 'empty resource measurement')
    require(keys == {(l, m) for l in ('python3', 'nodejs', 'jinja2') for m in ('serial', 'four-way')},
            'missing serial/four-way language measurement')
    harness_revision = expected.get('harness_revision')
    require(isinstance(harness_revision, str) and REVISION.fullmatch(harness_revision) is not None,
            'protected harness revision required')
    require(REVISION.fullmatch(d['ci']['test_revision']) is not None
            and d['ci']['test_revision'] == harness_revision and bool(d['ci']['run_id']),
            'CI harness revision/run mismatch')
    provenance = trust.build_provenance()
    require(provenance['source_revision'] == d['source_revision']
            and provenance['image'] == d['image'] and provenance['ci'] == d['ci'],
            'authenticated build provenance mismatch')
    receipt = trust.endpoint_receipt()
    require(receipt['endpoint_identity'] == expected['endpoint_identity'], 'endpoint identity mismatch')
    require(receipt['qualification_digest'] == digest(d), 'endpoint qualification digest mismatch')
    require(receipt['deployment_generation'] == expected['deployment_generation'], 'deployment generation mismatch')
    require(receipt['effective_profile_digest'] == r['profile_digest'], 'endpoint profile mismatch')
    require(type(receipt['expires_at']) is int and now < receipt['expires_at'], 'endpoint receipt expired')
    require(receipt['revocation_id'] not in expected['revoked_ids'], 'endpoint receipt revoked')
    require(receipt['effective'] == d['effective'] and receipt['image'] == d['image'], 'endpoint effective/image mismatch')
    import jsonschema
    from referencing import Registry, Resource
    directory = Path(__file__).resolve().parents[1] / 'tests/conformance'
    schema = json.loads((directory / 'manifest.schema.json').read_text())
    budget_schema = json.loads((directory / 'budget.schema.json').read_text())
    registry = Registry().with_resource('https://dify.ai/schemas/budget.schema.json',
                                        Resource.from_contents(budget_schema))
    try:
        jsonschema.Draft202012Validator(schema, registry=registry).validate(d)
    except jsonschema.ValidationError as exc:
        raise ValueError('qualification schema: ' + exc.message) from exc
    return d


class CosignTrust:
    """Adapter to maintained cosign verification. Policy is protected CI input.

    Every role has a separate key file and bundle; cosign authenticates both
    signed payload and transparency evidence. Never use --insecure-ignore-tlog.
    No executable, keys, identity or issuer are selected by the document.
    """
    def __init__(self, policy, directory, executable='/usr/local/bin/cosign'):
        self.policy = policy
        self.directory = Path(directory).resolve()
        self.executable = executable

    def _authenticated(self, role):
        p = self.policy[role]
        payload = self.directory / p['payload']
        bundle = self.directory / p['bundle']
        require(payload.resolve().is_relative_to(self.directory)
                and bundle.resolve().is_relative_to(self.directory), 'evidence path escapes directory')
        command = [self.executable, 'verify-blob', '--bundle', str(bundle), '--key', p['public_key'], str(payload)]
        subprocess.run(command, check=True, timeout=60, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        raw = payload.read_bytes()
        value = json.loads(raw)
        require(canonical(value) == raw, f'{role} payload must be canonical JSON')
        return value

    def authenticate_qualification(self, payload):
        require(self._authenticated('qualification') == payload, 'qualification signature payload mismatch')

    def build_provenance(self):
        value = self._authenticated('provenance')
        policy = self.policy['build_attestation']
        image = policy['image_ref']
        require(image.endswith('@' + value['image']['index_digest']),
                'authenticated build index mismatch')
        # Execute the maintained verifier against the immutable image. A JSON
        # export claiming prior verification is not accepted as authority.
        command = ['gh', 'attestation', 'verify', 'oci://' + image,
                   '--repo', policy['repository'],
                   '--signer-workflow', policy['signer_workflow'],
                   '--signer-digest', policy['signer_revision'],
                   '--source-digest', value['source_revision'],
                   '--deny-self-hosted-runners', '--predicate-type',
                   'https://slsa.dev/provenance/v1', '--format', 'json']
        if policy.get('bundle'):
            command += ['--bundle', policy['bundle']]
        result = subprocess.run(command, check=True, timeout=60,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        require(bool(json.loads(result.stdout)), 'authenticated build provenance absent')
        return value

    def endpoint_receipt(self):
        return self._authenticated('endpoint')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('document')
    parser.add_argument('--protected-policy', required=True)
    parser.add_argument('--evidence-directory', required=True)
    args = parser.parse_args()
    policy = json.loads(Path(args.protected_policy).read_text())
    expected = policy['expected']
    expected['now'] = int(time.time())
    expected['revoked_ids'] = set(expected['revoked_ids'])
    result = verify_release(json.loads(Path(args.document).read_text()),
                            trust=CosignTrust(policy['trust'], args.evidence_directory), expected=expected)
    print(digest(result))


if __name__ == '__main__':
    main()
