-- QUOTA-GPU-LOCAL-01 版本数据脚本：插入固定配额目录，检查并回填旧行。
-- 执行顺序：migrations/20260922190000_quota_expand.sql → 本脚本 → migrations/20260922190100_quota_constraints.sql。
-- 语义（计划 §12.2）：
--   * 任何坏数据整笔失败并输出脱敏主键清单，不合并、不删除问题行；
--   * 正常旧行精确映射，保留 ID、plan_id、数量与审计时间，只回填 quota_code 为空的合法旧行；
--   * 重复执行校验一致并 no-op，不覆盖已存在的不同定义；
--   * gpu.count 合法新行（quota_type=NULL）不得因缺少旧枚举被拒绝。

BEGIN;

DO $$
DECLARE
  bad_count bigint;
  bad_rows text;
BEGIN

-- ── 一、旧行（quota_code IS NULL）预检 ──────────────────────────────

-- 1. plan_id / quota_type / quota_value 为 NULL 的旧行
SELECT count(*), coalesce(string_agg('id=' || id, ', '), '') INTO bad_count, bad_rows
FROM sys_plan_quotas
WHERE quota_code IS NULL AND (plan_id IS NULL OR quota_type IS NULL OR quota_value IS NULL);
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: rows with NULL plan_id/quota_type/quota_value: %', bad_rows;
END IF;

-- 2. 未知 quota_type（不在 USER_LIMIT/STORAGE/API_CALL 内）
SELECT count(*), coalesce(string_agg('id=' || id, ', '), '') INTO bad_count, bad_rows
FROM sys_plan_quotas
WHERE quota_code IS NULL AND quota_type NOT IN ('USER_LIMIT', 'STORAGE', 'API_CALL');
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: unknown quota_type rows: %', bad_rows;
END IF;

-- 3. 同套餐同类型重复
SELECT count(*), coalesce(string_agg('plan_id=' || plan_id || ' type=' || quota_type, ', '), '') INTO bad_count, bad_rows
FROM (
  SELECT plan_id, quota_type, count(*) AS c
  FROM sys_plan_quotas
  WHERE quota_code IS NULL
  GROUP BY plan_id, quota_type
  HAVING count(*) > 1
) dup;
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: duplicate (plan_id, quota_type) rows: %', bad_rows;
END IF;

-- 4. 数量负数/越界（uint64 合同上限 9223372036854775807）
SELECT count(*), coalesce(string_agg('id=' || id, ', '), '') INTO bad_count, bad_rows
FROM sys_plan_quotas
WHERE quota_code IS NULL AND (quota_value < 0 OR quota_value > 9223372036854775807);
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: quota_value out of range rows: %', bad_rows;
END IF;

-- 5. 不存在的 plan 引用
SELECT count(*), coalesce(string_agg('id=' || id, ', '), '') INTO bad_count, bad_rows
FROM sys_plan_quotas pq
WHERE pq.quota_code IS NULL AND NOT EXISTS (SELECT 1 FROM sys_plans p WHERE p.id = pq.plan_id);
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: rows referencing missing plan: %', bad_rows;
END IF;

-- ── 二、目录：插入固定四项；重复执行校验一致并 no-op ────────────────

INSERT INTO sys_quota_definitions (code, display_name, unit, accounting_kind, created_at)
SELECT v.code, v.display_name, v.unit, v.kind, now()
FROM (VALUES
  ('user.count',   '用户数上限', 'user',    'CONCURRENT'),
  ('storage.bytes','存储空间',   'byte',    'CONCURRENT'),
  ('api.calls',    'API 调用量', 'request', 'COUNTER'),
  ('gpu.count',    'GPU 占用',   'gpu',     'CONCURRENT')
) AS v(code, display_name, unit, kind)
WHERE NOT EXISTS (SELECT 1 FROM sys_quota_definitions d WHERE d.code = v.code);

-- 已存在的定义必须与本脚本一致（不覆盖已存在的不同定义）。
SELECT count(*) INTO bad_count
FROM (VALUES
  ('user.count',   '用户数上限', 'user',    'CONCURRENT'),
  ('storage.bytes','存储空间',   'byte',    'CONCURRENT'),
  ('api.calls',    'API 调用量', 'request', 'COUNTER'),
  ('gpu.count',    'GPU 占用',   'gpu',     'CONCURRENT')
) AS v(code, display_name, unit, kind)
JOIN sys_quota_definitions d ON d.code = v.code
WHERE d.display_name <> v.display_name OR d.unit <> v.unit OR d.accounting_kind <> v.kind;
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota catalog mismatch: % existing definition(s) differ from this script', bad_count;
END IF;

-- ── 三、精确回填：旧三项映射；未知类型已在预检拒绝 ─────────────────

UPDATE sys_plan_quotas SET quota_code = 'user.count'
WHERE quota_code IS NULL AND quota_type = 'USER_LIMIT';
UPDATE sys_plan_quotas SET quota_code = 'storage.bytes'
WHERE quota_code IS NULL AND quota_type = 'STORAGE';
UPDATE sys_plan_quotas SET quota_code = 'api.calls'
WHERE quota_code IS NULL AND quota_type = 'API_CALL';

-- ── 四、已有 quota_code 的行：校验目录存在与合法 ───────────────────

-- 4.1 目录必须存在
SELECT count(*), coalesce(string_agg('id=' || id, ', '), '') INTO bad_count, bad_rows
FROM sys_plan_quotas pq
WHERE NOT EXISTS (SELECT 1 FROM sys_quota_definitions d WHERE d.code = pq.quota_code);
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: rows with unknown quota_code: %', bad_rows;
END IF;

-- 4.2 旧三项若提供 quota_type 必须一致（gpu.count 等 quota_type=NULL 合法）
SELECT count(*), coalesce(string_agg('id=' || pq.id, ', '), '') INTO bad_count, bad_rows
FROM sys_plan_quotas pq
JOIN sys_quota_definitions d ON d.code = pq.quota_code
WHERE d.code IN ('user.count', 'storage.bytes', 'api.calls')
  AND pq.quota_type IS NOT NULL
  AND (  (d.code = 'user.count'   AND pq.quota_type <> 'USER_LIMIT')
      OR (d.code = 'storage.bytes' AND pq.quota_type <> 'STORAGE')
      OR (d.code = 'api.calls'     AND pq.quota_type <> 'API_CALL'));
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: quota_type inconsistent with quota_code rows: %', bad_rows;
END IF;

-- 4.3 已回填/新建行按 code 唯一
SELECT count(*) INTO bad_count FROM (
  SELECT plan_id, quota_code FROM sys_plan_quotas
  WHERE quota_code IS NOT NULL
  GROUP BY plan_id, quota_code HAVING count(*) > 1
) dup;
IF bad_count > 0 THEN
  RAISE EXCEPTION 'quota backfill precheck failed: duplicate (plan_id, quota_code) rows: %', bad_count;
END IF;

END $$;

COMMIT;
