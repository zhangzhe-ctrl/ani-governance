#!/usr/bin/env python3
"""Fedora-only extension of the retained task namespace; no shared resources."""
import base64, json, os, re, secrets, subprocess, sys
from pathlib import Path
import yaml
R=Path(sys.argv[1]); OLD=Path(sys.argv[2]); NS='gov-model-20260919-01'
p=R/'private'; p.mkdir(exist_ok=True); os.chmod(p,0o700)
if not (p/'secrets.json').exists():
    (p/'secrets.json').write_text(json.dumps({k:secrets.token_hex(24) for k in ['owner','reader','cursor']}))
    os.chmod(p/'secrets.json',0o600)
s=json.loads((p/'secrets.json').read_text())
def ssl(*args): subprocess.run(['openssl',*args],cwd=p,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
if not (p/'network.pem').exists():
    ssl('req','-new','-newkey','rsa:2048','-nodes','-subj','/CN=ani-network-service','-keyout','network.key','-out','network.csr')
    (p/'network.ext').write_text('subjectAltName=DNS:ani-network-service\nextendedKeyUsage=serverAuth\nkeyUsage=digitalSignature,keyEncipherment\n')
    ssl('x509','-req','-in','network.csr','-CA',str(OLD/'private/ca.pem'),'-CAkey',str(OLD/'private/ca.key'),'-set_serial',str(secrets.randbits(120)),'-days','7','-extfile','network.ext','-out','network.pem')
    os.chmod(p/'network.key',0o600)
config=yaml.safe_load(re.sub(r'\$\{[^:}]+:([^}]*)\}',r'\1',(R/'work/network/configs/config.yaml').read_text()))
config['network'].update(database_dsn=f"postgres://network_read:{s['reader']}@network-db.{NS}.svc.cluster.local.:5432/network?sslmode=disable&connect_timeout=1",cursor_signing_key=base64.b64encode(s['cursor'].encode()).decode())
config['server']['grpc'].update(addr='0.0.0.0:19090',timeout='5s');config['server']['admin'].update(addr='0.0.0.0:19091',timeout='2s')
images=json.loads((R/'inputs/images.json').read_text()); objects=[]
def add(kind,name,**kw):
    obj=dict(apiVersion='v1',kind=kind,metadata=dict(name=name,namespace=NS),**kw);objects.append(obj);return obj
add('Secret','network-runtime',type='Opaque',stringData={'config.yaml':json.dumps(config),'owner-password':s['owner'],'migration-dsn':f"postgres://network:{s['owner']}@network-db.{NS}.svc.cluster.local.:5432/network?sslmode=disable",'ca.pem':(OLD/'private/ca.pem').read_text(),'tls.crt':(p/'network.pem').read_text(),'tls.key':(p/'network.key').read_text()})
add('PersistentVolumeClaim','network-db',spec={'accessModes':['ReadWriteOnce'],'storageClassName':'rook-ceph-block','resources':{'requests':{'storage':'2Gi'}}})
def es(name,key):return {'name':name,'valueFrom':{'secretKeyRef':{'name':'network-runtime','key':key}}}
def deploy(name,image,ports,**container):
    c=dict(name=name,image=image,imagePullPolicy='Never',ports=[{'containerPort':port} for port in ports],resources={'requests':{'cpu':'100m','memory':'256Mi'},'limits':{'cpu':'2','memory':'1Gi'}},**container)
    volumes=[{'name':'runtime','secret':{'secretName':'network-runtime'}}] if name=='network' else [{'name':'data','persistentVolumeClaim':{'claimName':'network-db'}}]
    objects.append({'apiVersion':'apps/v1','kind':'Deployment','metadata':{'name':name,'namespace':NS},'spec':{'replicas':1,'strategy':{'type':'Recreate'},'selector':{'matchLabels':{'app':name}},'template':{'metadata':{'labels':{'app':name}},'spec':{'automountServiceAccountToken':False,'nodeSelector':{'kubernetes.io/hostname':'ani-01'},'volumes':volumes,'containers':[c]}}}})
    add('Service',name,spec={'selector':{'app':name},'ports':[{'name':'p'+str(port),'port':port,'targetPort':port} for port in ports]})
deploy('network-db',images['postgres'],[5432],env=[{'name':'POSTGRES_USER','value':'network'},{'name':'POSTGRES_DB','value':'network'},es('POSTGRES_PASSWORD','owner-password'),{'name':'PGDATA','value':'/var/lib/postgresql/task/data'}],volumeMounts=[{'name':'data','mountPath':'/var/lib/postgresql/task'}],readinessProbe={'exec':{'command':['pg_isready','-U','network']},'periodSeconds':3})
deploy('network',images['network'],[19090,19091],args=['-conf','/config/config.yaml'],env=[{'name':k,'value':v} for k,v in {'ANI_NETWORK_MODE':'vpc-read','ANI_NETWORK_CLIENT_CA':'/config/ca.pem','ANI_NETWORK_TLS_CERT':'/config/tls.crt','ANI_NETWORK_TLS_KEY':'/config/tls.key'}.items()],volumeMounts=[{'name':'runtime','mountPath':'/config','readOnly':True}],readinessProbe={'httpGet':{'path':'/readyz','port':19091},'periodSeconds':3,'timeoutSeconds':2,'failureThreshold':1})
job={'apiVersion':'batch/v1','kind':'Job','metadata':{'name':'network-migrate-v1','namespace':NS},'spec':{'backoffLimit':0,'template':{'spec':{'restartPolicy':'Never','automountServiceAccountToken':False,'nodeSelector':{'kubernetes.io/hostname':'ani-01'},'containers':[{'name':'migration','image':images['network'],'imagePullPolicy':'Never','args':['-migrate'],'env':[es('ANI_NETWORK_MIGRATION_DSN','migration-dsn'),{'name':'ANI_NETWORK_RUNTIME_ROLE','value':'network_read'}]}]}}}}
for filename,values in [('database.json',objects[:3]+[o for o in objects[3:] if o['metadata']['name']=='network-db']),('network.json',[o for o in objects if o['metadata']['name']=='network']),('migration.json',[job])]:
    (p/filename).write_text(json.dumps({'apiVersion':'v1','kind':'List','items':values}));os.chmod(p/filename,0o600)
# role creation and privilege reduction are task-private SQL, never print passwords.
(p/'role.sql').write_text("CREATE ROLE network_read LOGIN PASSWORD '"+s['reader']+"';\n");os.chmod(p/'role.sql',0o600)
for o in objects:
    if o['kind']=='Secret':o['stringData']={k:'<task-private>' for k in o['stringData']}
(R/'evidence/network-runtime-manifest.json').write_text(json.dumps({'items':objects,'migration':job},indent=2)+'\n')
