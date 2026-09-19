#!/usr/bin/env python3
"""Historical 2026-09-19 collector for the pre-relocation source layout; see README.md."""
import hashlib, json, subprocess, sys
from pathlib import Path
R=Path(sys.argv[1]); OLD=Path(sys.argv[2]); NS='gov-model-20260919-01'
# The fixed baseline/core comparison below is part of the historical acceptance.
# Reject a relocated checkout before reading the cluster or writing any evidence.
if (R/'work/governance/go.mod').is_file() or not (R/'work/governance/backend/go.mod').is_file():
 raise SystemExit('Historical collector requires the pre-relocation Governance backend/ worktree; see scripts/network-lab/README.md')
old_gov=OLD/'work/governance'
if not (old_gov/'go.mod').is_file():old_gov=old_gov/'backend'
if not (old_gov/'go.mod').is_file():raise FileNotFoundError(f'Governance go.mod not found under {OLD / "work/governance"}')
E=R/'evidence'; C=old_gov/'scripts/model-lab/cluster.sh'
def cmd(args,**kw):return subprocess.check_output(args,stdin=subprocess.DEVNULL,text=True,**kw)
def cluster(s):return cmd(['bash',str(C),s])
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
rows=[json.loads(l) for l in (E/'acceptance.jsonl').read_text().splitlines()]
latest={r['case']:r for r in rows}
assert all(r['status']=='pass' for r in latest.values())
for required in ['N01-isolated-deployment','N02-incremental-api-inventory','N03-real-pg-fixtures','N04-tenant-a','N04-tenant-b','N05-no-existence-leak','N06-no-permission','N06-no-network-plan','N08-real-mtls','N08-no-cert','N08-wrong-cert','N09-same-session-model-a','N09-same-session-model-b','N10-network-recovered','N10-network-db-recovered','N10-connected-query-timeout','N11-permission-revoked','N11-permission-restored']:
 assert required in latest,required
(E/'acceptance.json').write_text(json.dumps({'status':'pass','cases':list(latest.values()),'not_verified':['network provisioning/data-plane','full-mode authentication','IAM workload grants','HA/capacity','frontend','production cutover']},indent=2)+'\n')
inventory=json.loads(cluster(f'kubectl -n {NS} get deployments,pods,pvc,jobs,services -o json'))
(E/'final-cluster.json').write_text(json.dumps(inventory,indent=2)+'\n')
# Preserve the migration job's actual initial image; refresh the final serving image.
rendered=json.loads((E/'network-runtime-manifest.json').read_text())
for obj in rendered['items']:
 if obj['kind']=='Deployment' and obj['metadata']['name']=='network':
  live=next(x for x in inventory['items'] if x['kind']=='Deployment' and x['metadata']['name']=='network')
  obj['spec']['template']['spec']['containers'][0]['image']=live['spec']['template']['spec']['containers'][0]['image']
(E/'network-runtime-manifest.json').write_text(json.dumps(rendered,indent=2)+'\n')
images=[{'pod':o['metadata']['name'],'containers':[{'name':c['name'],'image':c['image'],'imageID':c.get('imageID'),'ready':c.get('ready')} for c in o.get('status',{}).get('containerStatuses',[])]} for o in inventory['items'] if o['kind']=='Pod']
(E/'final-images.json').write_text(json.dumps(images,indent=2)+'\n')
role=json.loads(cluster(f'''kubectl -n {NS} exec deploy/network-db -- psql -X -qAt -U network -d network -c "SELECT row_to_json(x) FROM (SELECT rolname,rolsuper,rolcreatedb,rolcreaterole,rolbypassrls,has_table_privilege('network_read','network_vpcs','SELECT') AS can_select,has_table_privilege('network_read','network_vpcs','UPDATE') AS can_update,has_table_privilege('network_read','network_vpcs','INSERT') AS can_insert,has_table_privilege('network_read','network_vpcs','DELETE') AS can_delete FROM pg_roles WHERE rolname='network_read') x"'''))
assert role['can_select'] and not any(role[k] for k in ['rolsuper','rolcreatedb','rolcreaterole','rolbypassrls','can_update','can_insert','can_delete'])
(E/'runtime-db-role.json').write_text(json.dumps(role,indent=2)+'\n')
manifest={'execution_host':'ssh fedora','namespace':NS,'node':'ani-01 / 172.16.101.10','repositories':{}}
for name,base in [('governance','604d0fab96e423c65f5c042a7f87bad5050cb021'),('network','e481e968d3cc2f17bc4c6a736c438428519b09a0')]:
 repo=R/'work'/name
 paths=set(cmd(['git','diff','--name-only',base],cwd=repo).splitlines()+cmd(['git','ls-files','--others','--exclude-standard'],cwd=repo).splitlines())
 if name=='governance':paths={p for p in paths if p.startswith('backend/')}
 else:paths={p for p in paths if p.startswith(('cmd/','internal/'))}
 manifest['repositories'][name]={'base_commit':base,'changed_source_sha256':{p:digest(repo/p) for p in sorted(paths) if (repo/p).is_file()}}
 manifest['repositories'][name]['binary_sha256']=digest(R/('ani-governance' if name=='governance' else 'ani-network-service'))
core=['backend/pkg/middleware/auth','backend/app/admin/service/internal/data/authorizer_provider.go','backend/app/admin/service/internal/data/tenant_resource.go','backend/app/admin/service/internal/service/model_service.go','backend/app/admin/service/internal/data/model_client.go']
core_diff=cmd(['git','diff','604d0fab96e423c65f5c042a7f87bad5050cb021','--',*core],cwd=R/'work/governance')
assert not core_diff
manifest['login_authorization_tenant_mapping_and_model_unchanged']=True
(E/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
for name in ['ani-governance','ani-network-service']:
 (E/(name+'-build-info.txt')).write_text(cmd(['go','version','-m',str(R/name)]))
print(json.dumps({'status':'pass','cases':len(latest),'source_files':sum(len(v['changed_source_sha256']) for v in manifest['repositories'].values()),'core_unchanged':True}))
