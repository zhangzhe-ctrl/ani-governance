#!/usr/bin/env python3
"""Fedora only: export an explicit, non-secret acceptance evidence set."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys

R=Path(sys.argv[1])
gov=R/'work/governance'
if (gov/'go.mod').is_file():
    # Explicit backend paths: never collect the whole repository after relocation.
    gov_scopes=['api','app','pkg','scripts','sql','tools','go.mod','go.sum',
                'Makefile','app.mk','Dockerfile','docker-compose.yaml','docker-compose.libs.yaml',
                '.dockerignore','.gitignore','.golangci.yml','.markdownlint.yaml','.yamllint',
                'AGENTS.md','CLAUDE.md','README.md','UPSTREAM.md']
elif (gov/'backend/go.mod').is_file():
    gov_scopes=['backend','UPSTREAM.md','docs/issues/model-list-first-slice.md','docs/model-catalog-bootstrap.md']
else:
    raise FileNotFoundError(f'Governance go.mod not found in {gov} or its backend directory')
E=R/'evidence'
out=R/'delivery'
out.mkdir(exist_ok=True)
history=[json.loads(line) for line in (E/'acceptance.jsonl').read_text().splitlines()]
latest={row['case']:row for row in history}
required=['V01-V02-a','V01-V02-b','V01-V02-empty','V03-no-token','V03-invalid-token',
'V03-no-permission-same-role-code','V04-platform','V04-frozen','V04-module-closed',
'V05-forged-headers','V05-rpc-tenant-mismatch','V05-duplicate-metadata',
'V06-no-certificate','V06-wrong-service-certificate','V06-other-business-rpc',
'V07-limit-1','V07-limit-100','V07-rpc-limit-1000','V07-rpc-limit-1001',
'V08-client-deadline','V08-model-unavailable','V08-model-recovered',
'V08-model-db-unavailable','V08-model-db-recovered','V09-force-logout',
'V09-reuse-revoked-token','V09-new-login-after-permission-removal','V09-permission-restored',
'V10-idempotent-bootstrap-and-two-process-restarts','V10-restarted-real-data',
'V10-uuid-immutable-public-update','V11-postgres-down-withdraws-readiness',
'V12-openapi-parameters','V12-inventory-permission-route','V12-final-real-query']
missing=[key for key in required if key not in latest]
failed=[row['case'] for row in latest.values() if row['status']!='pass']
if missing or failed:raise RuntimeError({'missing':missing,'latest_failed':failed})
(out/'acceptance.json').write_text(json.dumps({'status':'pass','latest_cases':list(latest.values()),'resolved_failed_attempts':[r for r in history if r['status']=='fail']},indent=2)+'\n')
for name in ['runtime-manifest.json','cluster-preflight.txt','public-response-a.json',
'model-targeted-tests.log','model-final-tests.log','model-composition-final-test.log',
'client-final-test.log','governance-service-tests-r2.log',
'generated-service-tests-final.log','domain-test.log','client-outage-test.log',
'uuid-mask-final-test.log','final-cluster.json','final-images.json','validation-commands.md']:
    shutil.copyfile(E/name,out/name)
shutil.copyfile(R/'inputs/proxy/manifest.json',out/'model-module-manifest.json')
assert (E/'generated-before.sha256').read_bytes()==(E/'generated-after.sha256').read_bytes()
(out/'generation-consistency.json').write_text(json.dumps({'status':'pass','sha256':hashlib.sha256((E/'generated-after.sha256').read_bytes()).hexdigest(),'entries':len((E/'generated-after.sha256').read_text().splitlines())},indent=2)+'\n')
for name,repo,scopes in [('governance',gov,gov_scopes),('model',R/'work/model',['cmd','internal','docs/governance-catalog.md'])]:
    def git(*args):return subprocess.check_output(['git','-C',str(repo),*args],text=True).splitlines()
    paths=sorted(set(git('diff','--name-only','HEAD','--',*scopes)+git('ls-files','--others','--exclude-standard','--',*scopes)))
    entries={p:hashlib.sha256((repo/p).read_bytes()).hexdigest() for p in paths if (repo/p).is_file()}
    (out/(name+'-sources.json')).write_text(json.dumps(entries,indent=2)+'\n')
    (R/(name+'-transfer.txt')).write_text('\n'.join(entries)+'\n')
snapshot=json.loads((E/'persistence-snapshot.json').read_text())
(out/'persistence-summary.json').write_text(json.dumps({k:{'rows':len(v),'sha256':hashlib.sha256(json.dumps(v,sort_keys=True).encode()).hexdigest()} for k,v in snapshot.items()},indent=2)+'\n')
print(json.dumps({'status':'pass','latest_cases':len(latest),'delivery':str(out)}))
