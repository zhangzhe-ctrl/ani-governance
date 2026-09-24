#!/usr/bin/env python3
"""GOV-ACC-V12-01 only. Default is read-only; --execute is irreversible.

Run on Fedora only after all acceptance jobs have ended. No wildcard process
signals, container pruning, image removal, or source/evidence removal.
"""

import argparse
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import time

ROOT = Path("/home/chabking/gov-acc-v12-01-20260923")
A_TEMP = Path("/tmp/TestFormalProductionConsumptionGate2132644118")
CONTAINERS = [
    ("docker", "gov-acc-v12-01-acc",
     "224573e9b689167b36b5bcd3ec8ba15d1a117b1b1a176acf074194e6bd5220dc",
     "ed70eb4db176adea821800913a7f444d61cb438942c52f71fe4780b2b75eff85"),
    ("podman", "gov-acc-v12-01-gov-pg",
     "78b915ff131bff554e623ed722506d9b1552376ffd9660ee6bd7495b17c3c7d4",
     "47201750b540fb41e61687daf3393edbf34268795b0f6ee16cfaed5b84bb64f0"),
    ("podman", "gov-acc-v12-01-bff-redis",
     "46ff43452c461d77bc15c86d9349532d44f1e548e2fd4bfea0dbfd592049fcdc",
     "ee05f849baf3e79681ce736ec510930eb1c0717c362036c29ca2fac38b7da79c"),
]
PROCESSES = {
    1085376: ("25735284", str(ROOT / "joint-a/formal-resume/contract.test")),
    1085520: ("25735893", str(A_TEMP / "001/source.test")),
    1085537: ("25735910", str(A_TEMP / "001/ani-accelerator-service")),
    1101036: ("25788646", str(ROOT / "joint-b/acc-contract-final.test")),
}
CACHES = [ROOT / "cache" / name for name in (
    "acc-build", "acc-mod", "gov-data-build", "gov-data-mod",
    "gov-bff-build", "gov-bff-mod", "gov-main-build", "gov-main-mod",
    "gov-fault-build", "gov-fault-mod",
)]
PRIVATE_FILES = """
acc-dsn acc-legacy-dsn acc-upgrade-final-dsn joint-a/acc.dsn
joint-a/source/provider.json joint-a/source/source.json
joint-b/acc.dsn joint-b/ca.pem joint-b/acc.pem joint-b/acc.key
joint-b/gov.pem joint-b/gov.key joint-b/owner.pem joint-b/owner.key
joint-b/cursor.key joint-b/hardware.pub joint-b/owner.pub
joint-b/provider.json joint-b/config.json joint-b/control-token
joint-b/gov-admin-dsn joint-b/gov-dsn joint-b/owner-admin-dsn joint-b/owner-dsn
joint-b/governance-config.json joint-b/owner-config.json
task/joint-b/gov-dsn task/joint-b/gov-admin-dsn
task/joint-b/owner-dsn task/joint-b/owner-admin-dsn
task/formal-gov/gov-dsn task/formal-gov/admin-dsn
task/formal-gov/session.json task/formal-gov/access-key
task/formal-gov/config/config.yaml
task/lab/gov-dsn task/lab/admin-dsn
task/gov-fault/runtime-dsn task/gov-fault/admin-dsn
task/gov-data-resume/runtime-dsn task/gov-upgrade-resume/runtime-dsn
""".split()


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def output(*args):
    return subprocess.check_output(args, text=True).strip()


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def digest(path):
    with path.open("rb") as src:
        return hashlib.file_digest(src, "sha256").hexdigest()


def process(pid):
    try:
        base = Path("/proc") / str(pid)
        fields = (base / "stat").read_text().rsplit(")", 1)[1].split()
        if fields[0] == "Z":
            return None
        return {"pid": pid, "ppid": int(fields[1]), "start": fields[19],
                "exe": os.readlink(base / "exe"),
                "cwd": os.readlink(base / "cwd"),
                "args": (base / "cmdline").read_bytes()}
    except (FileNotFoundError, ProcessLookupError, PermissionError):
        return None


def task_processes():
    ignored = {os.getpid()}
    ancestor = process(os.getpid())
    while ancestor and ancestor["ppid"] > 1:
        ignored.add(ancestor["ppid"])
        ancestor = process(ancestor["ppid"])
    found = []
    for item in Path("/proc").iterdir():
        if not item.name.isdigit() or int(item.name) in ignored:
            continue
        proc = process(int(item.name))
        if proc and (str(ROOT) in proc["cwd"] or str(ROOT) in proc["exe"]
                     or str(ROOT).encode() in proc["args"]
                     or proc["pid"] in PROCESSES):
            found.append(proc)
    return found


def process_guard(allow_helpers):
    for proc in task_processes():
        expected = PROCESSES.get(proc["pid"])
        require(allow_helpers and expected == (proc["start"], proc["exe"]),
                f"Task process still active or PID changed: pid={proc['pid']} "
                f"exe={proc['exe']} (no signal sent)")


def inspect_containers():
    result = []
    for engine, name, identity, volume in CONTAINERS:
        info = json.loads(output(engine, "inspect", name))[0]
        require(info["Id"] == identity, f"Container identity changed: {name}")
        mounts = info.get("Mounts", [])
        require(len(mounts) == 1 and mounts[0]["Type"] == "volume"
                and mounts[0]["Name"] == volume, f"Mounts changed: {name}")
        vol = json.loads(output(engine, "volume", "inspect", volume))[0]
        require(vol.get("Anonymous") is True or
                "com.docker.volume.anonymous" in (vol.get("Labels") or {}),
                f"Volume no longer identified as anonymous: {volume}")
        users = output(engine, "ps", "-a", "--no-trunc", "--filter",
                       "volume=" + volume, "--format", "{{.ID}}")
        require(users == identity, f"Unexpected volume user: {volume}")
        require(info["State"]["Running"], f"Container must be running for backup: {name}")
        result.append({"engine": engine, "name": name, "id": identity,
                       "image": info["Image"], "volume": volume})
    return result


def private_paths():
    files = [ROOT / name for name in PRIVATE_FILES]
    # Only these reviewed leaf names under this test's UUID directories.
    for directory in (ROOT / "joint-b").iterdir():
        if re.fullmatch(r"policy-recovery-[0-9a-f-]{36}", directory.name):
            require(not directory.is_symlink(), "Policy directory is a symlink")
            files.extend(directory / name for name in (
                "acc-no-grants.json", "governance-config.backup.json"))
    certs = ROOT / "task/lab/certs"
    require(not certs.is_symlink(), "Lab certificate directory is a symlink")
    if certs.exists():
        for item in certs.iterdir():
            require(item.suffix in (".key", ".pem", ".csr", ".ext", ".srl"),
                    "Unexpected file in isolated lab certificate directory")
            files.append(item)
    for path in files:
        require(path.resolve().is_relative_to(ROOT), "Private path escapes task root")
        require(not path.is_symlink(), "Private path is a symlink")
        if path.exists():
            require(path.is_file() and path.stat().st_uid == os.getuid(),
                    f"Unexpected private file owner/type: {path}")
    return sorted(set(path for path in files if path.exists()))


def wait_stopped(pids):
    deadline = time.monotonic() + 45
    while any(process(pid) for pid in pids):
        require(time.monotonic() < deadline, "Graceful stop timed out; no force-kill")
        time.sleep(0.25)


def dbs(engine, name):
    return output(engine, "exec", name, "psql", "-U", "postgres", "-Atc",
                  "SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY datname").splitlines()


def no_db_clients(engine, name):
    count = output(engine, "exec", name, "psql", "-U", "postgres", "-Atc",
                   "SELECT count(*) FROM pg_stat_activity WHERE backend_type='client backend' AND pid<>pg_backend_pid()")
    require(count == "0", f"Database still has clients: {name}; stop workers first")


def backup_databases(destination):
    for engine, name, _, _ in CONTAINERS[:2]:
        no_db_clients(engine, name)
        cluster = destination / name
        cluster.mkdir(mode=0o700)
        with (cluster / "cluster.sql").open("xb") as dst:
            run(engine, "exec", name, "pg_dumpall", "-U", "postgres", stdout=dst)
        with (cluster / "cluster.sql").open("rb") as src:
            src.seek(max(0, (cluster / "cluster.sql").stat().st_size - 4096))
            require(b"PostgreSQL database cluster dump complete" in src.read(),
                    f"Incomplete cluster dump: {name}")
        databases = dbs(engine, name)
        (cluster / "databases.json").write_text(json.dumps(databases, indent=2) + "\n")
        for database in databases:
            require(re.fullmatch(r"[A-Za-z0-9_]+", database), "Unexpected database name")
            path = cluster / (database + ".dump")
            with path.open("xb") as dst:
                run(engine, "exec", name, "pg_dump", "-U", "postgres", "-Fc",
                    "--dbname", database, stdout=dst)
            with path.open("rb") as src, path.with_suffix(".toc").open("xb") as dst:
                run(engine, "exec", "-i", name, "pg_restore", "--list", stdin=src, stdout=dst)
        no_db_clients(engine, name)
    engine, name, _, _ = CONTAINERS[2]
    require(output(engine, "exec", name, "redis-cli", "SAVE") == "OK", "Redis SAVE failed")
    run(engine, "cp", name + ":/data/dump.rdb", str(destination / "redis.rdb"))


def preserve_existing_dumps(destination):
    retained = []
    paths = sorted((ROOT / "evidence").rglob("*.dump"))
    paths.append(ROOT / "task/formal-gov/identity-snapshot.dump")
    for path in paths:
        require(not path.is_symlink() and path.resolve().is_relative_to(ROOT)
                and path.stat().st_uid == os.getuid(), "Unexpected existing dump path/owner")
        path.chmod(0o600)
        retained.append({"path": str(path.relative_to(ROOT)), "sha256": digest(path)})
    (destination / "retained-existing-dumps.json").write_text(json.dumps(retained, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true", help="Stop helpers, back up, then remove reviewed task resources")
    args = parser.parse_args()
    require(ROOT.is_dir() and ROOT.resolve() == ROOT, "Unexpected task root")
    require(output("hostname").split(".")[0] == "fedora", "Run only on Fedora")
    require(os.getuid() == ROOT.stat().st_uid, "Run as task directory owner")
    metadata = inspect_containers()
    private = private_paths()
    procs = [{k: v for k, v in p.items() if k != "args"} for p in task_processes()]
    print(json.dumps({"observed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                      "execute": args.execute, "containers": metadata,
                      "processes": procs, "private_files": [str(p) for p in private],
                      "cache_directories": [str(p) for p in CACHES],
                      "preserve": ["all source checkouts", "all evidence", "version locks",
                                   "existing recovery dumps", "recovery/final-*"]}, indent=2), flush=True)
    if not args.execute:
        return
    # Nonblocking locks before any stop/write. Another acceptance run refuses cleanup.
    held_locks = []
    for lock in sorted((ROOT / "locks").glob("*.lock")):
        handle = lock.open("r+")
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        held_locks.append(handle)
    process_guard(True)
    os.umask(0o077)
    recovery = ROOT / "recovery"
    require(not recovery.is_symlink(), "Recovery directory is a symlink")
    recovery.mkdir(mode=0o700, exist_ok=True)
    recovery.chmod(0o700)
    destination = recovery / ("final-" + datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ"))
    destination.mkdir(mode=0o700)
    preserve_existing_dumps(destination)
    # A wrapper removes its own t.TempDir on graceful stop; retain its runtime logs first.
    for leaf in ("production.log", "source.log"):
        source = A_TEMP / "001" / leaf
        if source.is_file():
            shutil.copyfile(source, destination / ("formal-a-" + leaf))
    if process(1085376):
        (ROOT / "joint-a/formal-resume/stop").touch(mode=0o600)
    wait_stopped([1085376, 1085520, 1085537])
    if process(1101036):
        proc = process(1101036)
        require((proc["start"], proc["exe"]) == PROCESSES[1101036], "B PID identity changed")
        os.kill(1101036, signal.SIGINT)
    wait_stopped([1101036])
    process_guard(False)
    backup_databases(destination)
    (destination / "containers.json").write_text(json.dumps(metadata, indent=2) + "\n")
    checksums = []
    for path in sorted(destination.rglob("*")):
        if path.is_file():
            path.chmod(0o600)
            checksums.append(f"{digest(path)}  {path.relative_to(destination)}")
    (destination / "SHA256SUMS").write_text("\n".join(checksums) + "\n")
    run("sha256sum", "--check", "SHA256SUMS", cwd=destination)
    (destination / "BACKUP_COMPLETE").write_text("Dump commands, archive TOCs and SHA256 checks passed; restore NOT run by cleanup.\n")
    for path in destination.rglob("*"):
        if path.is_file():
            with path.open("rb") as src:
                os.fsync(src.fileno())
    # Recheck all identities and writer absence immediately before irreversible removal.
    process_guard(False)
    inspect_containers()
    for engine, name, _, _ in CONTAINERS[:2]:
        no_db_clients(engine, name)
    for engine, name, identity, volume in CONTAINERS:
        run(engine, "stop", "--time", "30", identity)
        run(engine, "rm", identity)
        require(not output(engine, "ps", "-a", "--filter", "volume=" + volume,
                           "--format", "{{.ID}}"), f"Volume gained another user: {volume}")
        run(engine, "volume", "rm", volume)
    for path in private_paths():
        path.unlink()
    # A normally already removed its exact temporary directory. Fail closed if it remains.
    require(not A_TEMP.exists(), "A temporary directory remains; inspect manually, no broad /tmp deletion")
    for cache in CACHES:
        require(not cache.is_symlink() and cache.resolve().parent == ROOT / "cache", "Unexpected cache path")
        if cache.exists():
            for directory, _, _ in os.walk(cache):
                path = Path(directory)
                require(path.stat().st_uid == os.getuid(), "Cache directory has another owner")
                path.chmod(path.stat().st_mode | 0o700)
            shutil.rmtree(cache)
    (destination / "CLEANUP_COMPLETE").write_text("Reviewed task containers, anonymous volumes, private files and caches removed. Sources/evidence/backups retained.\n")
    print("Cleanup complete; protected recovery directory: " + str(destination))


if __name__ == "__main__":
    main()
