#!/usr/bin/env python3
"""Run only on Fedora; render this task's isolated runtime and private secrets."""
import base64
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import yaml

r = Path(sys.argv[1])
ns = "gov-model-20260919-01"
private = r / "private"
private.mkdir(exist_ok=True)
os.chmod(private, 0o700)
if (private / "secrets.json").exists():
    passwords = json.loads((private / "secrets.json").read_text())
else:
    passwords = {k: secrets.token_hex(24) for k in ["governance", "model", "redis", "jwt"]}
    (private / "secrets.json").write_text(json.dumps(passwords))
    os.chmod(private / "secrets.json", 0o600)

def openssl(*args):
    subprocess.run(["openssl", *args], cwd=private, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

if not (private / "ca.pem").exists():
    openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "7", "-subj", "/CN=gov-model-20260919-01", "-keyout", "ca.key", "-out", "ca.pem")
    for name, usage in [("ani-model-service", "serverAuth"), ("ani-governance", "clientAuth"), ("wrong-service", "clientAuth")]:
        openssl("req", "-new", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN="+name, "-keyout", name+".key", "-out", name+".csr")
        (private / (name+".ext")).write_text("subjectAltName=DNS:"+name+"\nextendedKeyUsage="+usage+"\nkeyUsage=digitalSignature,keyEncipherment\n")
        openssl("x509", "-req", "-in", name+".csr", "-CA", "ca.pem", "-CAkey", "ca.key", "-CAcreateserial", "-days", "7", "-extfile", name+".ext", "-out", name+".pem")
    for p in private.glob("*.key"):
        os.chmod(p, 0o600)

gov = r / "work/governance"
if not (gov / "go.mod").is_file():
    gov = gov / "backend"
if not (gov / "go.mod").is_file():
    raise FileNotFoundError(f"Governance go.mod not found in {r / 'work/governance'} or its backend directory")
authz = yaml.safe_load((gov/"app/admin/service/configs/auth.yaml").read_text())["authz"]
authz["type"] = "casbin"
config = {"server":{"rest":{"addr":":7788","timeout":"10s","enable_swagger":False,"enable_pprof":False}},
 "data":{"database":{"driver":"postgres","source":f"host=governance-db port=5432 user=governance password={passwords['governance']} dbname=governance sslmode=disable","migrate":True,"max_open_connections":8,"max_idle_connections":4},"redis":{"addr":"redis:6379","password":passwords["redis"],"dial_timeout":"3s","read_timeout":"1s","write_timeout":"1s"}},
 "authn":{"type":"jwt","jwt":{"method":"HS256","key":passwords["jwt"],"access_token_expires":"1800s","refresh_token_expires":"7200s"}},
 "authz":authz, "logger":{"type":"std"},
 # Upstream constructs this unused file adapter without contacting it. No OSS
 # service is enabled or reported healthy in the model-list slice.
 "oss":{"minio":{"endpoint":"127.0.0.1:1","use_ssl":False}}}
modelconfig={"server":{"grpc":{"network":"tcp","addr":"0.0.0.0:19090","timeout":"5s"},"admin":{"network":"tcp","addr":"0.0.0.0:19091","timeout":"2s"},"shutdown_timeout":"5s"}}
objects=[]
def add(kind,name,**fields):
    x={"apiVersion":"v1","kind":kind,"metadata":{"name":name,"namespace":ns},**fields};objects.append(x);return x
objects.append({"apiVersion":"v1","kind":"Namespace","metadata":{"name":ns,"labels":{"app.kubernetes.io/part-of":"governance-model-bootstrap","ani-task":"01a0b596"}}})
def secret(name,values):add("Secret",name,type="Opaque",stringData=values)
secret("runtime",{"config.yaml":json.dumps(config),"model.yaml":json.dumps(modelconfig),"governance-password":passwords["governance"],"model-password":passwords["model"],"redis-password":passwords["redis"],"model-dsn":f"postgres://model:{passwords['model']}@model-db:5432/model?sslmode=disable"})
secret("governance-tls",{x:(private/y).read_text() for x,y in {"ca.pem":"ca.pem","tls.crt":"ani-governance.pem","tls.key":"ani-governance.key"}.items()})
secret("model-tls",{x:(private/y).read_text() for x,y in {"ca.pem":"ca.pem","tls.crt":"ani-model-service.pem","tls.key":"ani-model-service.key"}.items()})

def envsecret(name,key): return {"name":name,"valueFrom":{"secretKeyRef":{"name":"runtime","key":key}}}
def deployment(name,image,ports,env=None,args=None,mounts=None,volumes=None,probe=None,cpu="100m",memory="128Mi",limit="512Mi"):
    c={"name":name,"image":image,"imagePullPolicy":"Never","ports":[{"containerPort":p} for p in ports],"resources":{"requests":{"cpu":cpu,"memory":memory},"limits":{"cpu":"2","memory":limit}}}
    if env:c["env"]=env
    if args:c["args"]=args
    if mounts:c["volumeMounts"]=mounts
    if probe:c["readinessProbe"]={**probe,"periodSeconds":3,"timeoutSeconds":2,"failureThreshold":1}
    objects.append({"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":name,"namespace":ns},"spec":{"replicas":1,"strategy":{"type":"Recreate"},"selector":{"matchLabels":{"app":name}},"template":{"metadata":{"labels":{"app":name}},"spec":{"nodeSelector":{"kubernetes.io/hostname":"ani-01"},"containers":[c],"volumes":volumes or []}}}})
    add("Service",name,spec={"selector":{"app":name},"ports":[{"name":"p"+str(p),"port":p,"targetPort":p} for p in ports]})

images=json.loads((r/"inputs/images.json").read_text())
for db in ["governance","model"]:
    name=db+"-db"
    add("PersistentVolumeClaim",name,spec={"accessModes":["ReadWriteOnce"],"storageClassName":"rook-ceph-block","resources":{"requests":{"storage":"2Gi"}}})
    deployment(name,images["postgres"],[5432],env=[{"name":"POSTGRES_USER","value":db},{"name":"POSTGRES_DB","value":db},envsecret("POSTGRES_PASSWORD",db+"-password"),{"name":"PGDATA","value":"/var/lib/postgresql/task/data"}],mounts=[{"name":"data","mountPath":"/var/lib/postgresql/task"}],volumes=[{"name":"data","persistentVolumeClaim":{"claimName":name}}],probe={"exec":{"command":["pg_isready","-U",db,"-d",db]}},memory="256Mi",limit="768Mi")
deployment("redis",images["redis"],[6379],env=[envsecret("REDIS_PASSWORD","redis-password")],args=["redis-server","--requirepass","$(REDIS_PASSWORD)","--save",""],probe={"tcpSocket":{"port":6379}},memory="64Mi",limit="256Mi")
for name,ports in [("model",[19090,19091]),("governance",[7788])]:
    volumes=[{"name":"runtime","secret":{"secretName":"runtime"}},{"name":"tls","secret":{"secretName":name+"-tls"}}]
    mounts=[{"name":"runtime","mountPath":"/config","readOnly":True},{"name":"tls","mountPath":"/tls","readOnly":True}]
    if name=="model":
        env=[{"name":"ANI_MODEL_MODE","value":"catalog-read"},envsecret("ANI_DATABASE_DSN","model-dsn")]+[{"name":k,"value":v} for k,v in {"ANI_MODEL_CLIENT_CA":"/tls/ca.pem","ANI_MODEL_TLS_CERT":"/tls/tls.crt","ANI_MODEL_TLS_KEY":"/tls/tls.key"}.items()]
        args=["-conf","/config/model.yaml"];probe={"httpGet":{"path":"/readyz","port":19091}}
    else:
        env=[{"name":k,"value":v} for k,v in {"ANI_MODEL_ADDR":"model."+ns+".svc.cluster.local.:19090","ANI_MODEL_CA":"/tls/ca.pem","ANI_MODEL_CERT":"/tls/tls.crt","ANI_MODEL_KEY":"/tls/tls.key","ANI_MODEL_TIMEOUT":"2s"}.items()]
        args=["-c","/config/config.yaml"];probe={"tcpSocket":{"port":7788}}
    deployment(name,images[name],ports,env,args,mounts,volumes,probe,memory="256Mi",limit="1Gi")

(private/"runtime.json").write_text(json.dumps({"apiVersion":"v1","kind":"List","items":objects},indent=2))
os.chmod(private/"runtime.json",0o600)
# Public manifest keeps structure and image pins, with secret payloads removed.
for obj in objects:
    if obj["kind"]=="Secret":obj["stringData"]={k:"<task-private>" for k in obj["stringData"]}
(r/"evidence/runtime-manifest.json").write_text(json.dumps({"apiVersion":"v1","kind":"List","items":objects},indent=2)+"\n")
