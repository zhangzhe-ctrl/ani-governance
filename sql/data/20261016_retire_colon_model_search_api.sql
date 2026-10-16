-- Retire the historical custom-verb Model search API after the new route has
-- been synchronized into sys_apis. The new route is GET /api/v1/models/search.
-- This only disables the old catalog row; it does not change permissions,
-- roles, plans, tenants or quotas.
--
-- Run after the API catalog sync and before the explicit Model permission
-- registration script:
--   psql "$DSN" -v ON_ERROR_STOP=1 -1 \
--     -f sql/data/20261016_retire_colon_model_search_api.sql

BEGIN;

DO $$
DECLARE
  active_count integer;
BEGIN
  PERFORM pg_advisory_xact_lock(718294633);

  SELECT count(*) INTO active_count
    FROM sys_apis
   WHERE method = 'GET'
     AND path = '/api/v1/models/search'
     AND module = 'ModelService'
     AND business_module = 'MODEL'
     AND status = 'ON'
     AND deleted_at IS NULL;

  IF active_count <> 1 THEN
    RAISE EXCEPTION 'Expected exactly one active Model search API at /api/v1/models/search, found %', active_count;
  END IF;

  UPDATE sys_apis
     SET status = 'OFF', updated_at = now()
   WHERE method = 'GET'
     AND path = '/api/v1/models:search'
     AND module = 'ModelService'
     AND business_module = 'MODEL'
     AND status = 'ON'
     AND deleted_at IS NULL;
END $$;

COMMIT;
