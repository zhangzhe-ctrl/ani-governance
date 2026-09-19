#!/usr/bin/env python3
"""Snapshot selected tx7do Go modules and verify file-GOPROXY recovery.

Run on the build host with task-owned GOMODCACHE/GOCACHE and GOWORK=off.
This backs up source packages, not the entire offline build toolchain.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import zipfile


def json_stream(raw):
    decoder = json.JSONDecoder()
    while raw.strip():
        obj, end = decoder.raw_decode(raw.lstrip())
        yield obj
        raw = raw.lstrip()[end:]


def run(*args, cwd, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True)


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-commit', required=True)
    parser.add_argument('--gow-version', default='v1.0.3')
    parser.add_argument('--output', default='third_party/tx7do')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    out = (root / args.output).resolve()
    if out.exists():
        parser.error(f'output already exists: {out}; use a new directory')
    if os.environ.get('GOWORK') != 'off' or not os.environ.get('GOMODCACHE') or not os.environ.get('GOCACHE'):
        parser.error('set GOWORK=off and task-owned GOMODCACHE/GOCACHE first')

    modules = [m for m in json_stream(run('go', 'list', '-m', '-json', 'all', cwd=root))
               if m['Path'].startswith('github.com/tx7do/')]
    if any('Replace' in m for m in modules):
        raise RuntimeError('module replacements require an explicit backup policy')
    for m in modules:
        m['Scope'] = 'application'
    application_count = len(modules)
    gow_path = 'github.com/tx7do/go-wind-toolkit/gowind'
    gow = json.loads(run('go', 'mod', 'download', '-json',
                         f'{gow_path}@{args.gow_version}', cwd=root))
    # Resolve the tool as its own module, without changing the application's graph.
    with tempfile.TemporaryDirectory(prefix='gow-graph-', dir=Path(os.environ['GOMODCACHE']).parent) as temp:
        for name in ('go.mod', 'go.sum'):
            shutil.copyfile(Path(gow['Dir']) / name, Path(temp) / name)
        tool_modules = list(json_stream(run('go', 'list', '-mod=readonly', '-m', '-json', 'all', cwd=temp)))
    selected = {f"{m['Path']}@{m['Version']}": m for m in modules}
    for m in tool_modules:
        if not m['Path'].startswith('github.com/tx7do/'):
            continue
        if 'Replace' in m:
            raise RuntimeError('tool module replacements require an explicit backup policy')
        if m.get('Main'):
            m['Version'] = args.gow_version
        key = f"{m['Path']}@{m['Version']}"
        if key in selected:
            selected[key]['UsedByTool'] = True
        else:
            m['Scope'] = 'development-tool'
            m['UsedByTool'] = True
            modules.append(m)
            selected[key] = m
    keys = [f"{m['Path']}@{m['Version']}" for m in modules]
    if len(keys) != len(set(keys)):
        raise RuntimeError('duplicate module versions')
    sums = {}
    for line in (root / 'go.sum').read_text().splitlines():
        path, version, checksum = line.split()
        sums[(path, version)] = checksum
    downloaded = {f"{d['Path']}@{d['Version']}": d for d in json_stream(
        run('go', 'mod', 'download', '-json', *keys, cwd=root))}

    out.mkdir(parents=True)
    entries = []
    for m, key in zip(modules, keys):
        d = downloaded[key]
        if d.get('Error'):
            raise RuntimeError(d['Error'])
        path, version = m['Path'], m['Version']
        if m['Scope'] == 'application':
            for v, field in [(version, 'Sum'), (version + '/go.mod', 'GoModSum')]:
                if sums.get((path, v)) != d[field]:
                    raise RuntimeError(f'go.sum mismatch or missing entry: {path}@{v}')
        target = out / 'proxy' / path / '@v'
        target.mkdir(parents=True, exist_ok=True)
        artifacts = {}
        for field, suffix in [('Info', 'info'), ('GoMod', 'mod'), ('Zip', 'zip')]:
            dest = target / f'{version}.{suffix}'
            shutil.copyfile(d[field], dest)
            artifacts[suffix] = {'path': str(dest.relative_to(out)), 'sha256': sha256(dest), 'bytes': dest.stat().st_size}
        version_list = target / 'list'
        versions = set(version_list.read_text().splitlines()) if version_list.exists() else set()
        version_list.write_text('\n'.join(sorted(versions | {version})) + '\n')
        if json.loads((target / f'{version}.info').read_text())['Version'] != version:
            raise RuntimeError(f'invalid info: {key}')
        with zipfile.ZipFile(target / f'{version}.zip') as z:
            if z.testzip() is not None:
                raise RuntimeError(f'invalid source zip: {key}')
            names = z.namelist()
            if not names or any(not n.startswith(key + '/') for n in names):
                raise RuntimeError(f'invalid zip prefix: {key}')
            licenses = [{'path': n, 'sha256': hashlib.sha256(z.read(n)).hexdigest()}
                        for n in names if Path(n).name.upper().startswith(('LICENSE', 'NOTICE', 'COPYING', 'COPYRIGHT'))]
        entries.append({'module_path': path, 'version': version, 'scope': m['Scope'],
                        'used_by_gow': m.get('UsedByTool', False),
                        'indirect': m.get('Indirect', False) if m['Scope'] == 'application' else None,
                        'upstream_repository': 'https://' + '/'.join(path.split('/')[:3]),
                        'origin': d.get('Origin'), 'sum': d['Sum'], 'go_mod_sum': d['GoModSum'],
                        'artifacts': artifacts, 'license_files': licenses})

    # No network fallback: replay in an empty cache outside the application module.
    with tempfile.TemporaryDirectory(prefix='tx7do-replay-', dir=Path(os.environ['GOMODCACHE']).parent) as temp:
        env = dict(os.environ, GOMODCACHE=str(Path(temp) / 'mod'),
                   GOPROXY=(out / 'proxy').as_uri(), GOPRIVATE='', GONOPROXY='',
                   GOSUMDB='off', GOWORK='off', GOTOOLCHAIN='local')
        replay = {f"{d['Path']}@{d['Version']}": d for d in json_stream(
            run('go', 'mod', 'download', '-json', *keys, cwd=temp, env=env))}
        for e, key in zip(entries, keys):
            if replay[key].get('Error') or replay[key]['Sum'] != e['sum'] or replay[key]['GoModSum'] != e['go_mod_sum']:
                raise RuntimeError(f'file proxy replay mismatch: {key}')

    locks = out / 'locks'
    locks.mkdir()
    for src in ['go.mod', 'go.sum', 'api/buf.lock']:
        shutil.copyfile(root / src, locks / Path(src).name)
    for name in ('go.mod', 'go.sum'):
        shutil.copyfile(Path(gow['Dir']) / name, locks / ('gow.' + name))
    manifest = {'schema_version': 1, 'created_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                'source_commit_before_layout_change': args.source_commit,
                'go_mod_sha256': sha256(root / 'go.mod'), 'go_sum_sha256': sha256(root / 'go.sum'),
                'toolchain': run('go', 'version', cwd=root).strip(),
                'scope': 'Selected application and pinned gow tool tx7do module graphs, deduplicated by module@version; other dependencies and toolchains are excluded.',
                'application_module_count': application_count,
                'additional_tool_module_versions': len(modules) - application_count,
                'total_module_versions': len(modules),
                'verification': {'go_sum_match_application_modules': 'pass', 'zip_integrity': 'pass',
                                 'empty_cache_file_proxy_replay_all_modules': 'pass',
                                 'full_offline_build': 'not_verified'},
                'modules': entries}
    (out / 'manifest.json').write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + '\n')
    checksums = [f'{sha256(p)}  {p.relative_to(out)}' for p in sorted(out.rglob('*')) if p.is_file()]
    (out / 'SHA256SUMS').write_text('\n'.join(checksums) + '\n')
    print(json.dumps({'backup': str(out), 'application_modules': application_count,
                      'additional_tool_module_versions': len(modules) - application_count,
                      'total_module_versions': len(modules), 'file_proxy_replay': 'pass'}))


if __name__ == '__main__':
    main()
