#!/usr/bin/env python3
"""Fedora-only real HTTP/mTLS acceptance. Credentials stay task-private."""
import base64
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shlex
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid
import yaml

R = Path(sys.argv[1])
GOV = R / 'work/governance'
if not (GOV / 'go.mod').is_file():
    GOV = GOV / 'backend'
if not (GOV / 'go.mod').is_file():
    raise FileNotFoundError(f'Governance go.mod not found in {R / "work/governance"} or its backend directory')
LAB = GOV / 'scripts/model-lab'
NS = 'gov-model-20260919-01'
BASE = 'http://127.0.0.1:17788'
statefile = R / 'private/accept-state.json'
S = json.loads(statefile.read_text()) if statefile.exists() else {}
results = []

def save():
    statefile.write_text(json.dumps(S))
    os.chmod(statefile, 0o600)

def cluster(command, input=None):
    return subprocess.check_output(['bash', str(LAB/'cluster.sh'), command], input=input, text=True)

def sql(db, query, args=''):
    return cluster(f'kubectl -n {NS} exec -i deploy/{db}-db -- psql -X -qAt -U {db} -d {db} -v ON_ERROR_STOP=1 {args}', query).strip()

def request(path, token=None, data=None, headers=None, method=None, jar=None):
    h = {'Content-Type':'application/json', **(headers or {})}
    if token: h['Authorization'] = 'Bearer '+token
    req = urllib.request.Request(BASE+path, data=json.dumps(data).encode() if data is not None else None, headers=h, method=method)
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar)) if jar is not None else urllib.request.build_opener()
    started = time.monotonic()
    try:
        res = op.open(req, timeout=15)
    except urllib.error.HTTPError as e:
        res = e
    body = res.read().decode()
    return res.code, json.loads(body) if body else {}, round(time.monotonic()-started, 3), res.headers

def check(name, condition, **evidence):
    row = {'case':name, 'status':'pass' if condition else 'fail', **evidence}
    results.append(row)
    with (R/'evidence/acceptance.jsonl').open('a') as f: f.write(json.dumps(row)+'\n')
    print(json.dumps(row), flush=True)
    if not condition: raise RuntimeError(name)

def login(label, username, password, tenant=''):
    jar = http.cookiejar.CookieJar()
    code, captcha, _, _ = request('/api/v1/auth/captcha', jar=jar)
    if code != 200: raise RuntimeError('captcha: '+str((code,captcha)))
    cid = captcha['captchaId']
    answer = cluster(f'kubectl -n {NS} exec deploy/redis -- sh -c '+shlex.quote('REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --raw get '+shlex.quote('gowind:captcha:'+cid))).strip()
    encoded = subprocess.check_output([str(R/'tools/probe'),'-encrypt'], input=password, text=True).strip()
    # 平台与租户登录已拆为两个端点：tenant 为空走平台端点（报文不含 tenant_name），
    # 非空走租户端点并传 tenant_name（取值 sys_tenants.code）。
    # 拆分的意义在于消除静默降级——单端点下漏传租户会被当成平台登录。
    if tenant:
        path = '/api/v1/auth/password/login'
        payload = {'tenant_name':tenant,'username':username,'password':encoded}
    else:
        path = '/api/v1/auth/platform/password/login'
        payload = {'username':username,'password':encoded}
    code, body, _, headers = request(path, data=payload, headers={'X-Captcha-Id':cid,'X-Captcha-Value':answer}, jar=jar)
    if code != 200: raise RuntimeError('login '+label+': '+str((code,body)))
    token = body['access_token']
    cookies = [c for c in jar if c.name == 'refresh_token']
    check('real-login-'+label, bool(token) and bool(cookies) and cookies[0].has_nonstandard_attr('HttpOnly'), http=code, http_only_refresh_cookie=bool(cookies), tenant_name=tenant)
    S[label] = token
    save()
    return token

def setup():
    p = login('platform','admin','Abcd@1234')
    base = int(sql('governance', "SELECT id FROM sys_permissions WHERE code='sys:access_backend';"))
    S.setdefault('tenants', {})
    for label in ['a','b','empty','denied']:
        code = 'model-lab-'+label
        tid = sql('governance', f"SELECT id FROM sys_tenants WHERE code='{code}';")
        S.setdefault('passwords', {}).setdefault(label, 'Aa1@'+secrets.token_hex(12))
        save()
        if not tid:
            status, body, _, _ = request('/admin/v1/tenants:with-admin',p,{'tenant':{'name':'Model lab '+label,'code':code,'status':'ON'},'user':{'username':'manager','status':'NORMAL'},'password':S['passwords'][label]})
            check('create-tenant-'+label, status==200, http=status, response=body)
            tid = sql('governance',f"SELECT id FROM sys_tenants WHERE code='{code}';")
        tid=int(tid)
        rid = sql('governance',f"SELECT id FROM sys_roles WHERE tenant_id={tid} AND code='tenant:model_reader';")
        if not rid:
            status, body, _, _ = request('/admin/v1/roles',p,{'data':{'tenantId':tid,'name':'Model reader','code':'tenant:model_reader','type':'TENANT','status':'ON','permissions':[base]}})
            check('create-role-'+label, status==200,http=status,response=body)
            rid=sql('governance',f"SELECT id FROM sys_roles WHERE tenant_id={tid} AND code='tenant:model_reader';")
        rid=int(rid)
        uid=sql('governance',f"SELECT id FROM sys_users WHERE tenant_id={tid} AND username='reader';")
        if not uid:
            status, body, _, _=request('/admin/v1/users',p,{'data':{'tenantId':tid,'username':'reader','status':'NORMAL','roleIds':[rid]},'password':S['passwords'][label]})
            check('create-reader-'+label,status==200,http=status,response=body)
            uid=sql('governance',f"SELECT id FROM sys_users WHERE tenant_id={tid} AND username='reader';")
        S['tenants'][label]={'id':tid,'role':rid,'user':int(uid),'uuid':sql('governance',f'SELECT resource_tenant_id FROM sys_tenants WHERE id={tid};'),'code':code}
        save()
        if label!='denied':
            sql('governance',(LAB.parent/'bootstrap-model-access.sql').read_text(), f'-v tenant_id={tid} -v role_id={rid}')
        else:
            sql('governance',f"UPDATE sys_tenants SET plan_id=(SELECT id FROM sys_plans WHERE name='ANI Model Catalog') WHERE id={tid} AND plan_id IS NULL;")
    check('distinct-persistent-uuids',len({t['uuid'] for t in S['tenants'].values()})==4)

def fixture():
    for label,count in [('a',105),('b',3)]:
        tid=S['tenants'][label]['uuid']
        rows=[]
        for i in range(count):
            mid=str(uuid.uuid5(uuid.NAMESPACE_URL,f'gov-model-20260919-01/{label}/{i}'))
            status=['ready','pending','downloading','error'][i%4]
            rows.append(f"('{tid}','{mid}','{label}-{i:03d}','external-{label}-{i:03d}','Display {label} {i}','builtin','[\"chat\"]','{status}','2026-09-19T00:00:00Z'::timestamptz + interval '{i//2} seconds')")
        sql('model','INSERT INTO models(tenant_id,id,name,model_id,display_name,source,capabilities,status,created_at) VALUES '+','.join(rows)+' ON CONFLICT(tenant_id,id) DO NOTHING;')
    check('postgres-fixture',sql('model','SELECT count(*) FROM models;')=='108',rows=108)

def expected(label,limit=100,status=None):
    tid=S['tenants'][label]['uuid']
    where=f" AND status='{status}'" if status else ''
    raw=sql('model',f"SELECT coalesce(json_agg(x),'[]') FROM (SELECT id::text,model_id,name,display_name,source,capabilities,status FROM models WHERE tenant_id='{tid}' AND deleted_at IS NULL {where} ORDER BY created_at DESC,id DESC LIMIT {limit}) x;")
    return json.loads(raw)

def metrics():
    raw=urllib.request.urlopen('http://127.0.0.1:17991/metrics',timeout=5).read().decode()
    lines=[l for l in raw.splitlines() if not l.startswith('#') and 'requests' in l and 'ListModels' in l]
    return sum(float(l.rsplit(' ',1)[1]) for l in lines)

def httpcase(name,label='a',query='',expected_code=200,body=None,headers=None,denied=False):
    before=metrics() if denied else None
    code,data,elapsed,_=request('/api/v1/models'+query,S.get(label),headers=headers)
    ok=code==expected_code and (body is None or data==body)
    ev={'http':code,'seconds':elapsed}
    if code==200:ev.update(count=len(data.get('models',[])),only_projected_fields=all(set(m)=={'id','model_id','name','display_name','source','capabilities','status'} for m in data.get('models',[])))
    else:ev['reason']=data.get('reason')
    if denied:
        ev['model_business_calls_delta']=metrics()-before
        ok=ok and ev['model_business_calls_delta']==0
    check(name,ok,**ev)
    return data

def rpc(name,code='OK',**args):
    cmd=[str(R/'tools/probe'),'-ca',str(R/'private/ca.pem'),'-tenant',S['tenants']['a']['uuid']]
    cert=args.pop('certificate','ani-governance')
    if cert:cmd += ['-cert',str(R/'private'/ (cert+'.pem')),'-key',str(R/'private'/(cert+'.key'))]
    for key,value in args.items():
        cmd += ['-'+key,str(value)] if value is not True else ['-'+key]
    result=json.loads(subprocess.check_output(cmd,text=True))
    check(name,result['code']==code,**result)

def contract():
    for label in ['a','b','empty']:
        httpcase('V01-V02-'+label,label,body={'models':expected(label)})
    httpcase('V03-no-token','missing',expected_code=401,denied=True)
    S['invalid']='not-a-token'
    httpcase('V03-invalid-token','invalid',expected_code=401,denied=True)
    httpcase('V03-no-permission-same-role-code','denied',expected_code=403,denied=True)
    httpcase('V04-platform','platform',expected_code=403,denied=True)
    tid=S['tenants']['a']['id']
    sql('governance',f"UPDATE sys_tenants SET status='FREEZE' WHERE id={tid};")
    try:httpcase('V04-frozen',expected_code=403,denied=True)
    finally:sql('governance',f"UPDATE sys_tenants SET status='ON' WHERE id={tid};")
    plan=sql('governance',f'SELECT plan_id FROM sys_tenants WHERE id={tid};')
    sql('governance',f'UPDATE sys_tenants SET plan_id=NULL WHERE id={tid};')
    try:httpcase('V04-module-closed',expected_code=403,denied=True)
    finally:sql('governance',f'UPDATE sys_tenants SET plan_id={plan} WHERE id={tid};')
    httpcase('V05-forged-headers',headers={'X-Tenant-Id':str(S['tenants']['b']['id']),'X-User-Id':'1','X-Ani-Tenant-Id':S['tenants']['b']['uuid'],'X-Ani-Actor':'governance:user:1','X-Ani-Request-Id':'spoof'},body={'models':expected('a')})
    for key in ['tenant_id','tenantId','user_id','userId']:
        httpcase('V05-query-'+key,query='?'+key+'=1',expected_code=400,denied=True)
    for limit in [1,100]:
        httpcase('V07-limit-'+str(limit),query='?limit='+str(limit),body={'models':expected('a',limit)})
    for status in ['ready','pending','downloading','error']:
        httpcase('V07-status-'+status,query='?status='+status,body={'models':expected('a',status=status)})
    for query in ['limit=0','limit=101','limit=-1','limit=abc','limit=','limit=1&limit=2','status=','status=deleted','status=ready&status=ready','unknown=1','cursor=foo','keyword=foo','source=upload','capability=chat']:
        httpcase('V07-reject-'+query,query='?'+query,expected_code=400,denied=True)
    rpc('V05-rpc-tenant-mismatch','PermissionDenied',**{'rpc-tenant':S['tenants']['b']['uuid']})
    rpc('V05-duplicate-metadata','Unauthenticated',duplicate=True)
    rpc('V06-no-certificate','Unavailable',certificate='')
    refresh_forwards()  # kubectl may close its forwarding stream on TLS rejection.
    rpc('V06-wrong-service-certificate','Unavailable',certificate='wrong-service')
    refresh_forwards()
    rpc('V06-other-business-rpc','PermissionDenied',rpc='DeleteModel')
    rpc('V07-rpc-limit-1000',limit=1000)
    rpc('V07-rpc-limit-1001','InvalidArgument',limit=1001)
    for key in ['cursor','keyword','source','capability']:
        rpc('V07-rpc-'+key,'InvalidArgument',filter=key)

def snapshot():
    out={}
    for name,query in {
      'tenant_uuids':'SELECT id,resource_tenant_id FROM sys_tenants ORDER BY id',
      'api_inventory':'SELECT id,path,method,business_module FROM sys_apis ORDER BY id',
      'permission_bindings':'SELECT permission_id,api_id FROM sys_permission_apis ORDER BY 1,2',
      'role_bindings':'SELECT role_id,permission_id,tenant_id,effect FROM sys_role_permissions ORDER BY 1,2,3,4',
    }.items():out[name]=sql('governance',f"SELECT coalesce(json_agg(x),'[]') FROM ({query}) x;")
    out['models']=sql('model',"SELECT coalesce(json_agg(x),'[]') FROM (SELECT * FROM models ORDER BY tenant_id,id) x;")
    return out

def refresh_forwards():
    subprocess.run(['bash',str(LAB/'forward.sh'),str(R)],check=True)
    deadline=time.monotonic()+20
    while True:
        try:
            urllib.request.urlopen(BASE+'/api/v1/auth/captcha',timeout=2).close()
            urllib.request.urlopen('http://127.0.0.1:17991/readyz',timeout=2).close()
            return
        except (OSError,urllib.error.URLError):
            if time.monotonic()>deadline:raise
            time.sleep(.5)

def restart(*names):
    cluster(f'kubectl -n {NS} rollout restart '+ ' '.join('deployment/'+n for n in names))
    for name in names:cluster(f'kubectl -n {NS} rollout status deployment/{name} --timeout=60s')
    refresh_forwards()

def recovery():
    # Lock acquisition is witnessed in pg_locks before the real HTTP call.
    lockcmd=f'kubectl -n {NS} exec -i deploy/model-db -- psql -X -qAt -U model -d model -v ON_ERROR_STOP=1'
    logfile=(R/'evidence/timeout-lock.log').open('w')
    proc=subprocess.Popen(['bash',str(LAB/'cluster.sh'),lockcmd],stdin=subprocess.PIPE,stdout=logfile,stderr=logfile,text=True)
    proc.stdin.write('BEGIN; LOCK TABLE models IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(8); ROLLBACK;\n');proc.stdin.close()
    for _ in range(20):
        if sql('model',"SELECT count(*) FROM pg_locks WHERE relation='models'::regclass AND mode='AccessExclusiveLock' AND granted;")=='1':break
        time.sleep(.2)
    else:raise RuntimeError('timeout lock not acquired')
    started=time.monotonic()
    httpcase('V08-client-deadline',expected_code=504)
    check('V08-timeout-bounded',time.monotonic()-started<4)
    if proc.wait(timeout=12)!=0:raise RuntimeError('lock process failed')
    logfile.close()
    httpcase('V08-timeout-recovered',body={'models':expected('a')})
    for deployment in ['model','model-db']:
        cluster(f'kubectl -n {NS} scale deployment/{deployment} --replicas=0')
        cluster(f'kubectl -n {NS} wait --for=delete pod -l app={deployment} --timeout=60s')
        try:
            if deployment=='model-db':
                try:urllib.request.urlopen('http://127.0.0.1:17991/readyz',timeout=4);ready=200
                except urllib.error.HTTPError as err:ready=err.code
                check('V11-postgres-down-withdraws-readiness',ready==503,http=ready)
            httpcase('V08-'+deployment+'-unavailable',expected_code=503)
        finally:
            cluster(f'kubectl -n {NS} scale deployment/{deployment} --replicas=1')
            cluster(f'kubectl -n {NS} rollout status deployment/{deployment} --timeout=60s')
            if deployment=='model-db':cluster(f'kubectl -n {NS} rollout status deployment/model --timeout=30s')
            refresh_forwards()
        httpcase('V08-'+deployment+'-recovered',body={'models':expected('a')})
    before=snapshot()
    for label in ['a','b','empty']:
        t=S['tenants'][label]
        sql('governance',(LAB.parent/'bootstrap-model-access.sql').read_text(), f"-v tenant_id={t['id']} -v role_id={t['role']}")
    sql('governance',(LAB/'bootstrap-platform.sql').read_text())
    restart('governance','model')
    after=snapshot()
    check('V10-idempotent-bootstrap-and-two-process-restarts',before==after,unchanged={k:before[k]==after[k] for k in before})
    (R/'evidence/persistence-snapshot.json').write_text(json.dumps({k:json.loads(v) for k,v in after.items()},indent=2))
    httpcase('V10-restarted-real-data',body={'models':expected('a')})

def revoke():
    t=S['tenants']['a'];old=S['a']
    payload=json.loads(base64.urlsafe_b64decode(old.split('.')[1]+'==='))
    code,body,_,_=request('/admin/v1/online-session/force-logout',S['platform'],{'clientType':'admin','userId':t['user'],'jti':payload['jti']})
    check('V09-force-logout',code==200,http=code,response=body)
    httpcase('V09-reuse-revoked-token',expected_code=401,denied=True)
    sql('governance',f"DELETE FROM sys_role_permissions WHERE role_id={t['role']} AND tenant_id={t['id']} AND permission_id=(SELECT id FROM sys_permissions WHERE code='model:catalog:list');")
    restart('governance')
    login('a','reader',S['passwords']['a'],t['code'])
    httpcase('V09-new-login-after-permission-removal',expected_code=403,denied=True)
    sql('governance',(LAB.parent/'bootstrap-model-access.sql').read_text(), f"-v tenant_id={t['id']} -v role_id={t['role']}")
    restart('governance')
    login('a','reader',S['passwords']['a'],t['code'])
    httpcase('V09-permission-restored',body={'models':expected('a')})

def document_contract():
    spec=yaml.safe_load((GOV/'app/admin/service/cmd/server/assets/openapi.yaml').read_text())
    operation=spec['paths']['/api/v1/models']['get']
    params={p['name']:p for p in operation['parameters']}
    # gnostic 0.7.1 does not copy property constraints onto GET query schemas;
    # the generated operation description carries those bounds explicitly.
    check('V12-openapi-parameters',set(params)=={'limit','status'} and 'defaults to 100' in operation['description'] and '1-100' in operation['description'] and set(operation['responses'])=={'200','400','401','403','503','504'},parameters=params,responses=list(operation['responses']))
    inventory=json.loads(sql('governance',"SELECT json_agg(x) FROM (SELECT a.id,a.path,a.method,a.business_module,p.code FROM sys_apis a JOIN sys_permission_apis pa ON pa.api_id=a.id JOIN sys_permissions p ON p.id=pa.permission_id WHERE a.path='/api/v1/models' AND p.code='model:catalog:list') x;"))
    check('V12-inventory-permission-route',len(inventory)==1 and inventory[0]['method']=='GET' and inventory[0]['business_module']=='MODEL',inventory=inventory)
    t=S['tenants']['a'];before=t['uuid']
    code,body,_,_=request('/admin/v1/tenants/'+str(t['id']),S['platform'],{'data':{'resourceTenantId':S['tenants']['b']['uuid']},'updateMask':'resourceTenantId'},method='PUT')
    after=sql('governance',f"SELECT resource_tenant_id FROM sys_tenants WHERE id={t['id']};")
    check('V10-uuid-immutable-public-update',before==after and code in [200,400],http=code,unchanged=before==after)
    (R/'evidence/public-response-a.json').write_text(json.dumps(request('/api/v1/models?limit=1',S['a'])[1],indent=2)+'\n')
    check('V12-final-real-query',request('/api/v1/models',S['a'])[0]==200)

if __name__ == '__main__':
    if sys.argv[2]=='setup': setup()
    elif sys.argv[2]=='login':
        for label,t in S['tenants'].items():login(label,'reader',S['passwords'][label],t['code'])
    elif sys.argv[2]=='fixture':fixture()
    elif sys.argv[2]=='contract':contract()
    elif sys.argv[2]=='recovery':recovery()
    elif sys.argv[2]=='revoke':revoke()
    elif sys.argv[2]=='document':document_contract()
