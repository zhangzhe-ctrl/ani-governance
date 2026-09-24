#!/usr/bin/env python3
"""Run the ordinary Governance binary with real isolated PG/Redis and optional A Acc.

This is external test orchestration. It neither adds a service test mode nor
copies session keys, tokens, DSNs or certificate private keys into evidence.
"""
import json
import hashlib
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

root = Path(sys.argv[1]).resolve()
private = root / "task/formal-gov"
material = json.loads((private / "session.json").read_text())
dsn = (private / "gov-dsn").read_text().strip()
acc = json.loads(Path(sys.argv[2]).read_text()) if len(sys.argv) > 2 else None
private.mkdir(mode=0o700, exist_ok=True)
os.chmod(private, 0o700)
config_dir = private / "config"
config_dir.mkdir(mode=0o700, exist_ok=True)
configuration = {
    "server": {"rest": {"addr": "127.0.0.1:25571", "timeout": "10s", "enable_swagger": False,
                        "middleware": {"enable_recovery": True, "enable_validate": True}}},
    "data": {"database": {"driver": "postgres", "source": dsn, "migrate": False,
                           "max_open_connections": 10, "max_idle_connections": 3},
             "redis": {"addr": "127.0.0.1:19382", "dial_timeout": "5s", "read_timeout": "1s", "write_timeout": "1s"}},
    "authn": {"type": "jwt", "jwt": {"method": "HS256", "key": material["jwt_key"]}},
    "authz": {"type": "casbin", "casbin": {}}, "logger": {"type": "std"},
}

def private_write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as stream:
        stream.write(value)
    os.chmod(path, 0o600)

private_write(config_dir / "config.yaml", json.dumps(configuration))
private_write(private / "access-key", secrets.token_hex(32))
hash_source = private / "password-hash.go"
private_write(hash_source, 'package main\nimport("crypto/rand";"encoding/hex";"fmt";"golang.org/x/crypto/bcrypt")\nfunc main(){ b:=make([]byte,32); if _,e:=rand.Read(b);e!=nil{panic(e)};h,e:=bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(b)),bcrypt.DefaultCost);if e!=nil{panic(e)};fmt.Print(string(h))}\n')
hash_environment = dict(os.environ, GOWORK="off", GOMAXPROCS="2", GOFLAGS="-p=2",
                        GOMODCACHE=str(root / "cache/gov-bff-mod"), GOCACHE=str(root / "cache/gov-bff-build"))
password_hash = subprocess.check_output(["go", "run", str(hash_source)], env=hash_environment, text=True)
fixture = Path("sql/gpu/ops/formal_bff_permissions.sql").read_text().replace("__PASSWORD_HASH__", password_hash).encode()
result = subprocess.run(["podman", "exec", "-i", "gov-acc-v12-01-gov-pg", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "gov_acc_formal"], input=fixture, capture_output=True)
if result.returncode:
    raise SystemExit("explicit formal permission fixture failed: " + result.stderr.decode())
environment = dict(os.environ)
for name in list(environment):
    if name.startswith(("ANI_ACCELERATOR_", "ANI_QUOTA_", "SIM_")):
        environment.pop(name)
environment["ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE"] = str(private / "access-key")
environment["GOWIND_CRYPTO_KEY"] = secrets.token_hex(32)
if acc:
    for key, value in {"ADDR": acc["address"], "SERVER_NAME": acc["server_name"], "CA": acc["ca_file"], "CERT": acc["gov_cert_file"], "KEY": acc["gov_key_file"]}.items():
        environment["ANI_ACCELERATOR_" + key] = value
evidence = {"layer": "A", "ordinary_binary": True, "quota_lab": False, "hardware_observed": False,
            "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "checks": [],
            "governance_binary_sha256": hashlib.sha256((private / "ani-governance").read_bytes()).hexdigest(),
            "accelerator_binary_sha256": acc["binary_sha256"] if acc else None}
log_path = private / "ordinary-process.private.log"
private_write(log_path, "")
with log_path.open("ab") as log:
    process = subprocess.Popen([str(private / "ani-governance"), "-c", str(config_dir)], env=environment, stdout=log, stderr=subprocess.STDOUT)
try:
    def request(path, token, method="GET", body=None):
        request = urllib.request.Request("http://127.0.0.1:25571" + path, method=method,
                                         data=json.dumps(body).encode() if body is not None else None,
                                         headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        try:
            response = urllib.request.urlopen(request, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            body = json.loads(raw) if raw.startswith(b"{") else {}
            return response.status, body
    for attempt in range(60):
        if process.poll() is not None:
            raise RuntimeError("ordinary Governance process exited during startup")
        try:
            status, definitions = request("/admin/v1/quota-definitions", material["platform_token"])
            if status == 200:
                break
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.25)
    else:
        raise RuntimeError("ordinary Governance quota catalog did not become available")
    gpu = {item["code"]: item["enforcement"] for item in definitions["items"] if item["code"] in ("gpu.shared_memory_mib", "gpu.physical.count")}
    assert gpu == {"gpu.shared_memory_mib": "NOT_ENABLED", "gpu.physical.count": "NOT_ENABLED"}, gpu
    evidence["checks"].append({"path": "/admin/v1/quota-definitions", "status": 200, "gpu_enforcement": gpu})
    status, accounts = request("/api/v1/me/quota-accounts", material["tenant_token"])
    assert status == 200 and accounts["tenantId"] == 1
    evidence["checks"].append({"path": "/api/v1/me/quota-accounts", "status": status, "trusted_tenant": 1})
    for path, method in [("/api/v1/quota-lab/gpu-allocations", "POST"), ("/api/v1/quota-lab/operations/test", "GET"), ("/fault", "POST"), ("/control", "POST")]:
        status, _ = request(path, material["platform_token"], method)
        assert status == 404, (path, status)
        evidence["checks"].append({"path": path, "status": status})
    if acc:
        status, clusters = request("/admin/v1/accelerator/clusters", material["platform_token"])
        assert status == 200 and any(row["cluster_id"] == acc["cluster_id"] for row in clusters["items"])
        evidence["checks"].append({"path": "/admin/v1/accelerator/clusters", "status": status, "formal_acc_mtls": True, "count": len(clusters.get("items", []))})
        group_path = "/admin/v1/accelerator/supply-groups?pool_id=" + acc["pool_id"]
        status, groups = request(group_path, material["platform_token"])
        assert status == 200
        group = next(row for row in groups["items"] if row["group_id"] == acc["group_id"])
        assert group["admission"] == "ADMISSION_CLOSED" and group["verification_state"] == "NOT_VERIFIED"
        profile_path = "/admin/v1/accelerator/profiles?cluster_id=" + acc["cluster_id"]
        status, profiles_before = request(profile_path, material["platform_token"])
        assert status == 200 and not profiles_before.get("items")
        status, denied = request("/admin/v1/accelerator/profiles", material["platform_token"], "POST", {"data": {
            "idempotency_key": str(uuid.uuid4()), "display_name": "formal-gov-proof-gate",
            "spec": {"group_id": acc["group_id"], "mode": "SHARED_FIXED", "model_key": group["model_key"],
                     "shared_memory_mib": "6144", "core_limit_percent": 25, "max_devices_per_replica": 1,
                     "isolation_class": "SOFTWARE_COOPERATIVE"},
            "expected_group_version": group["version"], "verification_ref": "formal:missing-proof"}})
        assert status == 412 and denied.get("reason") == "CONSUMPTION_NOT_VERIFIED", (status, denied)
        status_after, profiles_after = request(profile_path, material["platform_token"])
        assert status_after == 200 and profiles_before == profiles_after
        status_after, groups_after = request(group_path, material["platform_token"])
        assert status_after == 200 and groups == groups_after
        evidence["checks"].append({"path": "/admin/v1/accelerator/profiles", "status": status,
                                  "reason": denied["reason"], "profile_count": 0,
                                  "group_unchanged": True, "admission": group["admission"],
                                  "verification_state": group["verification_state"]})
    evidence["status"] = "pass"
finally:
    process.send_signal(signal.SIGTERM) if process.poll() is None else None
    try:
        evidence["process_exit"] = process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        process.kill()
        evidence["process_exit"] = process.wait(timeout=5)
    evidence["finished_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    output = root / "evidence" / ("formal-gov-acc-process.json" if acc else "formal-gov-process.json")
    output.write_text(json.dumps(evidence, indent=2) + "\n")
    raw_log = log_path.read_text(errors="replace")
    for secret in [dsn, *material.values(), environment["GOWIND_CRYPTO_KEY"]]:
        raw_log = raw_log.replace(secret, "[REDACTED]")
    (root / "evidence/formal-gov-process.log").write_text(raw_log)
print(json.dumps(evidence))
