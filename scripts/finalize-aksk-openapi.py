#!/usr/bin/env python3
"""Remove gnostic's synthetic 200 from explicit non-200 create operations.

This is part of generation, never a manual edit of the generated document.
"""
import pathlib
import re

path = pathlib.Path(__file__).resolve().parents[1] / "app/admin/service/cmd/server/assets/openapi.yaml"

def remove_synthetic_200(text, route, status):
    start = text.index("    " + route + ":\n")
    end = text.find("\n    /", start + 1)
    if end == -1:
        end = text.index("\ncomponents:", start)
    section = text[start:end]
    post = section.index("        post:\n")
    prefix, operation = section[:post], section[post:]
    if '"' + status + '":' not in operation and "'" + status + "':" not in operation:
        raise SystemExit("source Create operation must explicitly declare " + status)
    operation, count = re.subn(r"(?m)^                ['\"]?200['\"]?:\n(?:(?: {17,}.*|)\n)*", "", operation, count=1)
    if count != 1:
        raise SystemExit("expected one synthetic 200 response for " + route)
    return text[:start] + prefix + operation + text[end:]


text = remove_synthetic_200(path.read_text(), "/api/v1/auth/api-keys", "201")
# Minimal generator fixtures may omit ModelDev. Its public contract test
# separately requires this path in the complete service document.
if "    /admin/v1/modeldev/executions:\n" in text:
    text = remove_synthetic_200(text, "/admin/v1/modeldev/executions", "202")
    text = remove_synthetic_200(text, "/admin/v1/modeldev/executions/{execution_id}:stop", "202")
path.write_text(text)
