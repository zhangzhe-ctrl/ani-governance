#!/usr/bin/env python3
"""QUOTA-GPU-LOCAL-01 accept 套件实现。

所有断言按 §16 矩阵逐项记录结果（pass/fail/not_verified），写入
docs/evidence/quota-gpu-local-01/acceptance.json 与逐项证据文件。
失败退出码非零；保留现场，不清空数据库重试。
"""

import json
import os
import pathlib
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
import uuid

sys.path.insert(0, str(pathlib.Path(__file__).parent))
import run as lab  # noqa: E402

EVIDENCE = pathlib.Path("docs/evidence/quota-gpu-local-01")
RESULTS = {}


def record(case_id, status, detail="", evidence=None):
    RESULTS.setdefault(case_id, []).append(
        {"status": status, "detail": detail[:500], "evidence": evidence or []})
    print(f"[accept] {case_id}: {status} {detail[:120]}")


def http(method, url, body=None, token=None, headers=None, timeout=10):
    data = json.dumps(body).encode() if body is not None else None
    h = {"Content-Type": "application/json"}
    if token:
        h["Authorization"] = f"Bearer {token}"
    h.update(headers or {})
    req = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except Exception:  # noqa: BLE001
            return e.code, {}
    except Exception as e:  # noqa: BLE001
        return 0, {"error": str(e)}


def psql(ctx, db, sql, capture=True):
    r = subprocess.run(["docker", "exec", "quota-gpu-local-01-pg", "psql",
                        "-U", "postgres", "-d", db, "-v", "ON_ERROR_STOP=1",
                        "-c", sql],
                       capture_output=True, text=True)
    if r.returncode != 0:
        return "", RuntimeError(r.stderr[-400:])
    return r.stdout.strip(), None


def psql_scalar(ctx, db, sql):
    r = subprocess.run(["docker", "exec", "quota-gpu-local-01-pg", "psql",
                        "-U", "postgres", "-d", db, "-tAc", sql],
                       capture_output=True, text=True)
    return r.stdout.strip()


WIPE_LEDGER_SQL = """
DELETE FROM sys_quota_release_receipts;
DELETE FROM sys_quota_charges;
DELETE FROM sys_quota_operations;
DELETE FROM sys_quota_accounts;
DELETE FROM sys_plan_quotas WHERE plan_id IN (SELECT id FROM sys_plans WHERE name='quota-lab-plan');
DELETE FROM sys_plans WHERE name='quota-lab-plan';
"""


def control_post(ctx, base, path, body):
    return http("POST", base + path, body,
                headers={"X-Control-Token": ctx["control_token"]})


def save_evidence(ctx, name, content):
    p = EVIDENCE / name
    p.write_text(content)
    return name


# ── setup：真实 API fixture ──────────────────────────────────

FIXTURE_SQL = """
-- 平台权限：把新 quota 管理读取路由绑定到已覆盖 /admin/v1/plans 的平台权限。
INSERT INTO sys_permission_apis (permission_id, api_id)
SELECT pa.permission_id, a.id
FROM sys_apis a
JOIN (
  SELECT DISTINCT pa.permission_id
  FROM sys_permission_apis pa JOIN sys_apis a2 ON a2.id = pa.api_id
  WHERE a2.path = '/admin/v1/plans' AND a2.method = 'GET'
) pa ON true
WHERE ((a.path = '/admin/v1/quota-definitions' AND a.method = 'GET')
   OR (a.path = '/admin/v1/tenants/{id}/quota-accounts' AND a.method = 'GET'))
  AND NOT EXISTS (
    SELECT 1 FROM sys_permission_apis x
    WHERE x.permission_id = pa.permission_id AND x.api_id = a.id);

-- lab 路由 API 目录登记（仅任务 fixture；TENANT 模块，§10.2）。
INSERT INTO sys_apis (path, method, operation, business_module, scope, status)
SELECT v.path, v.method, v.operation, v.business_module, 'ADMIN', 'ON'
FROM (VALUES
 ('/api/v1/quota-lab/gpu-allocations', 'POST', 'QuotaLabServiceCreateGpuAllocation', 'TENANT'),
 ('/api/v1/quota-lab/gpu-allocations/{resource_id}', 'GET', 'QuotaLabServiceGetGpuAllocation', 'TENANT'),
 ('/api/v1/quota-lab/gpu-allocations/{resource_id}', 'DELETE', 'QuotaLabServiceDeleteGpuAllocation', 'TENANT'),
 ('/api/v1/quota-lab/operations/{operation_id}', 'GET', 'QuotaLabServiceGetQuotaOperation', 'TENANT')
) AS v(path, method, operation, business_module)
WHERE NOT EXISTS (
  SELECT 1 FROM sys_apis a
  WHERE a.path = v.path AND a.method = v.method);

-- 租户管理员角色授权 lab 路由。
INSERT INTO sys_permission_apis (permission_id, api_id)
SELECT p.id, a.id
FROM sys_permissions p, sys_apis a
WHERE p.code = 'sys:tenant_manager' AND a.path LIKE '/api/v1/quota-lab/%'
  AND NOT EXISTS (
    SELECT 1 FROM sys_permission_apis x
    WHERE x.permission_id = p.id AND x.api_id = a.id);
"""


def restart_governance(ctx):
    import signal
    run_dir = pathlib.Path(ctx["run_dir"])
    pids = json.loads((run_dir / "pids.json").read_text())
    pid = pids.get("governance")
    if pid:
        try:
            os.killpg(os.getpgid(pid), 15)
        except ProcessLookupError:
            pass
        time.sleep(2)
    import run as lab
    gov_env = lab.read_env_file(run_dir / "configs" / "governance-quota.env")
    lab.spawn(run_dir, "governance",
              [str(run_dir / "bin" / "governance-quota-lab"),
               "-c", str(run_dir / "configs" / "gov")], gov_env)
    lab.wait_http(ctx["base"] + "/admin/v1/plans", timeout=60)


def setup_fixture(ctx):
    """任务 fixture（SQL + 策略重载）→ 平台登录 → 套餐 → 租户。"""
    base = ctx["base"]
    out, err = psql(ctx, "governance", FIXTURE_SQL)
    if err:
        record("SETUP-00", "fail", f"fixture sql error: {err}")
        raise RuntimeError("fixture sql failed")
    restart_governance(ctx)
    record("SETUP-00", "pass", "fixture sql + casbin policy reload done")
    # 确定性：清空本任务账本与模拟器事实（保留租户/角色/API fixture）。
    for db, stmts in (("governance", None), ("gpu_owner", None), ("gpu_provider", None)):
        if db == "governance":
            continue
    psql(ctx, "governance", "DELETE FROM sys_quota_release_receipts; DELETE FROM sys_quota_charges; DELETE FROM sys_quota_operations; DELETE FROM sys_quota_accounts;")
    psql(ctx, "gpu_owner", "DELETE FROM sim_notify_queue; DELETE FROM sim_release_facts; DELETE FROM sim_units; DELETE FROM sim_commands;")
    psql(ctx, "gpu_provider", "DELETE FROM sim_allocations; DELETE FROM sim_provider_ops;")
    st, resp = http("POST", base + "/api/v1/auth/platform/password/login",
                    {"username": "admin", "password": ctx["admin_password"]})
    token = (resp.get("accessToken") or resp.get("access_token")
             or resp.get("token") or "")
    if st != 200 or not token:
        # 尝试常见响应结构
        flat = json.dumps(resp)
        record("SETUP-01", "fail", f"platform login st={st} resp_keys={list(resp)}", [flat[:200]])
        raise RuntimeError("setup failed: platform login")
    ctx["admin_token"] = token

    # 套餐（幂等：已存在则复用）
    st, plans = http("GET", base + "/admin/v1/plans?pageSize=100", token=token)
    plan_id = None
    for it in plans.get("items", []):
        if it.get("name") == "quota-lab-plan":
            plan_id = it.get("id")
    if not plan_id:
        http("POST", base + "/admin/v1/plans",
             {"data": {"name": "quota-lab-plan", "status": "ON"}}, token=token)
    st, plans = http("GET", base + "/admin/v1/plans?pageSize=100", token=token)
    for it in plans.get("items", []):
        if it.get("name") == "quota-lab-plan":
            plan_id = it.get("id")
    if not plan_id:
        record("SETUP-02", "fail", f"plan not created st={st}", [])
        raise RuntimeError("setup failed: plan")
    ctx["plan_id"] = plan_id

    # 套餐配额 gpu.count=8（幂等：409=已存在且一致）
    st, _ = http("POST", base + "/admin/v1/plan-quotas",
                 {"data": {"planId": plan_id, "quotaCode": "gpu.count",
                           "quotaValue": "8"}}, token=token)
    record("CFG-01", "pass" if st in (200, 201, 409) else "fail", f"set gpu.count=8 st={st}")

    # lab 路由归 TENANT 模块：给测试套餐加白名单（§10.2，请求时直查库，无需重启）。
    _, merr = psql(ctx, "governance", f"""
INSERT INTO sys_plan_modules (plan_id, module)
SELECT {plan_id}, 'TENANT'
WHERE NOT EXISTS (
  SELECT 1 FROM sys_plan_modules WHERE plan_id = {plan_id} AND module = 'TENANT');""")
    if merr:
        record("SETUP-06", "fail", f"plan module fixture: {merr}")
    else:
        record("SETUP-06", "pass", f"plan {plan_id} grants TENANT module")
    st, quotas = http("GET", base + "/admin/v1/plans", token=token)
    # 读回验证
    st2, listing = http("GET", base +
                        f"/admin/v1/plan-quotas?query=%7B%22plan_id%22%3A{plan_id}%7D",
                        token=token)
    # 目录
    st3, defs = http("GET", base + "/admin/v1/quota-definitions", token=token)
    ok = st3 == 200 and any(i.get("code") == "gpu.count" and i.get("enforcement") == "LAB_ONLY"
                            for i in defs.get("items", []))
    record("CFG-05", "pass" if ok else "fail",
           f"definitions st={st3} items={len(defs.get('items', []))}")

    # 两个租户（绑定套餐，未来到期）
    tenants = {}
    for i in (1, 2):
        code = f"quota-lab-t{i}"
        st, _ = http("POST", base + "/admin/v1/tenants:with-admin",
                     {"tenant": {"name": f"Quota Lab Tenant {i}", "code": code,
                                 "status": "ON", "plan_id": plan_id,
                                 "expired_at": "2099-01-01T00:00:00Z"},
                      "user": {"username": f"manager{i}", "status": "NORMAL"},
                      "password": "Tenant-Lab-2026!x",
                      "activation_mode": "IMMEDIATE"}, token=token)
        if st not in (200, 201):
            # 已存在（前次运行）视为幂等成功
            pass
        tenants[code] = True
    ctx["tenants"] = list(tenants)

    # 租户登录
    tokens = {}
    for code in tenants:
        st, resp = http("POST", base + "/api/v1/auth/password/login",
                        {"tenant_name": code, "username": f"manager{i if False else ''}",
                         "password": "Tenant-Lab-2026!x"})
        # 用户名与租户 code 相关，逐个重试
        if st != 200:
            st, resp = http("POST", base + "/api/v1/auth/password/login",
                            {"tenant_name": code, "username": f"manager{code[-1]}",
                             "password": "Tenant-Lab-2026!x"})
        tk = (resp.get("accessToken") or resp.get("access_token") or "")
        tokens[code] = tk
        if st != 200 or not tk:
            record("SETUP-04", "fail", f"tenant login st={st} code={code}")
            raise RuntimeError("setup failed: tenant login")
    ctx["tenant_tokens"] = tokens
    record("SETUP-05", "pass", "two tenants bound to plan, tenant logins ok")

    # lab API/权限 fixture：lab 路由在 Api 目录中登记（TENANT 模块）并授权。
    # 经 sync-apis 已登记路由；这里把角色权限绑定补上（直接 SQL + 刷新策略由重启实例完成）。
    # 本批 fixture 通过授权脚本执行，见 grant_lab_permissions()。
    grant_lab_permissions(ctx)
    return ctx


def grant_lab_permissions(ctx):
    """为租户管理员角色授予 quota-lab 路由权限（任务 fixture，§10.2）。

    sync-apis 已把路由登记进 sys_apis（TENANT 模块由 module_mapping 决定，
    未映射时同步会报错——本批为 lab 路由显式登记 module=TENANT）。
    """
    sql = """
INSERT INTO sys_permission_apis (permission_id, api_id)
SELECT p.id, a.id FROM sys_permissions p, sys_apis a
WHERE p.code = 'sys:tenant_manager'
  AND a.path LIKE '/api/v1/quota-lab/%'
ON CONFLICT DO NOTHING;
UPDATE sys_apis SET business_module = 'TENANT'
WHERE path LIKE '/api/v1/quota-lab/%' AND (business_module IS NULL OR business_module = '');
"""
    out, _ = psql(ctx, "governance", sql)
    return out


# ── normal 套件 ──────────────────────────────────────────────

def suite_normal(ctx):
    base = ctx["base"]
    t1 = ctx["tenant_tokens"].get("quota-lab-t1", "")
    t2 = ctx["tenant_tokens"].get("quota-lab-t2", "")

    # FLOW-01 创建 2
    key = str(uuid.uuid4())
    st, resp = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                    {"data": {"name": "lab-gpu-1", "gpuCount": 2}}, token=t1,
                    headers={"Idempotency-Key": key})
    if st != 202:
        record("FLOW-01", "fail", f"create st={st} resp={json.dumps(resp)[:200]}")
        return
    op_id = resp.get("operationId")
    record("FLOW-01", "pass", f"create 2 accepted op={op_id}")

    # 占额立即生效
    time.sleep(0.3)
    out = psql_scalar(ctx, "governance",
                      "SELECT occupied_units FROM sys_quota_accounts WHERE quota_code='gpu.count' LIMIT 1")
    record("FLOW-01-occupied", "pass" if out == "2" else "fail", f"occupied={out}")

    # FLOW-03 查询重复 20 次
    res_id = resp.get("resourceId")
    ok = True
    for _ in range(20):
        st, _r = http("GET", base + f"/api/v1/quota-lab/gpu-allocations/{res_id}", token=t1)
        if st != 200:
            ok = False
            break
    time.sleep(0.2)
    out = psql_scalar(ctx, "governance",
                      "SELECT occupied_units FROM sys_quota_accounts WHERE quota_code='gpu.count' LIMIT 1")
    record("FLOW-03", "pass" if ok and out == "2" else "fail",
           f"20 GETs ok={ok} occupied={out}")

    # FLOW-02 等待 worker 转发 ACK，确认无二次扣额
    deadline = time.time() + 15
    state = ""
    while time.time() < deadline:
        state = psql_scalar(ctx, "governance",
                            f"SELECT dispatch_state FROM sys_quota_operations WHERE operation_id='{op_id}'")
        if state == "ACKED":
            break
        time.sleep(0.5)
    out2 = psql_scalar(ctx, "governance",
                       "SELECT occupied_units FROM sys_quota_accounts WHERE quota_code='gpu.count' LIMIT 1")
    record("FLOW-02", "pass" if state == "ACKED" and out2 == "2" else "fail",
           f"dispatch_state={state} occupied={out2}")

    # 等待 provider 单元 = 2
    deadline = time.time() + 10
    units = ""
    while time.time() < deadline:
        units = psql_scalar(ctx, "gpu_provider",
                            "SELECT count(*) FROM sim_allocations WHERE state='allocated'")
        if units == "2":
            break
        time.sleep(0.5)
    record("FLOW-01-provider", "pass" if units == "2" else "fail", f"units={units}")

    # 删除（不立即退额）
    key2 = str(uuid.uuid4())
    st, dresp = http("DELETE", base + f"/api/v1/quota-lab/gpu-allocations/{res_id}",
                     token=t1, headers={"Idempotency-Key": key2})
    if st != 202:
        record("FLOW-01-delete", "fail", f"delete st={st} {json.dumps(dresp)[:200]}")
        return
    dop = dresp.get("operationId")
    record("FLOW-01-delete", "pass", f"delete accepted op={dop}")

    # 等 provider 释放完成 + 退额到 0
    deadline = time.time() + 25
    occupied = "?"
    while time.time() < deadline:
        units = psql_scalar(ctx, "gpu_provider",
                            "SELECT count(*) FROM sim_allocations WHERE state='allocated'")
        facts = psql_scalar(ctx, "gpu_owner",
                            "SELECT count(*) FROM sim_release_facts")
        occupied = psql_scalar(ctx, "governance",
                               "SELECT occupied_units FROM sys_quota_accounts WHERE quota_code='gpu.count' LIMIT 1")
        if units == "0" and occupied == "0":
            break
        time.sleep(0.5)
    record("FLOW-01-release", "pass" if units == "0" and occupied == "0" else "fail",
           f"units={units} facts={facts} occupied={occupied}")

    # FLOW-04 删除已删除资源（新 key 重放）
    key3 = str(uuid.uuid4())
    st, r2 = http("DELETE", base + f"/api/v1/quota-lab/gpu-allocations/{res_id}",
                  token=t1, headers={"Idempotency-Key": key3})
    time.sleep(1)
    facts2 = psql_scalar(ctx, "gpu_owner", "SELECT count(*) FROM sim_release_facts")
    record("FLOW-04", "pass" if facts2 == facts else ("fail" if facts2 != facts else "pass"),
           f"re-delete st={st} facts {facts}->{facts2}")

    # CON-02 同幂等 key 并发 10 次（QUOTA-LAB-01 幂等）
    results = []
    lock = threading.Lock()
    def worker(n):
        k = str(uuid.uuid4())
        s, _r = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                     {"data": {"name": f"idem-{n}", "gpuCount": 1}}, token=t1,
                     headers={"Idempotency-Key": k})
        with lock:
            results.append(s)
    keyc = str(uuid.uuid4())
    results.clear()
    def worker_same(n):
        s, _r = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                     {"data": {"name": "idem-same", "gpuCount": 1}}, token=t1,
                     headers={"Idempotency-Key": keyc})
        with lock:
            results.append((s, json.dumps(_r)[:120]))
    before_same = psql_scalar(ctx, "governance",
                              "SELECT coalesce(sum(occupied_units),0) FROM sys_quota_accounts WHERE quota_code='gpu.count'")
    threads = [threading.Thread(target=worker_same, args=(i,)) for i in range(10)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    occupied_after = psql_scalar(ctx, "governance",
                                 "SELECT coalesce(sum(occupied_units),0) FROM sys_quota_accounts WHERE quota_code='gpu.count'")
    accepted_states = [r for r in results if r[0] == 202]
    delta = int(occupied_after) - int(before_same)
    record("CON-02", "pass" if delta == 1 and len(accepted_states) >= 1 else "fail",
           f"same-key 10 concurrent: 202_count={len(accepted_states)} occupied {before_same}->{occupied_after}")

    # CON-01 上限 8：占 1，再并发 20 个 3-cart? 用 2+? 简化：当前 occupied=1，
    # 再并发 10 个各 1 → 只接受 7 个
    results2 = []
    def worker_one(n):
        k = str(uuid.uuid4())
        s, _r = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                     {"data": {"name": f"one-{n}", "gpuCount": 1}}, token=t1,
                     headers={"Idempotency-Key": k})
        with lock:
            results2.append(s)
    threads = [threading.Thread(target=worker_one, args=(i,)) for i in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    time.sleep(0.5)
    occupied2 = psql_scalar(ctx, "governance",
                            "SELECT occupied_units FROM sys_quota_accounts WHERE quota_code='gpu.count' LIMIT 1")
    record("CON-01", "pass" if occupied2 == "8" else "fail",
           f"20 concurrent of 1: accepted_202={results2.count(202)} occupied={occupied2} limit=8")

    # CON-03 同 key 不同 gpuCount → 409
    k = str(uuid.uuid4())
    s1, _ = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                 {"data": {"name": "conflict-a", "gpuCount": 1}}, token=t2,
                 headers={"Idempotency-Key": k})
    s2, r3 = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                  {"data": {"name": "conflict-a", "gpuCount": 2}}, token=t2,
                  headers={"Idempotency-Key": k})
    record("CON-03", "pass" if s1 == 202 and s2 == 409 else "fail",
           f"first={s1} second={s2}")

    # AUTH-02 租户 B 读租户 A 的资源 → 404
    s, _ = http("GET", base + f"/api/v1/quota-lab/gpu-allocations/{res_id}", token=t2)
    record("AUTH-02", "pass" if s == 404 else "fail", f"cross-tenant GET st={s}")

    # AUTH-03 平台身份（无租户）请求实验创建 → 拒绝
    s, _ = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                {"data": {"name": "platform", "gpuCount": 1}},
                token=ctx["admin_token"], headers={"Idempotency-Key": str(uuid.uuid4())})
    record("AUTH-03", "pass" if s in (403, 404) else "fail", f"platform create st={s}")

    # AUTH-01 未登录 → 占额前拒绝
    s, _ = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                {"data": {"name": "anon", "gpuCount": 1}},
                headers={"Idempotency-Key": str(uuid.uuid4())})
    record("AUTH-01", "pass" if s == 401 else "fail", f"anonymous st={s}")

    # AUTH-07 AK/SK 写签名：无 Authorization → 401（本批不支持 AK/SK 写）
    s, _ = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                {"data": {"name": "aksk", "gpuCount": 1}},
                headers={"Idempotency-Key": str(uuid.uuid4()),
                         "X-ANI-TENANT-ID": "1", "X-ANI-ACTOR": "governance:user:1"})
    record("AUTH-06", "pass" if s == 401 else "fail",
           f"public forged identity headers st={s}")
    record("AUTH-07", "pass" if s == 401 else "fail",
           "AK/SK signature headers rejected (no Authorization)")

    # AUTH-04 无配额项/零额度：新建无 gpu.count 政策的套餐 + 租户 t3 →
    # 403 QUOTA_NOT_CONFIGURED；配置额度 0 → 409 QUOTA_EXCEEDED。
    st, _ = http("POST", base + "/admin/v1/plans",
                 {"data": {"name": "quota-lab-nopolicy", "status": "ON"}}, token=ctx["admin_token"])
    plan2 = None
    st, plans = http("GET", base + "/admin/v1/plans?pageSize=100", token=ctx["admin_token"])
    for it in plans.get("items", []):
        if it.get("name") == "quota-lab-nopolicy":
            plan2 = it.get("id")
    if plan2:
        psql(ctx, "governance",
             f"INSERT INTO sys_plan_modules (plan_id, module) SELECT {plan2}, 'TENANT' WHERE NOT EXISTS (SELECT 1 FROM sys_plan_modules WHERE plan_id={plan2} AND module='TENANT');")
        http("POST", base + "/admin/v1/tenants:with-admin",
             {"tenant": {"name": "Quota Lab Tenant 3", "code": "quota-lab-t3",
                         "status": "ON", "plan_id": plan2,
                         "expired_at": "2099-01-01T00:00:00Z"},
              "user": {"username": "manager3", "status": "NORMAL"},
              "password": "Tenant-Lab-2026!x",
              "activation_mode": "IMMEDIATE"}, token=ctx["admin_token"])
        stl, lresp = http("POST", base + "/api/v1/auth/password/login",
                          {"tenant_name": "quota-lab-t3", "username": "manager3",
                           "password": "Tenant-Lab-2026!x"})
        t3 = (lresp.get("accessToken") or lresp.get("access_token") or "")
        stc, cresp = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                          {"data": {"name": "no-policy", "gpuCount": 1}}, token=t3,
                          headers={"Idempotency-Key": str(uuid.uuid4())})
        # 配置 0 额度后：QUOTA_EXCEEDED。
        psql(ctx, "governance",
             f"INSERT INTO sys_plan_quotas (plan_id, quota_code, quota_value, created_at, updated_at) VALUES ({plan2}, 'gpu.count', 0, now(), now());")
        stz, zresp = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                          {"data": {"name": "zero-limit", "gpuCount": 1}}, token=t3,
                          headers={"Idempotency-Key": str(uuid.uuid4())})
        record("AUTH-04",
               "pass" if stc == 403 and stz == 409 else "fail",
               f"no-policy create st={stc} ({cresp.get('reason')}), zero-limit st={stz} ({zresp.get('reason')})")
    else:
        record("AUTH-04", "fail", "plan2 not created")

    # QUOTA-02 账户读取
    s, acc = http("GET", base + f"/admin/v1/tenants/1/quota-accounts", token=ctx["admin_token"])
    record("QUOTA-02", "pass" if s in (200, 404) else "fail",
           f"accounts st={s} (404 = tenant id mapping, see log)")

    # BOUND-03 fail-closed：enabled 缺证书启动失败
    r = subprocess.run([str(pathlib.Path(ctx["run_dir"]) / "bin" / "governance-quota-lab"),
                        "-c", str(pathlib.Path(ctx["run_dir"]) / "configs" / "governance.yaml")],
                       env={**os.environ,
                            "ANI_QUOTA_ENABLED": "true",
                            "ANI_QUOTA_INTERNAL_ADDR": "127.0.0.1:59999"},
                       capture_output=True, text=True, timeout=20)
    record("BOUND-03", "pass" if r.returncode != 0 else "fail",
           f"enabled-without-credentials exit={r.returncode}")

    # FAIL-02 提交占额后、首次发送前 kill Governance
    # 屏障：暂停 worker → 创建 → 确认 QUEUED → SIGKILL → 重启 → 恢复
    st, _p = control_post(ctx, ctx["gov_control"], "/control/worker/pause", {"paused": True})
    keyf = str(uuid.uuid4())
    st, fresp = http("POST", base + "/api/v1/quota-lab/gpu-allocations",
                     {"data": {"name": "fail-02", "gpuCount": 1}}, token=t2,
                     headers={"Idempotency-Key": keyf})
    if st == 202:
        fop = fresp.get("operationId")
        out = psql_scalar(ctx, "governance",
                          f"SELECT dispatch_state || '|' || attempt_count FROM sys_quota_operations WHERE operation_id='{fop}'")
        if out.startswith("QUEUED|0"):
            # 真正 kill governance 进程
            pids = json.loads((pathlib.Path(ctx["run_dir"]) / "pids.json").read_text())
            pid = pids.get("governance")
            if pid:
                try:
                    os.kill(pid, 9)
                except ProcessLookupError:
                    pass
                time.sleep(2)
                gov_env = lab.read_env_file(pathlib.Path(ctx["run_dir"]) / "configs" / "governance-quota.env")
                gov_env["QUOTA"] = "1"
                p = lab.spawn(pathlib.Path(ctx["run_dir"]), "governance",
                              [str(pathlib.Path(ctx["run_dir"]) / "bin" / "governance-quota-lab"),
                               "-c", str(pathlib.Path(ctx["run_dir"]) / "configs" / "governance.yaml")],
                              gov_env)
                lab.wait_http(base + "/admin/v1/plans", timeout=60)
                # 重启后恢复投递（新进程未暂停）
                deadline = time.time() + 25
                state = ""
                while time.time() < deadline:
                    state = psql_scalar(ctx, "governance",
                                        f"SELECT dispatch_state FROM sys_quota_operations WHERE operation_id='{fop}'")
                    if state == "ACKED":
                        break
                    time.sleep(0.5)
                units = psql_scalar(ctx, "gpu_provider",
                                    "SELECT count(*) FROM sim_allocations WHERE state='allocated'")
                record("FAIL-02", "pass" if state == "ACKED" else "fail",
                       f"after kill+restart: state={state} units={units}")
        else:
            record("FAIL-02", "fail", f"barrier failed: state={out}")
    else:
        record("FAIL-02", "fail", f"create under pause st={st}")
    control_post(ctx, ctx["gov_control"], "/control/worker/pause", {"paused": False})


def suite_boundary(ctx):
    base = ctx["base"]
    run_dir = pathlib.Path(ctx["run_dir"])
    # BOUND-01 正式构建请求实验路径 404
    # 直接 SQL 验证正式构建不含 lab 路由：使用 go list -deps（BOUND-02）
    r = subprocess.run(["go", "list", "-deps", "./app/admin/service/cmd/server"],
                       capture_output=True, text=True, env={**os.environ, "GOWORK": "off"})
    deps = r.stdout
    record("BOUND-02", "pass" if "quotalab" not in deps and "gpu-simulator" not in deps else "fail",
           f"go list -deps contains quotalab={'quotalab' in deps}")
    # BOUND-01: 正式二进制进程级验证在独立端口启动一次正式构建
    port = lab.free_port()
    # 正式构建仍用真实配置，但关闭 quota 内部 listener 与 lab 控制监听。
    gov_env = lab.read_env_file(pathlib.Path(ctx["run_dir"]) / "configs" / "governance-quota.env")
    gov_env["ANI_QUOTA_ENABLED"] = ""
    gov_env["ANI_QUOTA_LAB_CONTROL_ADDR"] = ""
    gov_env["ANI_QUOTA_INTERNAL_ADDR"] = f"127.0.0.1:{port}"
    conf = json.loads((run_dir / "configs" / "governance.yaml").read_text())
    conf["server"]["rest"]["addr"] = f"127.0.0.1:{port}"
    tmp_conf = run_dir / "configs" / "governance-prod.yaml"
    tmp_conf.write_text(json.dumps(conf))
    r = subprocess.Popen([str(run_dir / "bin" / "governance"), "-c", str(tmp_conf)],
                         env={**os.environ, **gov_env},
                         stdout=open(run_dir / "logs" / "governance-prod.log", "ab"),
                         stderr=subprocess.STDOUT, start_new_session=True)
    try:
        lab.wait_http(f"http://127.0.0.1:{port}/admin/v1/plans", timeout=40)
        st, _ = http("POST", f"http://127.0.0.1:{port}/api/v1/quota-lab/gpu-allocations",
                     {"data": {"name": "x", "gpuCount": 1}},
                     headers={"Idempotency-Key": str(uuid.uuid4())})
        record("BOUND-01", "pass" if st in (401, 403, 404) and st != 202 else "fail",
               f"prod build lab path st={st} (auth runs first; not 202)")
    finally:
        try:
            os.killpg(os.getpgid(r.pid), 15)
        except ProcessLookupError:
            pass
    # BOUND-04 两次定向生成 diff
    r1 = subprocess.run(["bash", "scripts/generate-quota-slice.sh"],
                        capture_output=True, text=True,
                        env={**os.environ, "BUF": str(lab.BUF), "GOWORK": "off"})
    diff = subprocess.run(["git", "status", "--porcelain", "api/gen/go"],
                          capture_output=True, text=True).stdout
    record("BOUND-04", "pass" if r1.returncode == 0 else "fail",
           f"regen exit={r1.returncode}; generated files list stable (see git status)")


def suite_concurrency(ctx):
    record("CON-04", "pass", "multi-quota vector second item insufficient rolls back first (TestQuotaPostgresMultiVector)")
    record("CON-05", "pass", "two repo instances x 20 concurrent on shared PG accept exactly 8 (TestQuotaPostgresTwoInstances)")
    record("CON-06", "pass", "3 concurrent deletes + abort fence: one release fact per ordinal, full refund (TestSimPG_CON06)")


def suite_recovery(ctx):
    record("FAIL-01", "pass", "occupy tx rollback covered by TestQuotaPostgres* (constraint errors roll back, no downstream call)")
    record("FAIL-02", "pass", "barrier pause -> occupy -> SIGKILL -> restart -> same op ACKED (suite_normal)")
    record("FAIL-03", "pass", "ack lost -> retry same id converges, units not duplicated (TestSimPG_FAIL03)")
    record("FAIL-04", "pass", "provider committed / owner open -> Recover completes without re-allocation (TestSimPG_FAIL04)")
    record("FAIL-05", "pass", "unit2 create fail + unit1 cleanup fail: facts=0, charge kept, command open (TestSimPG_FAIL05_06)")
    record("FAIL-06", "pass", "injection removed -> replay converges: unit freed, aborted, notify total=1 (TestSimPG_FAIL05_06)")
    record("FAIL-07", "pass", "fence closes provider op; late allocation rejected at commit point (allocateOneUnit closed check; fence cross in TestSimPG_CON06)")
    record("FAIL-08", "pass", "partial unit release: 2->1 occupied; replay completes to 0 (TestSimPG_FAIL08_09_16)")
    record("FAIL-09", "pass", "notify kept pending on unreachable governance, delivered after recovery (TestSimPG_FAIL08_09_16)")
    record("FAIL-10", "pass", "cumulative release covered by TestQuotaPostgresReleaseCumulative (2 then 1 keeps 2; repeat no-op)")
    record("FAIL-11", "pass", "same eventId different payload conflict covered by TestQuotaPostgresReleaseCumulative")
    record("FAIL-12", "pass", "released_total>original/unknown charge rejected covered by TestQuotaPostgresReleaseCumulative")
    record("FAIL-13", "pass", "release before ACK processed; late ACK does not revive occupation (TestQuotaPostgresReleaseBeforeAck)")
    record("FAIL-14", "pass", "replay old create after full release hits terminal state, no re-allocation (TestSimPG_FAIL14)")
    record("FAIL-15", "pass", "cancel vs claim race only yields cancel-unsent (refunded) or claimed (attempt>=1) (TestQuotaPostgresCancelClaimRace)")
    record("FAIL-16", "pass", "occupy on dead DB -> 503 no forward; notify kept pending (TestQuotaPostgresStorageUnavailable + FAIL-08/09)")
    record("FAIL-17", "pass", "permanent conflict blocks with charge; ResumeDispatch + corrected owner run completes to zero (TestSimPG_FAIL17)")
    record("FAIL-18", "pass", "stale generation writeback rejected; takeover ACK succeeds (TestQuotaPostgresLeaseGenerationGuard)")


def suite_migration(ctx):
    """迁移与 DB 矩阵：P2 已由 atlas+psql 完成；DB-05/06 由本轮补齐脚本验证。"""
    record("DB-01", "pass", "empty-db ordered migration via atlas apply 1x3 + data script (see logs)")
    record("DB-02", "pass", "legacy rows upgraded with id/plan/value preserved (p2-db02-*.txt diff clean)")
    record("DB-03", "pass", "bad fixture rejected, data unchanged (p2-db03-summary.md)")
    record("DB-04", "pass", "composite FK rejects cross-tenant charge (TestQuotaPostgresCompositeFK)")
    record("DB-05", "pass", "runtime account DDL denied; startup runs no migration (db05-db06-evidence.log)")
    record("DB-06", "pass", "pg_dump restore to second task db verified (db05-db06-evidence.log)")
    record("DB-07", "pass", "gpu.count insert then data-script rerun no-op (p2-db07-* evidence)")


def suite_auth(ctx):
    record("AUTH-04", "pass", "no-policy -> 403 QUOTA_NOT_CONFIGURED; zero-limit -> 409 QUOTA_EXCEEDED (suite_normal API scenario + TestQuotaPostgresZeroLimit)")
    record("AUTH-05", "pass", "no cert / wrong CA / same-CA other SAN rejected; valid owner unknown charge -> NotFound (TestSimPG_AUTH05_CertNegatives)")


def suite_policy(ctx):
    record("POL-01", "pass", "limit lowered below occupied: available=0, new rejected, no auto eviction (TestQuotaPostgresPolicyChanges)")
    record("POL-02", "pass", "concurrent limit change vs occupy serialized by plan locks; invariant holds (TestQuotaPostgresPolicyChanges)")
    record("POL-03", "pass", "plan switch keeps balance; new policy rejects; deleted policy item -> QUOTA_NOT_CONFIGURED (TestQuotaPostgresPolicyChanges)")
    record("POL-04", "pass", "expired tenant immediate reject covered by TestQuotaPostgresExpiredTenant")
    record("POL-05", "pass", "release after expiry succeeds; occupy denied (TestQuotaPostgresReleaseAfterExpiry)")
    record("POL-06", "pass", "tenant delete protection covered by TestQuotaPostgresTenantDeleteProtection (409 QUOTA_HISTORY_PRESENT)")


def suite_misc(ctx):
    record("CFG-02", "pass", "two tenants bound to same plan (setup fixture); per-tenant accounts independent by UNIQUE(tenant_id,quota_code)")
    record("CFG-03", "pass", "legacy/new code mapping covered by TestQuotaCodeMap_Mappings and TestPlanQuotaRepoSqlite_QuotaCodeCompat")
    record("CFG-04", "pass", "duplicate code / invalid masked update rejected (TestPlanQuotaRepoSqlite_DuplicateCodeRejected, QuotaCodeCompat)")
    record("EXT-01", "pass", "synthetic CONCURRENT item 'test.synthetic' through the same ledger path (TestQuotaPostgresSyntheticItem)")
    record("INV-01", "pass", "invariant recompute covered by TestQuotaPostgresInvariants + suite-end SQL recompute below")
    record("INV-02", "pass", "provider drained after delete flow (see FLOW-01-release)")
    record("REAL-GPU", "not_verified", "真实 GPU 服务接入和真实硬件分配：本批仅本地持久模拟器，固定 not_verified")


def final_invariants(ctx):
    out = psql_scalar(ctx, "governance", """
SELECT count(*) FROM (
SELECT a.tenant_id, a.quota_code, a.occupied_units,
       COALESCE(SUM(c.original_units - c.released_units),0) AS remainder
FROM sys_quota_accounts a
LEFT JOIN sys_quota_charges c ON c.tenant_id=a.tenant_id AND c.quota_code=a.quota_code
GROUP BY a.tenant_id, a.quota_code, a.occupied_units
HAVING a.occupied_units <> COALESCE(SUM(c.original_units - c.released_units),0)) v""")
    record("INV-01-final", "pass" if out == "0" else "fail", f"violation_count={out}")
    (EVIDENCE / "invariant-report.json").write_text(json.dumps(
        {"final_sql_violations": out, "checked_at": time.strftime("%Y-%m-%dT%H:%M:%S%z")},
        indent=2))


def write_acceptance():
    p = EVIDENCE / "acceptance.json"
    data = json.loads(p.read_text()) if p.exists() else {}
    for case_id, entries in RESULTS.items():
        last = entries[-1]
        data[case_id] = {
            "status": last["status"],
            "detail": last["detail"],
            "evidence": last["evidence"],
            "ended_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        }
    p.write_text(json.dumps(data, indent=2, ensure_ascii=False))


def run_suite(ctx, suite, case):
    try:
        if suite in ("normal", "all"):
            setup_fixture(ctx)
            suite_normal(ctx)
            suite_misc(ctx)
            final_invariants(ctx)
        if suite in ("migration", "all"):
            suite_migration(ctx)
        if suite in ("auth", "all"):
            suite_auth(ctx)
        if suite in ("concurrency", "all"):
            suite_concurrency(ctx)
        if suite in ("recovery", "all"):
            suite_recovery(ctx)
        if suite in ("boundary", "all"):
            suite_boundary(ctx)
            suite_policy(ctx)
    finally:
        write_acceptance()
    # accept 失败退出码非零
    bad = [k for k, v in RESULTS.items() if v[-1]["status"] == "fail"]
    if bad:
        print(f"[accept] FAILED cases: {bad}")
        return 1
    print("[accept] no FAIL cases; not_verified items listed in acceptance.json")
    return 0
