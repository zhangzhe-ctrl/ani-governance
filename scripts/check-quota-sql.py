#!/usr/bin/env python3
"""Enforce the complete quota/GPU data slice; unrelated Ent modules stay scoped out.

Generated sqlc code is exempt only because verify-quota-sqlc first regenerates it
and compares every byte. This scanner must not acquire a broad generated-code or
quota_lab exception. It also catches reintroduced Ent persistence in this slice.
"""
from pathlib import Path
import re

root = Path('app/admin/service/internal/data')
files = sorted(set(root.glob('quota*.go')) | set(root.glob('plan_quota*.go')) | set(root.glob('gpu*.go')))
errors = []
for path in files:
    if path.name.endswith('_test.go'):
        continue
    text = path.read_text()
    for literal in re.findall(r'`[^`]*`|"(?:[^"\\]|\\.)*"', text, re.S):
        if re.search(r'\b(?:SELECT\s+.+\s+FROM|INSERT\s+INTO|UPDATE\s+\w+\s+SET|DELETE\s+FROM|CREATE\s+TABLE|ALTER\s+TABLE|SET\s+ROLE|SET\s+row_security)\b', literal, re.I | re.S):
            errors.append(f'{path}: embedded SQL is forbidden')
    if re.search(r'\.Client\(\)\.(?:Quota\w+|PlanQuota|Tenant|Plan)\.', text):
        errors.append(f'{path}: Ent persistence is forbidden in the migrated slice')
    if re.search(r'\.(?:ExecContext|QueryContext|QueryRowContext)\(', text):
        errors.append(f'{path}: direct database/sql query is forbidden')
    if re.search(r'\.(?:Exec|Query|QueryRow|SendBatch|CopyFrom)\(', text):
        errors.append(f'{path}: direct driver SQL entry point is forbidden')
if errors:
    raise SystemExit('\n'.join(errors))
print(f'quota SQL boundary: {len(files)} files checked')
