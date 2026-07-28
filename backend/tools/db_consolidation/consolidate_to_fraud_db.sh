#!/usr/bin/env bash
set -euo pipefail

COMPOSE=${COMPOSE:-docker compose}
POSTGRES_SERVICE=${POSTGRES_SERVICE:-postgres}
POSTGRES_USER=${POSTGRES_USER:-fraud}
TARGET_DB=${TARGET_DB:-fraud}
REQUESTED_BACKUP_DIR=${BACKUP_DIR:-}
REUSE_DUMPS=${REUSE_DUMPS:-true}
DROP_LEGACY_DBS=${DROP_LEGACY_DBS:-true}
ALLOW_EXISTING_TARGET=${ALLOW_EXISTING_TARGET:-0}

DATA_MODEL_DB=${DATA_MODEL_DB:-datamodel}
INGESTION_DB=${INGESTION_DB:-ingestion}
DECISION_ENGINE_DB=${DECISION_ENGINE_DB:-decision_engine}
SCREENING_DB=${SCREENING_DB:-screening}
CASE_MANAGER_DB=${CASE_MANAGER_DB:-case_manager}

read -r -a COMPOSE_CMD <<< "$COMPOSE"

log() {
  printf '[db-consolidation] %s\n' "$*"
}

die() {
  printf '[db-consolidation] error: %s\n' "$*" >&2
  exit 1
}

compose() {
  "${COMPOSE_CMD[@]}" "$@"
}

find_latest_backup_dir() {
  local latest
  latest=$(find /tmp -maxdepth 1 -type d -name 'fraud-db-consolidation-*' -print 2>/dev/null | sort -r | head -n 1 || true)
  if [[ -n "$latest" ]] && find "$latest" -maxdepth 1 -type f -name '*.dump' -size +0c -print -quit | grep -q .; then
    printf '%s\n' "$latest"
  fi
}

resolve_backup_dir() {
  if [[ -n "$REQUESTED_BACKUP_DIR" ]]; then
    printf '%s\n' "$REQUESTED_BACKUP_DIR"
    return
  fi

  if [[ "$REUSE_DUMPS" == "true" ]]; then
    local latest
    latest=$(find_latest_backup_dir || true)
    if [[ -n "$latest" ]]; then
      printf '%s\n' "$latest"
      return
    fi
  fi

  printf '/tmp/fraud-db-consolidation-%s\n' "$(date +%Y%m%d-%H%M%S)"
}

service_exists() {
  local service=$1
  local services
  services=$(compose config --services 2>/dev/null || true)
  if printf '%s\n' "$services" | grep -qx "$service"; then
    return 0
  fi

  services=$(compose --profile screening-worker config --services 2>/dev/null || true)
  printf '%s\n' "$services" | grep -qx "$service"
}

stop_service_if_defined() {
  local service=$1
  if ! service_exists "$service"; then
    return
  fi

  log "stopping $service"
  compose stop "$service" >/dev/null || true
}

require_identifier() {
  local value=$1
  local label=$2
  if [[ ! "$value" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
    die "$label must be a simple PostgreSQL identifier, got: $value"
  fi
}

sql_ident() {
  local value=${1//\"/\"\"}
  printf '"%s"' "$value"
}

sql_literal() {
  local value=${1//\'/\'\'}
  printf "'%s'" "$value"
}

psql_exec() {
  local db=$1
  shift
  compose exec -T "$POSTGRES_SERVICE" psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$db" "$@"
}

psql_scalar() {
  local db=$1
  local sql=$2
  psql_exec "$db" -Atc "$sql"
}

db_exists() {
  local db=$1
  [[ "$(psql_scalar postgres "SELECT 1 FROM pg_database WHERE datname = $(sql_literal "$db")")" == "1" ]]
}

schema_exists() {
  local db=$1
  local schema=$2
  [[ "$(psql_scalar "$db" "SELECT 1 FROM information_schema.schemata WHERE schema_name = $(sql_literal "$schema")")" == "1" ]]
}

schema_row_count() {
  local db=$1
  local schema=$2
  if ! db_exists "$db" || ! schema_exists "$db" "$schema"; then
    printf '0\n'
    return
  fi

  psql_scalar "$db" "
    WITH tables AS (
      SELECT format('%I.%I', schemaname, tablename) AS relation_name
      FROM pg_tables
      WHERE schemaname = $(sql_literal "$schema")
    )
    SELECT COALESCE(SUM((
      xpath('/row/c/text()', query_to_xml('SELECT count(*) AS c FROM ' || relation_name, false, true, ''))
    )[1]::text::bigint), 0)
    FROM tables;
  "
}

target_user_row_count() {
  psql_scalar "$TARGET_DB" "
    WITH tables AS (
      SELECT format('%I.%I', schemaname, tablename) AS relation_name
      FROM pg_tables
      WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
        AND NOT (schemaname = 'public' AND tablename LIKE 'schema_migrations%')
        AND NOT (schemaname = 'river' AND tablename IN ('river_migration'))
    )
    SELECT COALESCE(SUM((
      xpath('/row/c/text()', query_to_xml('SELECT count(*) AS c FROM ' || relation_name, false, true, ''))
    )[1]::text::bigint), 0)
    FROM tables;
  "
}

assert_no_active_river_jobs() {
  local db=$1
  if ! db_exists "$db"; then
    return
  fi
  if [[ "$(psql_scalar "$db" "SELECT to_regclass('river.river_job') IS NOT NULL")" != "t" ]]; then
    return
  fi

  local active_count
  active_count=$(psql_scalar "$db" "
    SELECT count(*)
    FROM river.river_job
    WHERE state::text NOT IN ('completed', 'cancelled', 'discarded');
  ")
  if [[ "$active_count" != "0" ]]; then
    die "$db has $active_count active River job(s). Drain workers or resolve pending jobs before consolidation."
  fi
}

backup_db() {
  local db=$1
  local dump_file="$BACKUP_DIR/$db.dump"
  if ! db_exists "$db"; then
    log "backup skipped; database $db does not exist"
    return
  fi

  if [[ "$REUSE_DUMPS" == "true" && -s "$dump_file" ]]; then
    log "backup reused for $db from $dump_file"
    return
  fi

  log "backing up $db to $dump_file"
  compose exec -T "$POSTGRES_SERVICE" pg_dump -U "$POSTGRES_USER" -Fc "$db" > "$dump_file"
}

create_target_db() {
  if db_exists "$TARGET_DB"; then
    log "target database $TARGET_DB already exists"
    return
  fi

  log "creating target database $TARGET_DB"
  psql_exec postgres -c "CREATE DATABASE $(sql_ident "$TARGET_DB")"
}

run_service_migration() {
  local service=$1
  local required=${2:-required}
  if ! service_exists "$service"; then
    if [[ "$required" == "optional" ]]; then
      log "migration skipped; optional compose service $service is not defined"
      return
    fi
    die "required compose service $service is not defined. Run this command from the New-fraud-system directory with the updated docker-compose.yml."
  fi

  log "running $service"
  compose run --rm "$service" up
}

dump_restore_schema_data() {
  local source_db=$1
  local schema=$2
  local label=$3
  local row_count

  if ! db_exists "$source_db"; then
    log "$label skipped; source database $source_db does not exist"
    return
  fi
  if ! schema_exists "$source_db" "$schema"; then
    log "$label skipped; schema $source_db.$schema does not exist"
    return
  fi

  row_count=$(schema_row_count "$source_db" "$schema")
  log "copying $label from $source_db.$schema ($row_count rows)"

  local dump_file="$BACKUP_DIR/${source_db}_${schema}_data.dump"
  if [[ "$REUSE_DUMPS" == "true" && -s "$dump_file" ]]; then
    log "data dump reused for $label from $dump_file"
  else
    compose exec -T "$POSTGRES_SERVICE" pg_dump -U "$POSTGRES_USER" -Fc --data-only --schema="$schema" "$source_db" > "$dump_file"
  fi
  compose exec -T "$POSTGRES_SERVICE" pg_restore -U "$POSTGRES_USER" -d "$TARGET_DB" --data-only --disable-triggers --no-owner < "$dump_file"

  validate_schema_table_counts "$source_db" "$schema" "$label"
}

dump_restore_tenant_schemas() {
  local source_db=$1
  if ! db_exists "$source_db"; then
    log "tenant schema copy skipped; source database $source_db does not exist"
    return
  fi

  local tenant_schema_count
  tenant_schema_count=$(psql_scalar "$source_db" "
    SELECT count(*)
    FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\';
  ")
  if [[ "$tenant_schema_count" == "0" ]]; then
    log "tenant schema copy skipped; no tenant schemas in $source_db"
    return
  fi

  log "copying $tenant_schema_count tenant schema(s) from $source_db"
  local dump_file="$BACKUP_DIR/${source_db}_tenant_schemas.dump"
  if [[ "$REUSE_DUMPS" == "true" && -s "$dump_file" ]]; then
    log "tenant schema dump reused from $dump_file"
  else
    compose exec -T "$POSTGRES_SERVICE" pg_dump -U "$POSTGRES_USER" -Fc --schema='tenant_*' "$source_db" > "$dump_file"
  fi
  compose exec -T "$POSTGRES_SERVICE" pg_restore -U "$POSTGRES_USER" -d "$TARGET_DB" --no-owner < "$dump_file"
  validate_tenant_schema_counts "$source_db"
}

validate_schema_table_counts() {
  local source_db=$1
  local schema=$2
  local label=$3
  local lines

  lines=$(psql_exec "$source_db" -At -F $'\t' -c "
    SELECT t.tablename,
      (xpath('/row/c/text()', query_to_xml(
        format('SELECT count(*) AS c FROM %I.%I', t.schemaname, t.tablename),
        false,
        true,
        ''
      )))[1]::text::bigint AS row_count
    FROM pg_tables t
    WHERE t.schemaname = $(sql_literal "$schema")
    ORDER BY t.tablename;
  ")

  while IFS=$'\t' read -r table_name source_count; do
    [[ -z "${table_name:-}" ]] && continue
    local target_count
    target_count=$(psql_scalar "$TARGET_DB" "SELECT count(*) FROM $(sql_ident "$schema").$(sql_ident "$table_name")")
    if [[ "$target_count" != "$source_count" ]]; then
      die "$label validation failed for $schema.$table_name: source=$source_count target=$target_count"
    fi
  done <<< "$lines"

  log "$label validation passed"
}

validate_tenant_schema_counts() {
  local source_db=$1
  local lines

  lines=$(psql_exec "$source_db" -At -F $'\t' -c "
    SELECT t.schemaname,
      t.tablename,
      (xpath('/row/c/text()', query_to_xml(
        format('SELECT count(*) AS c FROM %I.%I', t.schemaname, t.tablename),
        false,
        true,
        ''
      )))[1]::text::bigint AS row_count
    FROM pg_tables t
    WHERE t.schemaname LIKE 'tenant\_%' ESCAPE '\'
    ORDER BY t.schemaname, t.tablename;
  ")

  while IFS=$'\t' read -r schema_name table_name source_count; do
    [[ -z "${schema_name:-}" ]] && continue
    local target_count
    target_count=$(psql_scalar "$TARGET_DB" "SELECT count(*) FROM $(sql_ident "$schema_name").$(sql_ident "$table_name")")
    if [[ "$target_count" != "$source_count" ]]; then
      die "tenant schema validation failed for $schema_name.$table_name: source=$source_count target=$target_count"
    fi
  done <<< "$lines"

  log "tenant schema validation passed"
}

drop_legacy_db() {
  local db=$1
  if [[ "$db" == "$TARGET_DB" ]]; then
    return
  fi
  if ! db_exists "$db"; then
    return
  fi

  log "dropping legacy database $db"
  psql_exec postgres -c "DROP DATABASE $(sql_ident "$db") WITH (FORCE)"
}

require_identifier "$TARGET_DB" TARGET_DB
require_identifier "$DATA_MODEL_DB" DATA_MODEL_DB
require_identifier "$INGESTION_DB" INGESTION_DB
require_identifier "$DECISION_ENGINE_DB" DECISION_ENGINE_DB
require_identifier "$SCREENING_DB" SCREENING_DB
require_identifier "$CASE_MANAGER_DB" CASE_MANAGER_DB

BACKUP_DIR=$(resolve_backup_dir)
mkdir -p "$BACKUP_DIR"

log "target database: $TARGET_DB"
log "backup directory: $BACKUP_DIR"
log "starting postgres"
compose up -d "$POSTGRES_SERVICE"

log "stopping application containers before copy"
for service in \
  data-model-service \
  ingestion-service \
  decision-engine-service \
  screening-service \
  data-model-worker \
  ingestion-worker \
  decision-engine-worker \
  screening-worker \
  frontend
do
  stop_service_if_defined "$service"
done

for db in "$DATA_MODEL_DB" "$INGESTION_DB" "$DECISION_ENGINE_DB" "$SCREENING_DB" "$CASE_MANAGER_DB"; do
  backup_db "$db"
  assert_no_active_river_jobs "$db"
done

create_target_db

existing_rows=$(target_user_row_count)
if [[ "$existing_rows" != "0" && "$ALLOW_EXISTING_TARGET" != "1" ]]; then
  die "$TARGET_DB already has $existing_rows non-migration row(s). Set ALLOW_EXISTING_TARGET=1 only if this is intentional."
fi

run_service_migration data-model-migrate required
run_service_migration ingestion-migrate required
run_service_migration decision-engine-migrate required
run_service_migration screening-migrate required
run_service_migration case-manager-migrate optional

datamodel_ingestion_rows=$(schema_row_count "$DATA_MODEL_DB" core_ingestion)
standalone_ingestion_rows=$(schema_row_count "$INGESTION_DB" core_ingestion)
if [[ "$datamodel_ingestion_rows" != "0" && "$standalone_ingestion_rows" != "0" ]]; then
  die "both $DATA_MODEL_DB.core_ingestion and $INGESTION_DB.core_ingestion contain rows; resolve the competing ingestion histories before consolidation"
fi

dump_restore_schema_data "$DATA_MODEL_DB" core "data-model metadata"
dump_restore_tenant_schemas "$DATA_MODEL_DB"

if [[ "$datamodel_ingestion_rows" != "0" ]]; then
  dump_restore_schema_data "$DATA_MODEL_DB" core_ingestion "ingestion metadata"
else
  dump_restore_schema_data "$INGESTION_DB" core_ingestion "ingestion metadata"
fi

dump_restore_schema_data "$DECISION_ENGINE_DB" core "decision-engine data"
dump_restore_schema_data "$SCREENING_DB" screening "screening data"
dump_restore_schema_data "$CASE_MANAGER_DB" case_manager "case-manager data"

if [[ "$DROP_LEGACY_DBS" == "true" ]]; then
  for db in "$DATA_MODEL_DB" "$INGESTION_DB" "$DECISION_ENGINE_DB" "$SCREENING_DB" "$CASE_MANAGER_DB"; do
    drop_legacy_db "$db"
  done
else
  log "legacy database drop skipped because DROP_LEGACY_DBS=$DROP_LEGACY_DBS"
fi

log "consolidation complete"
log "restart services with: docker compose up -d"
