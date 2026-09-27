#!/usr/bin/env python3
"""Require named Go tests to have actually passed in one go test -json log.

A profile is a nonempty JSON list of {"Package": "...", "Test": "Test..."}.
Use run_checked.sh as well: this checker is not a substitute for command exit
status. Missing, skipped or failed required tests and failed packages fail.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--required", required=True)
    parser.add_argument("--log", required=True)
    args = parser.parse_args()
    try:
        profile: Any = json.loads(Path(args.required).read_text(encoding="utf-8"))
        if not isinstance(profile, list) or not profile:
            raise ValueError("required profile must be a nonempty list")
        required: set[tuple[str, str]] = set()
        for row in profile:
            if not isinstance(row, dict):
                raise ValueError("each profile row must be an object")
            pkg, test = row.get("Package"), row.get("Test")
            if not isinstance(pkg, str) or not pkg or not isinstance(test, str) or not test:
                raise ValueError("every row needs nonempty Package and Test strings")
            required.add((pkg, test))
        log_path = Path(args.log)
        states: dict[tuple[str, str], str] = {}
        nonpasses: dict[tuple[str, str], set[str]] = {}
        package_failures: set[str] = set()
        with log_path.open(encoding="utf-8", errors="replace") as stream:
            for line in stream:
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue  # Download messages/stderr are not Go JSON events.
                if not isinstance(event, dict):
                    continue
                action, pkg, test = event.get("Action"), event.get("Package"), event.get("Test")
                if action == "fail" and not test:
                    package_failures.add(pkg if isinstance(pkg, str) else "<unknown>")
                if isinstance(pkg, str) and isinstance(test, str) and action in ("pass", "skip", "fail"):
                    pair = (pkg, test)
                    states[pair] = action
                    if action != "pass":
                        nonpasses.setdefault(pair, set()).add(action)
        bad = []
        for pair in sorted(required):
            state = states.get(pair, "NOT_EXECUTED")
            if state != "pass" or pair in nonpasses:
                bad.append({"Package": pair[0], "Test": pair[1], "status": state,
                            "observed_nonpass": sorted(nonpasses.get(pair, set()))})
        passed = not bad and not package_failures
        print(json.dumps({"pass": passed, "required": len(required),
                          "bad_tests": bad, "failed_packages": sorted(package_failures)},
                         ensure_ascii=False, indent=2))
        return 0 if passed else 1
    except (OSError, ValueError, TypeError) as exc:
        print(f"invalid test evidence: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
