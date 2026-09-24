#!/usr/bin/env python3
"""Prepare private test-process configuration from the Accelerator B-layer manifest.

Run only on Fedora, inside the authorized isolated task. No database migration,
production registration, or credentials are printed by this configuration step.
"""
import json
import os
from pathlib import Path
import secrets
import sys

root = Path(sys.argv[1]).resolve()
manifest = json.loads((root / "ready.json").read_text())
for name in ("gov-dsn", "owner-dsn"):
    if not (root / name).is_file():
        raise SystemExit(f"missing explicit database provisioning: {name}")

def private_json(name, value):
    path = root / name
    fd = os.open(path, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, indent=2)
        stream.write("\n")

private_json("governance-config.json", {
    "Address": "127.0.0.1:25561",
    "ReleaseAddress": "127.0.0.1:25563",
    "OwnerAddress": "https://127.0.0.1:25562",
    "CA": manifest["ca_file"],
    "Cert": manifest["gov_cert_file"],
    "Key": manifest["gov_key_file"],
    "Accelerator": {
        "Address": manifest["address"],
        "ServerName": manifest["server_name"],
        "CAFile": manifest["ca_file"],
        "CertFile": manifest["gov_cert_file"],
        "KeyFile": manifest["gov_key_file"],
        "Timeout": 3_000_000_000,
    },
})
private_json("owner-config.json", {
    "Address": "127.0.0.1:25562",
    "GovernanceAddress": "127.0.0.1:25563",
    "CA": manifest["ca_file"],
    "Cert": manifest["owner_cert_file"],
    "Key": manifest["owner_key_file"],
})
token = root / "control-token"
if not token.exists():
    fd = os.open(token, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as stream:
        stream.write(secrets.token_hex(32))
print("prepared isolated test configuration; no hardware or production owner")
