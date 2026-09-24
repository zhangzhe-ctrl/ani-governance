-- name: GetPlanQuota :one
SELECT * FROM sys_plan_quotas WHERE id=$1;
-- name: ListPlanQuotas :many
WITH ranked AS (
 SELECT p.*,
 dense_rank() OVER (ORDER BY id) rank_id,
 dense_rank() OVER (ORDER BY plan_id) rank_plan_id,
 dense_rank() OVER (ORDER BY quota_code) rank_quota_code,
 dense_rank() OVER (ORDER BY quota_type) rank_quota_type,
 dense_rank() OVER (ORDER BY quota_value) rank_quota_value,
 dense_rank() OVER (ORDER BY created_at) rank_created_at,
 dense_rank() OVER (ORDER BY updated_at) rank_updated_at,
 dense_rank() OVER (ORDER BY deleted_at) rank_deleted_at,
 dense_rank() OVER (ORDER BY created_by) rank_created_by,
 dense_rank() OVER (ORDER BY updated_by) rank_updated_by,
 dense_rank() OVER (ORDER BY deleted_by) rank_deleted_by
 FROM sys_plan_quotas p
 WHERE jsonb_path_exists_tz(to_jsonb(p) || jsonb_build_object('_search',ARRAY(
  SELECT to_tsvector(coalesce(to_jsonb(p)->>(term.item->>'field'),'')) @@ plainto_tsquery(term.item->>'value')
  FROM jsonb_array_elements(sqlc.arg(search_terms)::jsonb) WITH ORDINALITY term(item,ordinal) ORDER BY term.ordinal
 )),sqlc.arg(predicate)::text::jsonpath)
)
SELECT r.id,r.created_at,r.updated_at,r.deleted_at,r.created_by,r.updated_by,r.deleted_by,r.quota_code,r.quota_type,r.quota_value,r.plan_id
FROM ranked r
ORDER BY ARRAY(
 SELECT (to_jsonb(r)->>('rank_' || (sort.item->>'field')))::bigint * (sort.item->>'direction')::bigint
 FROM jsonb_array_elements(sqlc.arg(sort_keys)::jsonb) WITH ORDINALITY AS sort(item,ordinal)
 ORDER BY sort.ordinal
),r.id
LIMIT sqlc.narg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;
-- name: CountPlanQuotas :one
SELECT COUNT(*) FROM sys_plan_quotas p WHERE jsonb_path_exists_tz(to_jsonb(p) || jsonb_build_object('_search',ARRAY(
 SELECT to_tsvector(coalesce(to_jsonb(p)->>(term.item->>'field'),'')) @@ plainto_tsquery(term.item->>'value')
 FROM jsonb_array_elements(sqlc.arg(search_terms)::jsonb) WITH ORDINALITY term(item,ordinal) ORDER BY term.ordinal
)),sqlc.arg(predicate)::text::jsonpath);
-- name: InsertPlanQuota :exec
INSERT INTO sys_plan_quotas(plan_id,quota_code,quota_type,quota_value,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,now(),now());
-- name: UpdatePlanQuota :execrows
UPDATE sys_plan_quotas SET quota_code=$3,quota_type=$4,quota_value=$5,updated_by=$6,updated_at=now() WHERE id=$1 AND plan_id=$2;
-- name: DeletePlanQuota :execrows
DELETE FROM sys_plan_quotas WHERE id=$1 AND plan_id=$2;
