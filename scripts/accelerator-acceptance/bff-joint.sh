#!/usr/bin/env bash
set -euo pipefail
# Run only on the authorized isolated Fedora workspace. Inputs are credential
# file paths; no certificate key or DSN content is printed into evidence.
task_root=${GOV_ACC_TASK_ROOT:?set the isolated task root}
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
export GOV_ACC_JOINT_DSN_FILE="$task_root/task/joint-b/gov-dsn"
export GOV_ACC_JOINT_ADMIN_DSN_FILE="$task_root/task/joint-b/gov-admin-dsn"
export GOV_ACC_JOINT_REDIS_ADDR=${GOV_ACC_JOINT_REDIS_ADDR:-127.0.0.1:19382}
export GOV_ACC_JOINT_SEED_FILE="$task_root/joint-b/seed.json"
export GOV_ACC_JOINT_USAGE_REF_FILE=${GOV_ACC_JOINT_USAGE_REF_FILE:-"$task_root/joint-b/gov-usage-ref.json"}
export ANI_ACCELERATOR_ADDR=127.0.0.1:29092
export ANI_ACCELERATOR_SERVER_NAME=ani-accelerator
export ANI_ACCELERATOR_CA="$task_root/joint-b/ca.pem"
export ANI_ACCELERATOR_CERT="$task_root/joint-b/gov.pem"
export ANI_ACCELERATOR_KEY="$task_root/joint-b/gov.key"
test -s "$GOV_ACC_JOINT_DSN_FILE"
test -s "$GOV_ACC_JOINT_ADMIN_DSN_FILE"
test -s "$GOV_ACC_JOINT_SEED_FILE"
go test ./app/admin/service/internal/service -run '^TestAcceleratorJointHTTP$' -count=1 -v
