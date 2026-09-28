#!/usr/bin/env python3
"""Remove credential-shaped text from a deliverable log or Go test JSON stream."""

from __future__ import annotations

import argparse
import pathlib
import re


PATTERNS = (
    (re.compile(r"(?i)\b(?:postgres(?:ql)?|redis)://[^\s\"\\]+"), "[REDACTED-DSN]"),
    (re.compile(r'(?i)("(?:password|passwd|token|secret|api[_-]?key|dsn)"\s*:\s*")[^"\\]*(")'),
     r"\1[REDACTED]\2"),
    (re.compile(r"(?i)\bAuthorization\s*:\s*Bearer\s+[^\s\"\\]+"),
     "Authorization: Bearer [REDACTED]"),
    (re.compile(r"(?i)\b(password|passwd|token|secret|api[_-]?key|dsn)\s*[:=]\s*"
                r"[^\s,\"\\]+"), r"\1=[REDACTED]"),
    (re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----.*?"
                r"-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----", re.S),
     "[REDACTED-PRIVATE-KEY]"),
)


def redact(raw: str) -> str:
    for pattern, replacement in PATTERNS:
        raw = pattern.sub(replacement, raw)
    return raw


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=pathlib.Path)
    parser.add_argument("destination", type=pathlib.Path)
    args = parser.parse_args()
    args.destination.write_text(redact(args.source.read_text(encoding="utf-8", errors="replace")),
                                encoding="utf-8")


if __name__ == "__main__":
    main()
