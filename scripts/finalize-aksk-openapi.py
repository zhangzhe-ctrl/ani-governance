#!/usr/bin/env python3
"""Remove gnostic synthetic responses shadowed by explicit source annotations.

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


def remove_shadowed_200(text):
    # gnostic prepends an inferred response before an explicit annotated 200.
    # Keep the source annotation without leaving duplicate YAML mapping keys.
    operations = r"(?m)^        (?:get|post|put|patch|delete|head|options):\n(?:(?: {9,}.*|)\n)*"
    response = r"(?m)^                ['\"]?200['\"]?:\n(?:(?: {17,}.*|)\n)*"

    def normalize(match):
        operation = match.group(0)
        candidates = list(re.finditer(response, operation))
        if len(candidates) > 2:
            raise SystemExit("ambiguous duplicate 200 responses")
        if len(candidates) == 2:
            inferred = candidates[0]
            return operation[:inferred.start()] + operation[inferred.end():]
        return operation

    return re.sub(operations, normalize, text)


text = remove_synthetic_200(path.read_text(), "/api/v1/auth/api-keys", "201")
# Minimal generator fixtures may omit ModelDev. Its public contract test
# separately requires this path in the complete service document.
if "    /admin/v1/modeldev/executions:\n" in text:
    text = remove_synthetic_200(text, "/admin/v1/modeldev/executions", "202")
    text = remove_synthetic_200(text, "/admin/v1/modeldev/executions/{execution_id}:stop", "202")
path.write_text(remove_shadowed_200(text))
