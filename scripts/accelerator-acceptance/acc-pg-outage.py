#!/usr/bin/env python3
"""Bounded failure of only the GOV-ACC-V12-01 Accelerator PostgreSQL container."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shlex
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import urlopen

root = Path(os.environ["GOV_ACC_TASK_ROOT"])
assert subprocess.check_output(["hostname"], text=True).strip() == "fedora"
ready = json.loads((root / "joint-a/formal-resume/ready.json").read_text())
config = json.loads(Path(ready["config_file"]).read_text())
dsn = Path(config["database"]["dsnFile"]).read_text().strip()
database = (urlsplit(dsn).path.lstrip("/") if dsn.startswith(("postgres://", "postgresql://"))
            else dict(part.split("=", 1) for part in shlex.split(dsn))["dbname"])
del dsn
container = "gov-acc-v12-01-acc"
evidence = root / "evidence/resume/acc-pg-outage"
evidence.mkdir(exist_ok=True)
base = "http://" + config["server"]["admin"]["addr"]

def state(path):
    try:
        with urlopen(base + path, timeout=2) as response:
            return response.status
    except HTTPError as error:
        return error.code
    except (URLError, TimeoutError):
        return 0

def await_status(path, wanted):
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        actual = state(path)
        if actual == wanted:
            return actual
        time.sleep(0.2)
    raise RuntimeError(f"{path}: expected {wanted}, observed {actual}")

def group():
    query = Path(__file__).with_name("acc-pg-outage.sql").read_bytes()
    return subprocess.check_output(["docker", "exec", "-i", container, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-v", "group_id=" + ready["group_id"], "-U", "postgres", "-d", database], input=query)

result = {"production_pid": ready["production_pid"], "binary_sha256": ready["binary_sha256"], "container": container, "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
try:
    result["ready_before"] = await_status("/readyz", 200)
    before = group()
    assert before.strip()
    subprocess.run(["docker", "stop", "--time", "5", container], check=True, stdout=subprocess.DEVNULL)
    result["ready_during"] = await_status("/readyz", 503)
    result["health_during"] = state("/healthz")
    assert result["health_during"] == 200
finally:
    subprocess.run(["docker", "start", container], check=True, stdout=subprocess.DEVNULL)
result["ready_after"] = await_status("/readyz", 200)
after = group()
assert before == after, "persisted formal supply group changed across restart"
os.kill(ready["production_pid"], 0)
result["group_sha256_before"] = hashlib.sha256(before).hexdigest()
result["group_sha256_after"] = hashlib.sha256(after).hexdigest()
result["same_production_process"] = True
result["status"] = "pass"
result["finished_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
(evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result, indent=2))
