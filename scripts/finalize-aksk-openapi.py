#!/usr/bin/env python3
"""Deterministic workaround for gnostic v0.7.1's unconditional 200 response.

The source operation explicitly declares 201; remove only its synthetic 200.
This is part of generation, never a manual edit of the generated document.
"""
import pathlib
import re

path = pathlib.Path(__file__).resolve().parents[1] / "app/admin/service/cmd/server/assets/openapi.yaml"
text = path.read_text()
start = text.index("    /api/v1/auth/api-keys:\n")
end = text.index("\n    /", start + 1)
section = text[start:end]
post = section.index("        post:\n")
prefix, operation = section[:post], section[post:]
if '"201":' not in operation and "'201':" not in operation:
    raise SystemExit("source Create operation must explicitly declare 201")
operation, n = re.subn(r"(?m)^                ['\"]?200['\"]?:\n(?:(?: {17,}.*|)\n)*", "", operation, count=1)
if n != 1:
    raise SystemExit("expected one synthetic 200 response for AccessKey Create")
path.write_text(text[:start] + prefix + operation + text[end:])
