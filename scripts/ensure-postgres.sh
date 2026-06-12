#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-multica}"
DATABASE_URL="${DATABASE_URL:-}"

export PGPASSWORD="$POSTGRES_PASSWORD"

db_host=""
db_port="${POSTGRES_PORT:-5432}"
db_name="$POSTGRES_DB"

parse_database_url() {
  local rest authority hostport path port_part

  rest="${DATABASE_URL#*://}"
  rest="${rest%%\?*}"
  authority="${rest%%/*}"
  path="${rest#*/}"

  if [ "$authority" = "$rest" ]; then
    path=""
  fi

  hostport="${authority##*@}"

  if [[ "$hostport" == \[* ]]; then
    db_host="${hostport#\[}"
    db_host="${db_host%%]*}"
    port_part="${hostport#*\]}"
    if [[ "$port_part" == :* ]] && [ -n "${port_part#:}" ]; then
      db_port="${port_part#:}"
    fi
  else
    db_host="${hostport%%:*}"
    if [[ "$hostport" == *:* ]] && [ -n "${hostport##*:}" ]; then
      db_port="${hostport##*:}"
    fi
  fi

  if [ -n "$path" ]; then
    db_name="${path%%/*}"
  fi
}

if [ -n "$DATABASE_URL" ]; then
  parse_database_url
fi

is_local() {
  [ -z "$DATABASE_URL" ] || [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ] || [ "$db_host" = "::1" ]
}

local_host="${db_host:-localhost}"
local_port="${db_port:-5432}"

# A Postgres already listening on the target host:port — native install, an
# already-running Docker container, or a shared instance — is reused as-is. We
# only fall back to "docker compose up postgres" when nothing is listening AND
# no native client exists to talk to it. This keeps "reuse what's already there"
# the default path (the dogfood flow depends on it) instead of forcing Docker
# for every localhost URL.
native_pg_running() {
  command -v pg_isready > /dev/null 2>&1 || return 1
  pg_isready -h "$local_host" -p "$local_port" > /dev/null 2>&1
}

ensure_db_native() {
  echo "==> Ensuring database '$POSTGRES_DB' exists (native client on $local_host:$local_port)..."
  local exists
  exists="$(psql -h "$local_host" -p "$local_port" -U "$POSTGRES_USER" -d postgres -Atqc \
    "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'" 2>/dev/null || true)"
  if [ "$exists" != "1" ]; then
    createdb -h "$local_host" -p "$local_port" -U "$POSTGRES_USER" "$POSTGRES_DB"
  fi
  echo "✓ PostgreSQL ready (existing instance). Database: $POSTGRES_DB"
}

if is_local; then
  # ---------- Local: reuse a running Postgres, else fall back to Docker ----------
  if native_pg_running; then
    ensure_db_native
  else
    echo "==> No Postgres on $local_host:$local_port; starting shared Docker container..."
    docker compose up -d postgres

    echo "==> Waiting for PostgreSQL to be ready..."
    until docker compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d postgres > /dev/null 2>&1; do
      sleep 1
    done

    echo "==> Ensuring database '$POSTGRES_DB' exists..."
    db_exists="$(docker compose exec -T postgres \
      psql -U "$POSTGRES_USER" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

    if [ "$db_exists" != "1" ]; then
      docker compose exec -T postgres \
        psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
        -c "CREATE DATABASE \"$POSTGRES_DB\"" \
        > /dev/null
    fi

    echo "✓ PostgreSQL ready (local Docker). Database: $POSTGRES_DB"
  fi
else
  # ---------- Remote: skip Docker, verify connectivity ----------
  echo "==> Remote database detected (host: $db_host). Skipping Docker."
  if command -v pg_isready > /dev/null 2>&1; then
    echo "==> Waiting for PostgreSQL at $db_host:$db_port to be ready..."
    until pg_isready -d "$DATABASE_URL" > /dev/null 2>&1; do
      sleep 1
    done
    echo "✓ PostgreSQL ready (remote: $db_host:$db_port). Database: $db_name"
  else
    echo "==> pg_isready not found. Skipping remote connectivity preflight."
    echo "✓ PostgreSQL configured (remote: $db_host:$db_port). Database: $db_name"
  fi
fi
