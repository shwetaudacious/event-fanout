#!/usr/bin/env bash
# Provision DigitalOcean infrastructure for event-fanout:
#   - DOKS cluster (event-fanout-cluster)
#   - Managed PostgreSQL 15
#   - Managed Redis 7 (Redis Streams backbone)
#
# Usage:
#   export DIGITALOCEAN_ACCESS_TOKEN=dop_v1_...
#   ./scripts/provision-digitalocean.sh
#
# Optional:
#   REGION=nyc1 CLUSTER_NAME=event-fanout-cluster ./scripts/provision-digitalocean.sh
#   REDIS_REGION=nyc3   # Managed Redis may not be available in all PG/K8s regions
#   SYNC_GITHUB_SECRETS=1 ./scripts/provision-digitalocean.sh

set -euo pipefail

REGION="${REGION:-nyc1}"
REDIS_REGION="${REDIS_REGION:-nyc3}"
CLUSTER_NAME="${CLUSTER_NAME:-event-fanout-cluster}"
PG_NAME="${PG_NAME:-event-fanout-pg}"
REDIS_NAME="${REDIS_NAME:-event-fanout-redis}"
PG_DB="${PG_DB:-eventfanout}"
GITHUB_REPO="${GITHUB_REPO:-shwetaudacious/event-fanout}"
GITHUB_ENV="${GITHUB_ENV:-production}"

if [[ -z "${DIGITALOCEAN_ACCESS_TOKEN:-}" ]]; then
  echo "ERROR: set DIGITALOCEAN_ACCESS_TOKEN" >&2
  exit 1
fi

export DIGITALOCEAN_ACCESS_TOKEN
command -v doctl >/dev/null || { echo "ERROR: install doctl"; exit 1; }

log() { echo "[provision] $*"; }

wait_db_ready() {
  local id="$1"
  local deadline=$((SECONDS + 900))
  while (( SECONDS < deadline )); do
    local status
    status="$(doctl databases get "$id" --format Status --no-header)"
    if [[ "$status" == "online" ]]; then
      return 0
    fi
    log "database $id status=$status; waiting..."
    sleep 20
  done
  echo "ERROR: database $id not online within 15m" >&2
  exit 1
}

ensure_cluster() {
  if doctl kubernetes cluster get "$CLUSTER_NAME" >/dev/null 2>&1; then
    log "DOKS cluster exists: $CLUSTER_NAME"
  else
    log "creating DOKS cluster $CLUSTER_NAME in $REGION"
    doctl kubernetes cluster create "$CLUSTER_NAME" \
      --region "$REGION" \
      --tag event-fanout \
      --node-pool "name=workers;size=s-2vcpu-4gb;count=1"
  fi
  doctl kubernetes cluster kubeconfig save "$CLUSTER_NAME"
}

ensure_postgres() {
  local id
  id="$(doctl databases list --format ID,Name --no-header | awk -v n="$PG_NAME" '$2==n {print $1; exit}')"
  if [[ -z "$id" ]]; then
    log "creating managed PostgreSQL $PG_NAME"
    doctl databases create "$PG_NAME" --engine pg --version 15 --region "$REGION" --size db-s-1vcpu-1gb --num-nodes 1 --wait
    id="$(doctl databases list --format ID,Name --no-header | awk -v n="$PG_NAME" '$2==n {print $1; exit}')"
  else
    log "managed PostgreSQL exists: $PG_NAME ($id)"
  fi
  wait_db_ready "$id"

  if ! doctl databases db list "$id" --format Name --no-header | grep -qx "$PG_DB"; then
    log "creating database $PG_DB"
    doctl databases db create "$id" "$PG_DB"
  fi

  local cluster_id
  cluster_id="$(doctl kubernetes cluster get "$CLUSTER_NAME" --format ID --no-header)"
  if ! doctl databases firewalls list "$id" --no-header 2>/dev/null | grep -q "k8s:$cluster_id"; then
    log "allowing DOKS cluster to access PostgreSQL"
    doctl databases firewalls append "$id" --rule "k8s:$cluster_id"
  fi

  DATABASE_URL="$(doctl databases connection "$id" --format URI --no-header)"
  # Prefer private networking when available
  if private="$(doctl databases connection "$id" --format URI --private --no-header 2>/dev/null)"; then
    DATABASE_URL="$private"
  fi
  # Point at application database instead of defaultdb
  DATABASE_URL="${DATABASE_URL/defaultdb/$PG_DB}"
  DATABASE_URL="${DATABASE_URL/\/postgres?/\/$PG_DB?}"
  # Ensure dbname and SSL for managed Postgres
  if [[ "$DATABASE_URL" != *"sslmode="* ]]; then
    if [[ "$DATABASE_URL" == *"?"* ]]; then
      DATABASE_URL="${DATABASE_URL}&sslmode=require"
    else
      DATABASE_URL="${DATABASE_URL}?sslmode=require"
    fi
  fi
  export DATABASE_URL
}

ensure_redis() {
  local id redis_opts
  redis_opts="$(doctl databases options regions --engine redis -o json 2>/dev/null || echo '{}')"
  if [[ "$redis_opts" == *'"redis": null'* ]] || [[ "$redis_opts" == '{"redis": null}' ]]; then
    log "DO Managed Redis unavailable on this account; using in-cluster Redis 7 (Streams) on DOKS"
    REDIS_URL="redis://event-fanout-queue:6379"
    export REDIS_URL
    return 0
  fi

  id="$(doctl databases list --format ID,Name --no-header | awk -v n="$REDIS_NAME" '$2==n {print $1; exit}')"
  if [[ -z "$id" ]]; then
    log "creating managed Redis $REDIS_NAME in $REDIS_REGION (Streams backbone)"
    if ! doctl databases create "$REDIS_NAME" --engine redis --version 7 --region "$REDIS_REGION" --size db-s-1vcpu-1gb --num-nodes 1 --wait; then
      log "managed Redis create failed; falling back to in-cluster Redis 7 (Streams) on DOKS"
      REDIS_URL="redis://event-fanout-queue:6379"
      export REDIS_URL
      return 0
    fi
    id="$(doctl databases list --format ID,Name --no-header | awk -v n="$REDIS_NAME" '$2==n {print $1; exit}')"
  else
    log "managed Redis exists: $REDIS_NAME ($id)"
  fi
  wait_db_ready "$id"

  local cluster_id
  cluster_id="$(doctl kubernetes cluster get "$CLUSTER_NAME" --format ID --no-header)"
  if ! doctl databases firewalls list "$id" --no-header 2>/dev/null | grep -q "k8s:$cluster_id"; then
    log "allowing DOKS cluster to access Redis"
    doctl databases firewalls append "$id" --rule "k8s:$cluster_id"
  fi

  REDIS_URL="$(doctl databases connection "$id" --format URI --no-header)"
  if private="$(doctl databases connection "$id" --format URI --private --no-header 2>/dev/null)"; then
    REDIS_URL="$private"
  fi
  REDIS_URL="${REDIS_URL/redis:\/\//rediss:\/\/}"
  export REDIS_URL
}

sync_github_secrets() {
  command -v gh >/dev/null || { echo "ERROR: gh CLI required for SYNC_GITHUB_SECRETS"; exit 1; }
  log "syncing secrets to GitHub environment $GITHUB_ENV on $GITHUB_REPO"
  printf '%s' "$DATABASE_URL" | gh secret set DATABASE_URL --env "$GITHUB_ENV" --repo "$GITHUB_REPO"
  printf '%s' "$REDIS_URL" | gh secret set REDIS_URL --env "$GITHUB_ENV" --repo "$GITHUB_REPO"
  if [[ -n "${DIGITALOCEAN_ACCESS_TOKEN:-}" ]]; then
    printf '%s' "$DIGITALOCEAN_ACCESS_TOKEN" | gh secret set DIGITALOCEAN_ACCESS_TOKEN --env "$GITHUB_ENV" --repo "$GITHUB_REPO"
  fi
}

run_migrations() {
  command -v psql >/dev/null || { log "psql not found; run migrations manually"; return 0; }
  log "applying schema migrations"
  psql "$DATABASE_URL" -f migrations/001_init_schema.sql
}

main() {
  cd "$(dirname "$0")/.."
  ensure_cluster
  ensure_postgres
  ensure_redis
  run_migrations

  log "provision complete"
  echo ""
  echo "DATABASE_URL=$DATABASE_URL"
  echo "REDIS_URL=$REDIS_URL"
  echo ""
  echo "Redis Streams: events:stream (consumer group fanout-workers)"
  echo "Deploy: ./scripts/deploy-doks.sh"

  if [[ "${SYNC_GITHUB_SECRETS:-0}" == "1" ]]; then
    sync_github_secrets
  fi
}

main "$@"
