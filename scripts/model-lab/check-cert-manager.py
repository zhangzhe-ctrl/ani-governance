#!/usr/bin/env python3
"""Fedora-only focused check using the existing lab's users, data and probe.

Usage: python3 check-cert-manager.py EXISTING_LAB_TASK OUTPUT_JSON
Does not create fixtures or run the full acceptance suite.
"""
import importlib.util
import json
from pathlib import Path
import sys

task = Path(sys.argv[1])
output = Path(sys.argv[2])
source = task / 'work/governance'
if not (source / 'go.mod').is_file():
    source /= 'backend'
spec = importlib.util.spec_from_file_location('model_lab', source / 'scripts/model-lab/accept.py')
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)
try:
    lab.refresh_forwards()
    for label in ['a', 'b', 'denied']:
        tenant = lab.S['tenants'][label]
        lab.login(label, 'reader', lab.S['passwords'][label], tenant['code'])
    for label in ['a', 'b']:
        lab.httpcase('cert-manager-real-models-' + label, label,
                     body={'models': lab.expected(label)})
    lab.httpcase('cert-manager-no-login', 'missing', expected_code=401, denied=True)
    lab.httpcase('cert-manager-no-permission', 'denied', expected_code=403, denied=True)
    lab.httpcase('cert-manager-forged-tenant',
                 headers={'X-Ani-Tenant-Id': lab.S['tenants']['b']['uuid'],
                          'X-Ani-Actor': 'governance:user:1'},
                 body={'models': lab.expected('a')})
    lab.rpc('cert-manager-no-client-certificate', 'Unavailable', certificate='')
    lab.refresh_forwards()
    lab.rpc('cert-manager-wrong-service', 'Unavailable', certificate='wrong-service')
    lab.refresh_forwards()
    lab.httpcase('cert-manager-final-read', body={'models': lab.expected('a')})
finally:
    output.write_text(json.dumps(lab.results, indent=2) + '\n')
