from __future__ import annotations
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]


class CIScriptTests(unittest.TestCase):
    def test_layout_rejects_retired_path_and_formal_test_dependency(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            target = root / 'app/admin/service/cmd/server/main.go'
            target.parent.mkdir(parents=True)
            target.write_text('package main\n')
            subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
            deps = root / 'deps.json'
            good = [
                {'ImportPath': 'go-wind-admin/app/admin/service/cmd/server'},
                {'ImportPath': 'go-wind-admin/app/admin/service/cmd/admin'},
            ]
            script = REPO / 'scripts/ci/check-layout.py'
            def check(items):
                deps.write_text(''.join(json.dumps(item) + '\n' for item in items))
                return subprocess.run(['python3', str(script), '--repo', str(root), '--deps-json', str(deps)], capture_output=True).returncode
            self.assertEqual(check(good), 0)
            self.assertNotEqual(check(good + [{'ImportPath': 'go-wind-admin/app/admin/service/tests/testutil'}]), 0)
            retired = root / 'migration/obsolete.txt'
            retired.parent.mkdir()
            retired.write_text('obsolete')
            subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
            self.assertNotEqual(check(good), 0)

    def test_quota_storage_rejects_bypass_and_missing_source(self):
        with tempfile.TemporaryDirectory() as td:
            repo = Path(td)
            script = REPO / 'scripts/ci/check_quota_storage.py'
            missing = subprocess.run(['python3', str(script), '--repo', str(repo)], capture_output=True)
            self.assertNotEqual(missing.returncode, 0)
            data = repo / 'app/admin/service/internal/data'
            data.mkdir(parents=True)
            (data / 'quota_repo.go').write_text('package data\n')
            passed = subprocess.run(['python3', str(script), '--repo', str(repo)], capture_output=True)
            self.assertEqual(passed.returncode, 0, passed.stdout + passed.stderr)
            (data / 'quota_repo.go').write_text('package data\nfunc f(){ db.ExecContext(ctx, "bad") }\n')
            rejected = subprocess.run(['python3', str(script), '--repo', str(repo)], capture_output=True)
            self.assertNotEqual(rejected.returncode, 0)

    def test_required_test_result_rejects_skip_missing_and_failure(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            profile = root / 'required.json'
            log = root / 'result.jsonl'
            profile.write_text(json.dumps([{'Package': 'p', 'Test': 'TestCritical'}]))
            script = REPO / 'scripts/ci/assert-test-results.py'
            def check(events):
                log.write_text(''.join(json.dumps(e) + '\n' for e in events))
                return subprocess.run(['python3', str(script), '--required', str(profile), '--log', str(log)], capture_output=True).returncode
            self.assertEqual(check([{'Package': 'p', 'Test': 'TestCritical', 'Action': 'pass'}]), 0)
            self.assertNotEqual(check([]), 0)
            self.assertNotEqual(check([{'Package': 'p', 'Test': 'TestCritical', 'Action': 'skip'}]), 0)
            self.assertNotEqual(check([{'Package': 'p', 'Action': 'fail'}]), 0)

    def test_redis_stop_rejects_foreign_container(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            state = root / 'state'
            state.mkdir()
            (state / 'id').write_text('foreign-id')
            (state / 'owner').write_text('our-run')
            fake = root / 'fake-engine'
            marker = root / 'removed'
            fake.write_text('#!/bin/sh\ncase "$1" in inspect) echo foreign-run;; rm) touch "$FAKE_REMOVED";; esac\n')
            fake.chmod(0o755)
            env = dict(os.environ, ANI_CONTAINER_CLI=str(fake), ANI_TEST_REDIS_STATE_DIR=str(state), FAKE_REMOVED=str(marker))
            result = subprocess.run(['bash', str(REPO / 'scripts/ci/with-redis.sh'), 'stop'], env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(marker.exists())
            self.assertTrue((state / 'id').exists())

    def test_ent_check_rejects_missing_artifact(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            script = root / 'scripts/ci/check-ent-generated.sh'
            script.parent.mkdir(parents=True)
            shutil.copyfile(REPO / 'scripts/ci/check-ent-generated.sh', script)
            result = subprocess.run(['bash', str(script)], cwd=root, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b'schema.sql missing', result.stderr)


if __name__ == '__main__':
    unittest.main()
