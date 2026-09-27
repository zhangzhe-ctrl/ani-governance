#!/usr/bin/env python3
"""Exercise the real Makefiles with fake tools in disposable directories.
No download, database, server, Docker, or host-configuration command is run.
A passing result proves delegation ONLY, not actual code generation/build.
"""
from __future__ import annotations
import argparse, json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

FAKE = r'''import json,os,pathlib,sys
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
def event(kind):
    row={'kind':kind,'args':args,'cwd':os.getcwd()}
    fd=os.open(os.environ['MOCK_LOG'],os.O_APPEND|os.O_CREAT|os.O_WRONLY,0o600)
    os.write(fd,(json.dumps(row)+'\n').encode());os.close(fd)
    if os.environ.get('MOCK_FAIL')==kind:sys.exit(77)
if name=='go':
    if args==['env','GOPATH']:print(os.environ['GOPATH']);sys.exit(0)
    if args and args[0]=='version':print('go version go1.26.7 mock/linux');sys.exit(0)
    if args and args[0]=='build':
        out=args[args.index('-o')+1] if '-o' in args else ''
        if out=='tools/bin/gow':
            event('BUILD_GOW');p=pathlib.Path(out);p.parent.mkdir(parents=True,exist_ok=True)
            p.write_text(pathlib.Path(__file__).read_text());p.chmod(0o755);sys.exit(0)
        event('COMPILE');sys.exit(0)
    if args and args[0]=='run':
        if './cmd/schema' in args:event('SQL_EXPORT');print('-- mock schema');sys.exit(0)
        event('RUN');sys.exit(0)
    event('UNEXPECTED_GO');sys.exit(81)
if name=='gow':
    if args==['api']:event('API');sys.exit(0)
    event('UNEXPECTED_GOW');sys.exit(82)
if name=='redact-builder':event('BUILD_REDACT');sys.exit(0)
if name=='ent':event('ENT');sys.exit(0)
if name=='buf':
    event('OPENAPI_RAW' if args==['generate','--template','buf.admin.openapi.gen.yaml'] else 'BUF_RAW');sys.exit(0)
if name=='python3':
    event('FINALIZE' if args==['scripts/finalize-aksk-openapi.py'] else 'UNEXPECTED_PYTHON');sys.exit(0)
event('FORBIDDEN_'+name);sys.exit(88)
'''

def fixture(repo:Path, root:Path)->dict:
    root.mkdir()
    for rel in ['Makefile','app.mk']:
        dest=root/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(repo/rel,dest)
    service=root/'app/admin/service';service.mkdir(parents=True)
    (service/'Makefile').write_bytes((repo/'app/admin/service/Makefile').read_bytes())
    (service/'internal/data/ent/schema').mkdir(parents=True)
    (root/'api').mkdir();(root/'.env').write_text('PROJECT_NAME=ani\nSERVICE_APP_VERSION=1.0.0\n')
    (root/'scripts/build-redact-plugin.sh').write_text('#!/bin/sh\nexec redact-builder\n')
    bin=root/'fake-bin';bin.mkdir()
    for name in ['go','gow','ent','buf','python3','redact-builder','docker','sudo','curl','git','apt-get','dnf','brew']:
        p=bin/name;p.write_text('#!'+sys.executable+' -S\n'+FAKE);p.chmod(0o755)
    env={k:v for k,v in os.environ.items() if k not in ['MAKEFLAGS','MFLAGS','MAKELEVEL','GOFLAGS','GOBIN','GOPATH','GOROOT','GOTOOLCHAIN','MAKEFILES','SHELL','ENV','BASH_ENV']}
    env.update(PATH=str(bin)+os.pathsep+os.environ['PATH'],GOPATH=str(root/'gopath'),GOWORK='off',GOPROXY='off',HOME=str(root/'home'),MOCK_LOG=str(root/'calls.jsonl'),PWD=str(root),GOFLAGS='-mod=readonly')
    (root/'home').mkdir();return env

def read_events(root:Path)->list:
    p=root/'calls.jsonl';return [json.loads(s) for s in p.read_text().splitlines()] if p.exists() else []

def counts(ev:list)->dict:
    out={}
    for e in ev:out[e['kind']]=out.get(e['kind'],0)+1
    return out

def check(repo:Path)->dict:
    make=shutil.which('make')
    if not make:raise RuntimeError('GNU make is required for this mock check')
    cases=[
      ('root-api',[], 'api',{'API':1,'COMPILE':0,'ENT':0,'FINALIZE':0}),
      ('root-build',[], 'build',{'API':1,'COMPILE':1,'ENT':0,'FINALIZE':0}),
      ('root-build-only',[], 'build_only',{'API':0,'COMPILE':1,'ENT':0}),
      ('root-gen',[], 'gen',{'API':1,'ENT':1,'SQL_EXPORT':1,'COMPILE':0,'FINALIZE':0}),
      ('root-all',[], 'all',{'API':1,'ENT':1,'SQL_EXPORT':1,'COMPILE':1,'FINALIZE':0}),
      ('root-openapi',[], 'openapi',{'API':0,'OPENAPI_RAW':1,'FINALIZE':1,'COMPILE':0}),
      ('service-api',['-C','app/admin/service'], 'api',{'API':1,'COMPILE':0,'FINALIZE':0}),
      ('service-build',['-C','app/admin/service'], 'build',{'API':1,'COMPILE':1,'ENT':0,'FINALIZE':0}),
      ('service-build-only',['-C','app/admin/service'], 'build_only',{'API':0,'COMPILE':1,'ENT':0}),
      ('service-run',['-C','app/admin/service'], 'run',{'API':1,'RUN':1,'ENT':0,'FINALIZE':0}),
      ('service-gen',['-C','app/admin/service'], 'gen',{'API':1,'ENT':1,'SQL_EXPORT':0,'COMPILE':0,'FINALIZE':0}),
      ('service-app',['-C','app/admin/service'], 'app',{'API':1,'ENT':1,'COMPILE':1,'FINALIZE':0}),
      ('service-openapi',['-C','app/admin/service'], 'openapi',{'API':0,'OPENAPI_RAW':1,'FINALIZE':1}),
      ('root-parallel-build',['-j4'], 'build',{'API':1,'COMPILE':1,'FINALIZE':0}),
      ('root-parallel-all',['-j4'], 'all',{'API':1,'ENT':1,'SQL_EXPORT':1,'COMPILE':1,'FINALIZE':0}),
      ('service-parallel-app',['-j4','-C','app/admin/service'], 'app',{'API':1,'ENT':1,'COMPILE':1,'FINALIZE':0}),
    ]
    results=[]
    with tempfile.TemporaryDirectory(prefix='ani-entrypoints-') as td:
      work=Path(td)
      for label,opts,target,want in cases:
        print('CHECK', label, flush=True);root=work/label;env=fixture(repo,root)
        p=subprocess.run([make,'--no-print-directory',*opts,target],cwd=root,env=env,capture_output=True,text=True,timeout=20)
        ev=read_events(root);got=counts(ev)
        if p.returncode:raise RuntimeError(f'{label}: exit {p.returncode}\n{p.stdout}\n{p.stderr}')
        for key,n in want.items():
            if got.get(key,0)!=n:raise RuntimeError(f'{label}: {key} expected {n}, got {got}; output:\n{p.stdout}')
        if got.get('BUF_RAW') or any(k.startswith(('FORBIDDEN','UNEXPECTED')) for k in got):raise RuntimeError(f'{label}: unsafe/bypass command: {got}')
        order=[e['kind'] for e in ev]
        if 'ENT' in order and 'API' in order and order.index('ENT')>order.index('API'):raise RuntimeError(label+': API preceded Ent')
        if 'API' in order and 'COMPILE' in order and order.index('API')>order.index('COMPILE'):raise RuntimeError(label+': compilation preceded API')
        for e in ev:
            if e['kind']=='COMPILE':
                args=e['args']
                if args!=['build','-mod=readonly','-ldflags','-s -w -X main.version=1.0.0','-o','./bin/','./...']:
                    raise RuntimeError(f'{label}: compilation flags changed: {args}')
            if e['kind']=='RUN' and not e['args'][-3:]==['./cmd/server','-c','./configs']:
                raise RuntimeError(label+': runtime arguments changed')
        results.append({'case':label,'pass':True,'events':got})
      # Failures must stop downstream work, not be hidden by a trailing command/foreach.
      for failure,target,forbidden in [('API','build',['COMPILE']),('ENT','gen',['API','SQL_EXPORT']),('COMPILE','build_only',[])]:
        label='reject-'+failure;print('CHECK', label, flush=True);root=work/label;env=fixture(repo,root);env['MOCK_FAIL']=failure
        if failure=='COMPILE':
            dest=root/'app/zzz/service';dest.mkdir(parents=True);(dest/'Makefile').write_text('include ../../../app.mk\n')
        p=subprocess.run([make,'--no-print-directory',target],cwd=root,env=env,capture_output=True,text=True,timeout=20)
        got=counts(read_events(root))
        if p.returncode==0 or not got.get(failure):raise RuntimeError(label+': injected failure was swallowed or did not execute')
        if any(got.get(k) for k in forbidden):raise RuntimeError(label+': continued after failed prerequisite')
        if failure=='COMPILE' and got.get('COMPILE')!=1:raise RuntimeError(label+': later service compiled after failure')
        results.append({'case':label,'pass':True,'actual_exit':p.returncode,'events':got})
      # The old installers have exited HEAD; the Make target remains fail-fast.
      root=work/'retired-root';env=fixture(repo,root)
      p=subprocess.run([make,'--no-print-directory','install-dev'],cwd=root,env=env,capture_output=True,text=True,timeout=10)
      if p.returncode!=2 or 'Retired:' not in p.stderr or read_events(root):
        raise RuntimeError('retired-root: side effect/exit check failed')
      if (repo/'scripts/env/install_unix_dev.sh').exists() or (repo/'scripts/env/install_windows_dev.ps1').exists():
        raise RuntimeError('retired installers unexpectedly restored')
      results.append({'case':'retired-root','pass':True,'actual_exit':p.returncode,'external_commands':0})
    return {'scope':'mock Make delegation and retired entrypoints ONLY','pass':True,'cases':results,'real_generation':'NOT_RUN','database':'NOT_RUN','deployment':'NOT_RUN'}

def main()->int:
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--repo',type=Path,default=Path(__file__).resolve().parents[2]);p.add_argument('--json',type=Path)
    a=p.parse_args();repo=a.repo.resolve()
    result=check(repo)
    if a.json:a.json.parent.mkdir(parents=True,exist_ok=True);a.json.write_text(json.dumps(result,indent=2)+'\n')
    print(json.dumps(result,indent=2));return 0
if __name__=='__main__':
    try:sys.exit(main())
    except (RuntimeError,OSError,subprocess.TimeoutExpired) as e:print('FAIL:',e,file=sys.stderr);sys.exit(1)
