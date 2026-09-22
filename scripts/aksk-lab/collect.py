#!/usr/bin/env python3
"""Collect sanitized evidence on ubuntu after every required lab stage passes."""
import hashlib
import importlib.util
import json
import subprocess
from pathlib import Path

spec = importlib.util.spec_from_file_location('lab', Path(__file__).with_name('lab.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
R, E, S = m.R, m.E, m.S
rows = [json.loads(line) for line in (E / 'acceptance.jsonl').read_text().splitlines()]
latest = {row['case']: row for row in rows}
required = ['explicit-empty-database-initialization', 'python-nodeport-governance-mtls-network-pg',
            'key-create-a-vpc-reader', 'bad-sk', 'cross-tenant-same-as-absent',
            'no-role-permission', 'no-network-plan', 'cross-tenant-role-create',
            'disabled-key', 'deleted-key-rejected', 'expired-key', 'reset-old-secret-rejected',
            'reset-new-secret-accepted', 'rebind-denied-role', 'user-only', 'key-management',
            'mtls-user', 'mtls-key', 'mtls-missing-cert', 'mtls-wrong-cert', 'mtls-tenant-mismatch',
            'user-jwt-vpc-query', 'logout-a', 'logout-platform', 'logout-revokes-jwt-a',
            'no-sk-in-logs-or-audits', 'no-signatures-in-logs-or-audits', 'audits-have-key-and-user',
            'restart-no-schema-or-initialization-writes', 'missing-master-key-startup-refused',
            'postgres-tenant-role-fk-and-delete-restrict', 'missing-ciphertext-fails-closed',
            'plaintext-ciphertext-fails-closed', 'malformed-ciphertext-fails-closed',
            'api-key-audit-no-user-impersonation', 'failed-signature-audit-unverified']
assert all(case in latest and latest[case]['status'] == 'pass' for case in required)
assert all(row['status'] == 'pass' for row in latest.values())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def manifest(repo, roots):
    paths = []
    for root in roots:
        p = repo / root
        paths += list(p.rglob('*')) if p.is_dir() else [p]
    entries = {str(p.relative_to(repo)): sha(p) for p in sorted(set(paths)) if p.is_file() and p.suffix in ['.go', '.proto', '.yaml', '.sql', '.mod', '.sum', '.hcl']}
    canonical = json.dumps(entries, sort_keys=True, separators=(',', ':')).encode()
    return {'sha256': hashlib.sha256(canonical).hexdigest(), 'files': entries}


sources = {
    'governance': manifest(R / 'governance', ['api', 'app', 'pkg', 'sql', 'migrations', 'go.mod', 'go.sum']),
    'network': manifest(R / 'network', ['api', 'cmd', 'internal', 'migrations', 'go.mod', 'go.sum']),
}
(E / 'source-manifest.json').write_text(json.dumps(sources, indent=2) + '\n')
# Prove that binaries in running Pods are exactly the built artifacts.
runtime = {}
for service, filename in [('governance', 'governance'), ('network', 'network')]:
    actual = m.kub('exec', 'deployment/' + service, '--', 'sha256sum', '/app/server').split()[0]
    expected = sha(R / 'bin' / filename)
    assert actual == expected, service + ' runtime binary mismatch'
    runtime[service] = {'binary_sha256': actual}
admin_actual = m.kub('exec', 'deployment/governance', '--', 'sha256sum', '/app/admin').split()[0]
assert admin_actual == sha(R / 'bin/admin')
runtime['admin'] = {'binary_sha256': admin_actual}
# Compare application sources to the build snapshot (docs/scripts do not affect binaries).
compiled = manifest(R / 'governance-key', ['api/gen', 'app/admin/service/cmd/server', 'app/admin/service/cmd/admin', 'app/admin/service/internal', 'pkg', 'sql', 'go.mod', 'go.sum'])
final = manifest(R / 'governance', ['api/gen', 'app/admin/service/cmd/server', 'app/admin/service/cmd/admin', 'app/admin/service/internal', 'pkg', 'sql', 'go.mod', 'go.sum'])
assert compiled == final, 'final application sources differ from the build snapshot'
summary = {
    'status': 'pass', 'case_count': len(latest), 'cases': list(latest.values()),
    'execution_host': m.run(['hostname']), 'context': m.CONTEXT, 'namespace': m.NS,
    'nodeport_origin': S['base'], 'runtime': runtime,
    'source_sha256': {key: value['sha256'] for key, value in sources.items()},
    'bases': {'governance': '5a2a2e8c6c303a280acf2f6168d63fcebca8990c', 'network': '66f787bd30134141726c596612501a83cf75bdb7'},
    'not_verified': ['network provisioning/data plane', 'other business APIs', 'frontend', 'public HTTPS', 'production cutover'],
}
# Fail collection if any actual credential appears in the exported evidence.
secrets = list(S.get('passwords', {}).values()) + list(S.get('tokens', {}).values()) + S.get('all_secrets', []) + S.get('request_signatures', []) + [S['jwt'], S['encryption']]
for path in E.iterdir():
    if path.is_file():
        text = path.read_text(errors='replace')
        assert not any(secret and secret in text for secret in secrets), 'credential found in evidence ' + path.name
(E / 'acceptance.json').write_text(json.dumps(summary, indent=2) + '\n')
for path in E.glob('*.json'):
    path.write_text(json.dumps(json.loads(path.read_text()), indent=2) + '\n')
print(json.dumps({key: summary[key] for key in ['status', 'case_count', 'namespace', 'nodeport_origin', 'source_sha256']}))
