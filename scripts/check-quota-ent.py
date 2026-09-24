#!/usr/bin/env python3
"""Quota storage uses Ent builders and one Ent transaction; no driver bypass."""
from pathlib import Path
import re

root = Path('app/admin/service/internal/data')
paths = sorted(set(root.glob('quota*.go')) | set(root.glob('plan_quota*.go')))
errors = []
for path in paths:
    if path.name.endswith('_test.go'):
        continue
    source = path.read_text()
    for pattern in (r'"github.com/jackc/pgx', r'/quotasql"', r'\.Raw\(', r'\.BeginTx\(',
                    r'\.(?:ExecContext|QueryContext|QueryRowContext)\(', r'\.DB\(\)\.(?:Conn|Begin)\('):
        if re.search(pattern, source):
            errors.append(f'{path}: quota storage bypasses Ent: {pattern}')
if (root / 'quotasql').exists() or Path('sqlc.yaml').exists():
    errors.append('Governance quota sqlc artifacts must not be restored')
if errors:
    raise SystemExit('\n'.join(errors))
print('quota Ent boundary: pass')
