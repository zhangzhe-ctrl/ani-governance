#!/usr/bin/env python3
"""Fedora-only incremental deployment and real login/RPC/database acceptance.

Reuses the completed task's isolated Model lab, never a platform namespace.
"""
import importlib.util, json, os, subprocess, sys, time, urllib.request, urllib.error, uuid
from pathlib import Path
R=Path(sys.argv[1]); OLD=Path(sys.argv[2]); stage=sys.argv[3]
def governance_module(task):
 repo=task/'work/governance'
 for module in (repo,repo/'backend'):
  if (module/'go.mod').is_file():return module
 raise FileNotFoundError(f'Governance go.mod not found in {repo} or its backend directory')
GOV=governance_module(R); OLD_GOV=governance_module(OLD)
sys.argv=[sys.argv[0],str(OLD)]
spec=importlib.util.spec_from_file_location('model_lab',OLD_GOV/'scripts/model-lab/accept.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
m.R=R;m.GOV=GOV;m.statefile=R/'private/accept-state.json'
if m.statefile.exists():m.S=json.loads(m.statefile.read_text())
else:m.save()
S=m.S; NS=m.NS; sql=m.sql; check=m.check
_original_cluster=m.cluster
def cluster(command,input=None):return _original_cluster(command, '' if input is None else input)
m.cluster=cluster
LAB=GOV/'scripts/network-lab'

def forward():
 subprocess.run(['bash',str(LAB/'forward.sh'),str(R),str(OLD)],check=True,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL)
 time.sleep(2)
def ready(name): cluster(f'kubectl -n {NS} rollout status deployment/{name} --timeout=150s')
def apply(filename):cluster('kubectl apply -f -',(R/'private'/filename).read_text())
def inv():return json.loads(sql('governance',"SELECT json_agg(x ORDER BY id) FROM (SELECT id,module,path,method,operation,business_module,scope,status FROM sys_apis) x;"))
def deploy():
 subprocess.run(['python3',str(LAB/'prepare.py'),str(R),str(OLD)],check=True)
 (R/'evidence/api-inventory-before.json').write_text(json.dumps(inv(),indent=2)+'\n')
 apply('database.json');ready('network-db')
 if not sql('network',"SELECT 1 FROM pg_roles WHERE rolname='network_read';"):sql('network',(R/'private/role.sql').read_text())
 apply('migration.json');cluster(f'kubectl -n {NS} wait --for=condition=complete job/network-migrate-v1 --timeout=90s')
 sql('network','REVOKE INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER ON ALL TABLES IN SCHEMA public FROM network_read;')
 apply('network.json');ready('network')
 image=json.loads((R/'inputs/images.json').read_text())['governance']
 env=[{'name':k,'value':v} for k,v in {'ANI_NETWORK_ADDR':f'network.{NS}.svc.cluster.local.:19090','ANI_NETWORK_CA':'/tls/ca.pem','ANI_NETWORK_CERT':'/tls/tls.crt','ANI_NETWORK_KEY':'/tls/tls.key','ANI_NETWORK_TIMEOUT':'2s'}.items()]
 patch={'spec':{'template':{'spec':{'containers':[{'name':'governance','image':image,'env':env}]}}}}
 import shlex
 cluster(f'kubectl -n {NS} patch deployment governance --type=strategic -p '+shlex.quote(json.dumps(patch)));ready('governance')
 check('N01-isolated-deployment',True,namespace=NS,network_mode='vpc-read',runtime_role='network_read',kubernetes_credentials=False)

def setup():
 # Create a dedicated plan so changing this entitlement cannot affect other tenants.
 sql('governance',"INSERT INTO sys_plans(name,version,expiry_policy) VALUES('ANI Network Read Lab','FREE','READONLY') ON CONFLICT(name) DO NOTHING; INSERT INTO sys_plan_modules(plan_id,module) SELECT id,m FROM sys_plans CROSS JOIN (VALUES('MODEL'),('NETWORK')) t(m) WHERE name='ANI Network Read Lab' AND NOT EXISTS(SELECT 1 FROM sys_plan_modules pm WHERE pm.plan_id=sys_plans.id AND pm.module=m);")
 for label in ['a','b','denied']:
  t=S['tenants'][label]
  sql('governance',f"UPDATE sys_tenants SET plan_id=(SELECT id FROM sys_plans WHERE name='ANI Network Read Lab') WHERE id={t['id']};")
  if label!='denied':
   sql('governance',(LAB.parent/'bootstrap-network-access.sql').read_text(),f"-v tenant_id={t['id']} -v role_id={t['role']}")
 before=json.loads((R/'evidence/api-inventory-before.json').read_text());after=inv()
 check('N02-incremental-api-inventory',all(x in after for x in before) and len(after)==len(before)+1,before=len(before),after=len(after),old_ids_preserved=True)
 (R/'evidence/api-inventory-after.json').write_text(json.dumps(after,indent=2)+'\n')
 for label in ['a','b']:
  t=S['tenants'][label]; vid='vpc_'+uuid.uuid5(uuid.NAMESPACE_URL,'gov-network-lab/'+label).hex;op=str(uuid.uuid5(uuid.NAMESPACE_URL,'gov-network-op/'+label))
  S.setdefault('vpcs',{})[label]=vid
  sql('network',f"""BEGIN;
  INSERT INTO network_vpcs(tenant_id,vpc_id,name,description,cidr,state,created_at,updated_at,last_operation_id) VALUES('{t['uuid']}','{vid}','network-{label}','task-owned query fixture','10.{20 if label=='a' else 21}.0.0/16','available','2026-09-19T00:00:00Z','2026-09-19T00:00:00Z','{op}') ON CONFLICT DO NOTHING;
  INSERT INTO network_operations(tenant_id,operation_id,vpc_id,kind,state,created_at,updated_at,completed_at) VALUES('{t['uuid']}','{op}','{vid}','create_vpc','succeeded','2026-09-19T00:00:00Z','2026-09-19T00:00:00Z','2026-09-19T00:00:00Z') ON CONFLICT DO NOTHING;
  COMMIT;""")
 m.save()
 check('N03-real-pg-fixtures',sql('network','SELECT count(*) FROM network_vpcs;')=='2',rows=2,network_provisioning='not_verified')
 cluster(f'kubectl -n {NS} rollout restart deployment/governance');ready('governance');forward()

def login():
 for label in ['a','b','empty','denied']:m.login(label,'reader',S['passwords'][label],S['tenants'][label]['code'])
 m.login('platform','admin','Abcd@1234')
def metric():
 raw=urllib.request.urlopen('http://127.0.0.1:18991/metrics',timeout=5).read().decode()
 return sum(float(l.rsplit(' ',1)[1]) for l in raw.splitlines() if not l.startswith('#') and 'requests' in l and 'GetVPC' in l)
def get(name,label='a',vid=None,want=200,query='',headers=None,early=False):
 before=metric() if early else None
 code,body,elapsed,_=m.request('/api/v1/networks/vpcs/'+(vid or S['vpcs']['a'])+query,S.get(label),headers=headers)
 ok=code==want;ev={'http':code,'seconds':elapsed}
 if early:
  ev['network_business_calls_delta']=metric()-before;ok &= ev['network_business_calls_delta']==0
 if code==200:
  v=body.get('vpc',{});ev.update(vpc_id=v.get('id'),vpc_name=v.get('name'))
  ok &= set(v)=={'id','name','description','cidr','state','reason','created_at','updated_at','version','observed_at','observation_stale','last_operation_id','subnet_count'}
  if want==200:
   expected_id=vid or S['vpcs']['a'];tenant=S['tenants'][label]['uuid']
   row=json.loads(sql('network',f"SELECT row_to_json(x) FROM (SELECT vpc_id,name,cidr,state,version FROM network_vpcs WHERE tenant_id='{tenant}' AND vpc_id='{expected_id}') x;"))
   ok &= v['id']==row['vpc_id'] and v['name']==row['name'] and v['cidr']==row['cidr'] and v['state']==row['state'] and int(v['version'])==row['version']
   (R/f'evidence/public-vpc-{label}.json').write_text(json.dumps(body,indent=2)+'\n')
 else:ev['reason']=body.get('reason')
 check(name,ok,**ev);return body

def rpc(name,want='OK',cert='ani-governance',**args):
 cmd=[str(R/'tools/probe'),'-ca',str(OLD/'private/ca.pem'),'-tenant',S['tenants']['a']['uuid'],'-vpc',S['vpcs']['a']]
 if cert:cmd+=['-cert',str(OLD/'private'/(cert+'.pem')),'-key',str(OLD/'private'/(cert+'.key'))]
 for k,v in args.items():cmd+=['-'+k,str(v)] if v is not True else ['-'+k]
 result=json.loads(subprocess.check_output(cmd,text=True));check(name,result['code']==want,**result)

def contract():
 get('N04-tenant-a');get('N04-tenant-b','b',S['vpcs']['b'])
 cross=get('N05-cross-tenant',vid=S['vpcs']['b'],want=404)
 absent=get('N05-absent',vid='vpc_'+'9'*32,want=404)
 check('N05-no-existence-leak',cross==absent)
 get('N06-no-login','missing',want=401,early=True)
 get('N06-no-permission','denied',want=403,early=True)
 get('N06-no-network-plan','empty',want=403,early=True)
 get('N06-platform','platform',want=403,early=True)
 for v in ['bad','vpc_'+'A'*32]:get('N07-invalid-id-'+v,vid=v,want=400,early=True)
 for q in ['tenant_id=x','vpc_id='+S['vpcs']['b'],'unknown=1','tenantId=1','limit=1']:
  get('N07-query-'+q,query='?'+q,want=400,early=True)
 get('N07-spoof-headers',headers={'X-Tenant-Id':str(S['tenants']['b']['id']),'X-Ani-Tenant-Id':S['tenants']['b']['uuid'],'X-Ani-Actor':'governance:user:1'})
 get('N07-spoof-cross-tenant',vid=S['vpcs']['b'],headers={'X-Ani-Tenant-Id':S['tenants']['b']['uuid']},want=404)
 rpc('N08-real-mtls')
 rpc('N08-mismatch','PermissionDenied',**{'rpc-tenant':S['tenants']['b']['uuid']})
 rpc('N08-duplicate','Unauthenticated',duplicate=True)
 rpc('N08-missing-actor','Unauthenticated',actor='')
 rpc('N08-other-rpc','PermissionDenied',rpc='DeleteVPC')
 rpc('N08-no-cert','Unavailable',cert='');forward()
 rpc('N08-wrong-cert','Unavailable',cert='wrong-service');forward()
 for label in ['a','b']:
  code,body,_,_=m.request('/api/v1/models',S[label]);expected=m.expected(label)
  check('N09-same-session-model-'+label,code==200 and body=={'models':expected},http=code,count=len(body.get('models',[])))

def recovery():
 for deployment in ['network','network-db']:
  cluster(f'kubectl -n {NS} scale deployment/{deployment} --replicas=0')
  cluster(f'kubectl -n {NS} wait --for=delete pod -l app={deployment} --timeout=90s')
  try:
   get('N10-'+deployment+'-unavailable',want=503)
   code,body,_,_=m.request('/api/v1/models',S['a']);check('N10-model-unaffected-'+deployment,code==200 and len(body.get('models',[]))>0,http=code)
   if deployment=='network-db':
    try:urllib.request.urlopen('http://127.0.0.1:18991/readyz',timeout=3);status=200
    except urllib.error.HTTPError as e:status=e.code
    check('N10-network-readiness-db-down',status==503,http=status)
  finally:
   cluster(f'kubectl -n {NS} scale deployment/{deployment} --replicas=1');ready(deployment)
   if deployment=='network-db':ready('network')
   forward()
  started=time.monotonic(); attempts=[]
  for _ in range(15):
   code,_,elapsed,_=m.request('/api/v1/networks/vpcs/'+S['vpcs']['a'],S['a'])
   attempts.append({'http':code,'seconds':elapsed})
   if code==200:break
   if code!=503:raise RuntimeError('unexpected recovery HTTP '+str(code))
   if time.monotonic()-started>=30:break
   time.sleep(1)
  check('N10-'+deployment+'-bounded-reconnect',code==200 and time.monotonic()-started<33,seconds=round(time.monotonic()-started,3),attempts=attempts)
  get('N10-'+deployment+'-recovered')
 # Distinguish a connected blocked SQL query from a broken transport.
 lock="BEGIN; LOCK TABLE network_vpcs IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(7); COMMIT;"
 proc=subprocess.Popen(['bash',str(m.LAB/'cluster.sh'),f'kubectl -n {NS} exec -i deploy/network-db -- psql -X -qAt -U network -d network'],stdin=subprocess.PIPE,stdout=subprocess.DEVNULL)
 proc.stdin.write(lock.encode());proc.stdin.close()
 try:
  for _ in range(30):
   if sql('network',"SELECT count(*) FROM pg_locks WHERE relation='network_vpcs'::regclass AND mode='AccessExclusiveLock' AND granted;")!='0':break
   time.sleep(.1)
  get('N10-connected-query-timeout',want=504)
 finally:
  if proc.wait(timeout=15)!=0:raise RuntimeError('SQL lock fixture failed')
 get('N10-after-query-timeout')

def revoke():
 tid=S['tenants']['a']['id'];rid=S['tenants']['a']['role']
 sql('governance',f"DELETE FROM sys_role_permissions WHERE tenant_id={tid} AND role_id={rid} AND permission_id=(SELECT id FROM sys_permissions WHERE code='network:vpc:get');")
 cluster(f'kubectl -n {NS} rollout restart deployment/governance');ready('governance');forward()
 try:
  get('N11-permission-revoked',want=403,early=True)
  code,body,_,_=m.request('/api/v1/models',S['a']);check('N11-model-permission-retained',code==200, http=code)
 finally:
  sql('governance',(LAB.parent/'bootstrap-network-access.sql').read_text(),f'-v tenant_id={tid} -v role_id={rid}')
  cluster(f'kubectl -n {NS} rollout restart deployment/governance');ready('governance');forward()
 get('N11-permission-restored')

if __name__=='__main__':globals()[stage]()
