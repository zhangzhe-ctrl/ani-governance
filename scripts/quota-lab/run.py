#!/usr/bin/env python3
"""QUOTA-GPU-LOCAL-01 本地任务编排器（计划 §15）。

子命令：preflight / prepare / migrate / build / start / seed / accept / collect / stop / cleanup。
公共参数 --run-dir 必填，路径名必须含 quota-gpu-local-01。

安全边界：
  * 只绑定 127.0.0.1；Docker 仅本地 unix socket；
  * 机密文件 0600；日志/证据脱敏（不记录 JWT/密码/DSN 全文）；
  * cleanup 只清理本任务创建的容器（名称前缀 quota-gpu-local-01-）。
"""

import argparse
import json
import os
import pathlib
import secrets
import shutil
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

TOOLS_DIR = pathlib.Path.home() / ".local" / "quota-lab-tools-01"
ATLAS = TOOLS_DIR / "atlas"
BUF = TOOLS_DIR / "buf"
POSTGRES_DIGEST = "postgres@sha256:a3b7f434b2dc57ce85a67e171163eb8ab1a1ebcb39d27484661f26b1dfbe30d6"
REDIS_DIGEST = "redis@sha256:c6eabf748fc7a61dbb5a705c78bcf3d6377b1127a97d0ce965c11c44ba46896f"
PG_CONTAINER = "quota-gpu-local-01-pg"
REDIS_CONTAINER = "quota-gpu-local-01-redis"
EVIDENCE = pathlib.Path("docs/evidence/quota-gpu-local-01")


def sh(cmd, env=None, cwd=None, capture=True, timeout=600):
    e = dict(os.environ)
    e.setdefault("GOWORK", "off")
    e.setdefault("GOMAXPROCS", "2")
    e.setdefault("GOFLAGS", "-p=2")
    if env:
        e.update(env)
    return subprocess.run(cmd, env=e, cwd=cwd, text=True,
                          capture_output=capture, timeout=timeout)


def die(msg):
    print(f"[quota-lab] FAIL: {msg}", file=sys.stderr)
    sys.exit(1)


def log(msg):
    print(f"[quota-lab] {msg}")


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def wait_http(url, expect_any=True, timeout=60, headers=None):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        try:
            req = urllib.request.Request(url, headers=headers or {})
            try:
                with urllib.request.urlopen(req, timeout=3) as r:
                    return r.status, r.read()
            except urllib.error.HTTPError as e:
                if expect_any:
                    return e.code, e.read()
                last = e.code
        except Exception as e:  # noqa: BLE001
            last = str(e)
        time.sleep(0.5)
    raise RuntimeError(f"wait_http timeout {url}: {last}")


def http(method, url, body=None, token=None, headers=None, timeout=10):
    data = json.dumps(body).encode() if body is not None else None
    h = {"Content-Type": "application/json"}
    if token:
        h["Authorization"] = f"Bearer {token}"
    h.update(headers or {})
    req = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except Exception:  # noqa: BLE001
            return e.code, {}


# ── 环境描述 ─────────────────────────────────────────────────

def load_env(run_dir):
    f = run_dir / "env.json"
    if not f.exists():
        die(f"{f} missing; run prepare first")
    return json.loads(f.read_text())


def save_env(run_dir, env):
    (run_dir / "env.json").write_text(json.dumps(env, indent=2))


def pg_port(run_dir):
    env = load_env(run_dir)
    return env["pg_port"]


def pg_dsn(run_dir, db):
    return f"postgres://postgres@127.0.0.1:{pg_port(run_dir)}/{db}?sslmode=disable"


# ── 子命令 ───────────────────────────────────────────────────

def cmd_preflight(args):
    run_dir = pathlib.Path(args.run_dir)
    if "quota-gpu-local-01" not in str(run_dir):
        die("run dir name must contain quota-gpu-local-01")
    if run_dir.exists() and (run_dir / "env.json").exists() and not args.force:
        die("run dir already prepared; use a fresh directory")
    run_dir.mkdir(parents=True, exist_ok=True)

    # Docker 必须本地 unix socket。
    if os.environ.get("DOCKER_HOST"):
        die("DOCKER_HOST is set; refusing remote docker")
    ctx = sh(["docker", "context", "show"]).stdout.strip()
    if ctx != "default":
        die(f"docker context is {ctx!r}, expected default (local)")
    go = sh(["go", "version"]).stdout.strip()
    if "1.26.7" not in go:
        die(f"go version mismatch: {go}")
    if not BUF.exists():
        die(f"buf not found at {BUF}")
    if not ATLAS.exists():
        die(f"atlas not found at {ATLAS}")
    versions = {
        "go": go, "buf": sh([str(BUF), "--version"]).stdout.strip(),
        "atlas": "v1.3.0 (built from tag source)", "postgres_image": POSTGRES_DIGEST,
        "redis_image": REDIS_DIGEST,
    }
    (run_dir / "preflight.json").write_text(json.dumps(versions, indent=2))
    log(f"preflight ok: {versions}")
    return 0


def ensure_container(name, image, ports):
    out = sh(["docker", "inspect", "-f", "{{.State.Running}}", name])
    if out.returncode == 0 and out.stdout.strip() == "true":
        # 校验所需端口映射是否存在；缺失则重建（仅限本任务容器）。
        for _h, private in ports:
            if container_port_safe(name, private) is None:
                sh(["docker", "rm", "-f", name])
                break
        else:
            return False
    if out.returncode == 0:  # 存在但停止
        sh(["docker", "start", name])
        return True
    args = ["docker", "run", "-d", "--name", name,
            "--label", "quota-gpu-local-01=true"]
    for h, c in ports:
        args += ["-p", f"127.0.0.1:{h}:{c}"]
    args.append(image)
    r = sh(args)
    if r.returncode != 0:
        die(f"start container {name}: {r.stderr}")
    return True


def container_port(name, private):
    p = container_port_safe(name, private)
    if p is None:
        die(f"container {name} has no published port for {private}")
    return p


def container_port_safe(name, private):
    out = sh(["docker", "inspect", "-f",
              f'{{{{(index (index .NetworkSettings.Ports "{private}/tcp") 0).HostPort}}}}', name])
    v = out.stdout.strip()
    if out.returncode != 0 or not v:
        return None
    return int(v)


def cmd_prepare(args):
    run_dir = pathlib.Path(args.run_dir)
    if "quota-gpu-local-01" not in str(run_dir):
        die("run dir name must contain quota-gpu-local-01")
    if (run_dir / "env.json").exists():
        die("run dir already prepared (must not reuse unknown directory)")
    run_dir.mkdir(parents=True, exist_ok=True)
    for sub in ("certs", "configs", "logs", "bin", "secrets"):
        (run_dir / sub).mkdir(exist_ok=True)

    ensure_container(PG_CONTAINER, POSTGRES_DIGEST, [(0, "5432")])
    ensure_container(REDIS_CONTAINER, REDIS_DIGEST, [(0, "6379")])
    time.sleep(3)
    pgp = container_port(PG_CONTAINER, "5432")
    redis_port = container_port(REDIS_CONTAINER, "6379")
    r = sh(["docker", "exec", PG_CONTAINER, "pg_isready", "-U", "postgres"])
    if r.returncode != 0:
        die("postgres not ready")

    # 四类隔离数据库 + 迁移/坏数据验证库（已存在则跳过）。
    for db in ("governance", "gpu_owner", "gpu_provider", "atlas_dev",
               "gov_upgrade", "gov_bad", "gov_pgtest"):
        sh(["docker", "exec", PG_CONTAINER, "psql", "-U", "postgres",
            "-c", f"CREATE DATABASE {db}"])

    env = {
        "pg_port": pgp,
        "redis_port": redis_port,
        "rest_port": free_port(),
        "sse_port": free_port(),
        "quota_internal_port": free_port(),
        "sim_grpc_port": free_port(),
        "sim_control_port": free_port(),
        "gov_control_port": free_port(),
        "started_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
    }
    save_env(run_dir, env)

    # ── mTLS 证书（本地 CA；仅 127.0.0.1 使用） ──
    certs = run_dir / "certs"
    openssl = lambda cmd: sh(cmd, timeout=120)  # noqa: E731
    openssl(["openssl", "genrsa", "-out", certs / "ca.key", "2048"])
    openssl(["openssl", "req", "-x509", "-new", "-key", certs / "ca.key",
             "-subj", "/CN=ani-lab-ca", "-days", "2", "-out", certs / "ca.pem"])
    for san, name in (("ani-governance", "ani-governance"),
                      ("ani-gpu-simulator", "ani-gpu-simulator"),
                      ("other-service", "other-service")):
        openssl(["openssl", "genrsa", "-out", certs / f"{name}.key", "2048"])
        ext = f"subjectAltName=DNS:{san}"
        (certs / f"{name}.ext").write_text(ext)
        openssl(["openssl", "req", "-new", "-key", certs / f"{name}.key",
                 "-subj", f"/CN={name}", "-out", certs / f"{name}.csr"])
        openssl(["openssl", "x509", "-req", "-in", certs / f"{name}.csr",
                 "-CA", certs / "ca.pem", "-CAkey", certs / "ca.key",
                 "-CAcreateserial", "-days", "2",
                 "-extfile", certs / f"{name}.ext",
                 "-out", certs / f"{name}.pem"])

    # ── 机密 ──
    secrets_dir = run_dir / "secrets"
    admin_password = "Qu0ta-Lab-" + secrets.token_hex(8) + "!A"
    (secrets_dir / "admin-password").write_text(admin_password)
    jwt_key = secrets.token_hex(32)
    control_token = secrets.token_hex(24)
    (secrets_dir / "control-token").write_text(control_token)
    ak_key = secrets.token_hex(32)
    (secrets_dir / "access-key-encryption").write_text(ak_key + "\n")
    for f in secrets_dir.iterdir():
        os.chmod(f, 0o600)

    # ── 治理配置 ──
    gov = {
        "server": {"rest": {"addr": f"127.0.0.1:{env['rest_port']}", "timeout": "10s",
                            "enable_swagger": False,
                            "middleware": {"enable_logging": True, "enable_recovery": True,
                                           "enable_validate": True, "enable_metadata": True}}},
        "data": {"database": {"driver": "postgres",
                              "source": f"host=127.0.0.1 port={pgp} user=postgres dbname=governance sslmode=disable",
                              "migrate": False, "max_open_connections": 10, "max_idle_connections": 5},
                 "redis": {"addr": f"127.0.0.1:{redis_port}", "dial_timeout": "10s",
                           "read_timeout": "1s", "write_timeout": "1s"}},
        "authn": {"type": "jwt", "jwt": {"method": "HS256", "key": jwt_key,
                                         "access_token_expires": "5400s",
                                         "refresh_token_expires": "43200s"}},
        "authz": {"type": "casbin", "casbin": {}},
        "logger": {"type": "std"},
    }
    (run_dir / "configs" / "governance.yaml").write_text(json.dumps(gov))
    # -c 需要"目录 + config.yaml"结构。
    gov_conf_dir = run_dir / "configs" / "gov"
    gov_conf_dir.mkdir(exist_ok=True)
    (gov_conf_dir / "config.yaml").write_text(json.dumps(gov))

    env_env = {
        "ANI_QUOTA_ENABLED": "true",
        "ANI_QUOTA_INTERNAL_ADDR": f"127.0.0.1:{env['quota_internal_port']}",
        "ANI_QUOTA_CA_FILE": str(certs / "ca.pem"),
        "ANI_QUOTA_CERT_FILE": str(certs / "ani-governance.pem"),
        "ANI_QUOTA_KEY_FILE": str(certs / "ani-governance.key"),
        "ANI_QUOTA_SIMULATOR_ADDR": f"127.0.0.1:{env['sim_grpc_port']}",
        "ANI_QUOTA_SIMULATOR_CA": str(certs / "ca.pem"),
        "ANI_QUOTA_SIMULATOR_CERT": str(certs / "ani-governance.pem"),
        "ANI_QUOTA_SIMULATOR_KEY": str(certs / "ani-governance.key"),
        "ANI_QUOTA_LAB_CONTROL_ADDR": f"127.0.0.1:{env['gov_control_port']}",
        "ANI_QUOTA_LAB_CONTROL_TOKEN_FILE": str(secrets_dir / "control-token"),
        "ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE": str(secrets_dir / "access-key-encryption"),
    }
    (run_dir / "configs" / "governance-quota.env").write_text(
        "\n".join(f"{k}={v}" for k, v in env_env.items()))

    sim_env = {
        "SIM_OWNER_DSN": pg_dsn(run_dir, "gpu_owner").replace("postgres://", "postgresql://"),
        "SIM_PROVIDER_DSN": pg_dsn(run_dir, "gpu_provider").replace("postgres://", "postgresql://"),
        "SIM_LISTEN_ADDR": f"127.0.0.1:{env['sim_grpc_port']}",
        "SIM_TLS_CA_FILE": str(certs / "ca.pem"),
        "SIM_TLS_CERT_FILE": str(certs / "ani-gpu-simulator.pem"),
        "SIM_TLS_KEY_FILE": str(certs / "ani-gpu-simulator.key"),
        "SIM_CONTROL_ADDR": f"127.0.0.1:{env['sim_control_port']}",
        "SIM_CONTROL_TOKEN_FILE": str(secrets_dir / "control-token"),
        "ANI_QUOTA_INTERNAL_ADDR": f"127.0.0.1:{env['quota_internal_port']}",
        "ANI_QUOTA_CA_FILE": str(certs / "ca.pem"),
        "ANI_QUOTA_CERT_FILE": str(certs / "ani-gpu-simulator.pem"),
        "ANI_QUOTA_KEY_FILE": str(certs / "ani-gpu-simulator.key"),
    }
    (run_dir / "configs" / "simulator.env").write_text(
        "\n".join(f"{k}={v}" for k, v in sim_env.items()))
    log(f"prepared: pg={pgp} rest={env['rest_port']}")
    return 0


def read_env_file(path):
    out = {}
    for line in pathlib.Path(path).read_text().splitlines():
        if "=" in line and not line.startswith("#"):
            k, v = line.split("=", 1)
            out[k] = v
    return out


def atlas_apply(run_dir, db, amount=None):
    cmd = [str(ATLAS), "migrate", "apply"]
    if amount:
        cmd.append(str(amount))
    cmd += ["--dir", "file://migrations", "--url", pg_dsn(run_dir, db)]
    return sh(cmd, capture=False) if False else sh(cmd)


def cmd_migrate(args):
    run_dir = pathlib.Path(args.run_dir)
    logd = run_dir / "logs"
    steps = []

    # Simulator startup has no DDL. Its fixture schema is applied only by this
    # explicit migration command, using the isolated administrator identity.
    for kind, database in (("owner", "gpu_owner"), ("provider", "gpu_provider")):
        schema = pathlib.Path("app/admin/service/internal/quotalab/simulator/testdata") / f"{kind}-schema.sql"
        with schema.open("rb") as source:
            result = subprocess.run(
                ["docker", "exec", "-i", PG_CONTAINER, "psql", "-U", "postgres",
                 "-d", database, "-v", "ON_ERROR_STOP=1"], stdin=source,
                capture_output=True, timeout=120)
        (logd / f"migrate-simulator-{kind}.log").write_bytes(result.stdout + result.stderr)
        if result.returncode:
            die(f"simulator {kind} explicit migration failed")

    def apply(db, amount, label, save=True):
        r = atlas_apply(run_dir, db, amount)
        if save:
            (logd / f"migrate-{label}.log").write_text(r.stdout + r.stderr)
        if r.returncode != 0:
            die(f"migrate {label} failed: {r.stderr[-500:]}")
        steps.append((label, r.stdout.strip().splitlines()[-1] if r.stdout else ""))

    # 空库：初始 → expand → data → constraints（一次一份，逐步核对）。
    apply("governance", 1, "governance-initial")
    apply("governance", 1, "governance-expand")
    with open("sql/quota/001_catalog_and_backfill.sql", "rb") as fin:
        r = subprocess.run(["docker", "exec", "-i", PG_CONTAINER, "psql", "-U", "postgres",
                            "-d", "governance", "-v", "ON_ERROR_STOP=1"],
                           stdin=fin, capture_output=True, text=True, timeout=120)
    (logd / "migrate-data.log").write_text(r.stdout + r.stderr)
    if r.returncode != 0:
        die(f"data script failed: {r.stderr[-500:]}")
    apply("governance", 1, "governance-constraints")

    status = atlas_apply(run_dir, "governance", None)
    status_r = sh([str(ATLAS), "migrate", "status", "--dir", "file://migrations",
                   "--url", pg_dsn(run_dir, "governance")])
    (logd / "migrate-status.log").write_text(status_r.stdout + status_r.stderr)
    catalog = subprocess.run(["docker", "exec", PG_CONTAINER, "psql", "-U", "postgres",
                              "-d", "governance", "-tAc",
                              "SELECT code FROM sys_quota_definitions ORDER BY code"],
                             capture_output=True, text=True)
    (logd / "migrate-catalog.txt").write_text(catalog.stdout)
    log(f"migrate done: catalog={catalog.stdout.split()}")
    return 0


def cmd_build(args):
    run_dir = pathlib.Path(args.run_dir)
    bin_dir = run_dir / "bin"
    env = {"GOWORK": "off", "GOMAXPROCS": "2", "GOFLAGS": "-p=2"}
    targets = [
        (["go", "build", "-o", str(bin_dir / "governance"), "./app/admin/service/cmd/server"], {}),
        (["go", "build", "-tags", "quota_lab", "-o", str(bin_dir / "governance-quota-lab"),
          "./app/admin/service/cmd/server"], {"GOFLAGS": "-p=2"}),
        (["go", "build", "-tags", "quota_lab", "-o", str(bin_dir / "gpu-simulator"),
          "./app/admin/service/cmd/quota-gpu-simulator"], {}),
        (["go", "build", "-o", str(bin_dir / "admin"), "./app/admin/service/cmd/admin"], {}),
    ]
    for cmd, extra in targets:
        r = sh(cmd, env=extra)
        if r.returncode != 0:
            die(f"build {' '.join(cmd[-1:])} failed:\n{r.stderr[-2000:]}")
        log(f"built {cmd[-1]}")
    return 0


def cmd_seed(args):
    run_dir = pathlib.Path(args.run_dir)
    bin_dir = run_dir / "bin"
    env = load_env(run_dir)
    dsn = pg_dsn(run_dir, "governance").replace("postgres://", "postgresql://")
    logd = run_dir / "logs"
    # 离线首次初始化（单事务；密码文件传入）。
    r = sh([str(bin_dir / "admin"), "init", "--username", "admin",
            "--password-file", str(run_dir / "secrets" / "admin-password")],
           env={"ANI_DATABASE_DSN": dsn})
    (logd / "seed-init.log").write_text(r.stdout + "\n[stderr redacted]" if "password" in (r.stderr or "").lower() else r.stdout + r.stderr)
    if r.returncode != 0:
        die(f"admin init failed: {r.stderr[-800:]}")
    r = sh([str(bin_dir / "admin"), "sync-apis", "--dry-run"], env={"ANI_DATABASE_DSN": dsn})
    (logd / "seed-sync-dry.log").write_text(r.stdout + r.stderr)
    r = sh([str(bin_dir / "admin"), "sync-apis"], env={"ANI_DATABASE_DSN": dsn})
    (logd / "seed-sync.log").write_text(r.stdout + r.stderr)
    if r.returncode != 0:
        die(f"sync-apis failed: {r.stderr[-800:]}")
    r = sh([str(bin_dir / "admin"), "check"], env={"ANI_DATABASE_DSN": dsn})
    (logd / "seed-check.log").write_text(r.stdout + r.stderr)
    if r.returncode != 0:
        die(f"admin check failed: {r.stderr[-800:]}")
    log("seed done (offline init + api sync)")
    return 0


def read_secrets(run_dir):
    return read_env_file(run_dir / "configs" / "governance-quota.env")


def spawn(run_dir, name, cmd, env):
    e = dict(os.environ)
    e.update(env)
    logf = open(run_dir / "logs" / f"{name}.log", "ab")
    p = subprocess.Popen(cmd, env=e, stdout=logf, stderr=subprocess.STDOUT,
                         start_new_session=True)
    pids = json.loads((run_dir / "pids.json").read_text()) if (run_dir / "pids.json").exists() else {}
    pids[name] = p.pid
    (run_dir / "pids.json").write_text(json.dumps(pids))
    return p


def cmd_start(args):
    run_dir = pathlib.Path(args.run_dir)
    env = load_env(run_dir)
    bin_dir = run_dir / "bin"
    sim_env = read_env_file(run_dir / "configs" / "simulator.env")
    gov_env = read_env_file(run_dir / "configs" / "governance-quota.env")
    gov_conf = str(run_dir / "configs" / "governance.yaml")

    spawn(run_dir, "gpu-simulator", [str(bin_dir / "gpu-simulator")], sim_env)
    time.sleep(1.5)
    spawn(run_dir, "governance", [str(bin_dir / "governance-quota-lab"), "-c", str(run_dir / "configs" / "gov")], gov_env)
    base = f"http://127.0.0.1:{env['rest_port']}"
    wait_http(f"{base}/admin/v1/plans", timeout=60)
    log(f"started: governance lab at {base}, simulator grpc={sim_env['SIM_LISTEN_ADDR']}")
    (run_dir / "base-url.txt").write_text(base)
    return 0


def cmd_stop(args):
    run_dir = pathlib.Path(args.run_dir)
    if (run_dir / "pids.json").exists():
        pids = json.loads((run_dir / "pids.json").read_text())
        for name, pid in pids.items():
            try:
                os.killpg(os.getpgid(pid), 15)
                log(f"stopped {name} ({pid})")
            except ProcessLookupError:
                pass
        (run_dir / "pids.json").unlink(missing_ok=True)
    return 0


def cmd_cleanup(args):
    run_dir = pathlib.Path(args.run_dir)
    for name in (PG_CONTAINER, REDIS_CONTAINER):
        r = sh(["docker", "inspect", "-f", "{{index .Config.Labels \"quota-gpu-local-01\"}}", name])
        if r.returncode == 0 and r.stdout.strip() == "true":
            sh(["docker", "rm", "-f", name])
            log(f"removed container {name}")
        else:
            log(f"container {name} not labeled by this task; left untouched")
    return 0


def cmd_collect(args):
    run_dir = pathlib.Path(args.run_dir)
    out = EVIDENCE / "run-artifacts"
    out.mkdir(parents=True, exist_ok=True)
    for name in ("migrate-catalog.txt", "migrate-status.log", "preflight.json"):
        src = run_dir / "logs" / name
        if src.exists():
            shutil.copy(src, out / name)
    for name in ("governance.log", "gpu-simulator.log"):
        src = run_dir / "logs" / name
        if src.exists():
            data = src.read_text(errors="replace")
            # 脱敏：不带 JWT/密码。
            lines = [l for l in data.splitlines()
                     if "password" not in l.lower() and "Bearer" not in l]
            (out / f"{name}.tail.log").write_text("\n".join(lines[-400:]))
    log(f"collected into {out}")
    return 0


# ── accept 套件 ──────────────────────────────────────────────

def cmd_accept(args):
    from quota_acceptance import run_suite  # 同目录模块
    run_dir = pathlib.Path(args.run_dir)
    env = load_env(run_dir)
    base = f"http://127.0.0.1:{env['rest_port']}"
    ctx = {
        "run_dir": str(run_dir),
        "base": base,
        "env": env,
        "pg_port": env["pg_port"],
        "admin_password": (run_dir / "secrets" / "admin-password").read_text(),
        "gov_control": f"http://127.0.0.1:{env['gov_control_port']}",
        "sim_control": f"http://127.0.0.1:{env['sim_control_port']}",
        "control_token": (run_dir / "secrets" / "control-token").read_text(),
        "certs": str(run_dir / "certs"),
    }
    return run_suite(ctx, args.suite, args.case)


def main():
    ap = argparse.ArgumentParser(prog="run.py")
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--run-dir", required=True)
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("preflight", "prepare", "migrate", "build", "seed", "start",
                 "accept", "collect", "stop", "cleanup"):
        p = sub.add_parser(name, parents=[common])
        if name == "preflight":
            p.add_argument("--force", action="store_true")
        if name == "accept":
            p.add_argument("--suite", required=True,
                           choices=["normal", "migration", "auth", "concurrency",
                                    "recovery", "boundary", "all"])
            p.add_argument("--case", default=None)
        p.set_defaults(func=globals()[f"cmd_{name}"])
    args = ap.parse_args()
    sys.exit(args.func(args))


if __name__ == "__main__":
    main()
