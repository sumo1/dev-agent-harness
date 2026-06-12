#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env.worktree}"

if [ -f "$ENV_FILE" ] && [ "${FORCE:-0}" != "1" ]; then
  echo "Refusing to overwrite existing $ENV_FILE. Re-run with FORCE=1 if you want to regenerate it."
  exit 1
fi

worktree_name="${WORKTREE_NAME:-$(basename "$PWD")}"
slug="$(printf '%s' "$worktree_name" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
if [ -z "$slug" ]; then
  slug="multica"
fi

hash_value="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
offset=$((hash_value % 1000))

postgres_port=5432

# DB strategy:
#   - Default: each worktree gets its own database (multica_<slug>_<offset>),
#     matching the CLAUDE.md "database-level isolation" rule for normal worktrees.
#   - REUSE_DATABASE_URL set: reuse an existing database verbatim (used by the
#     dogfood flow, which isolates only the git worktree + ports and shares the
#     control plane's live tables). We still parse out POSTGRES_* so tooling that
#     reads those individual fields stays consistent with DATABASE_URL.
if [ -n "${REUSE_DATABASE_URL:-}" ]; then
  database_url="$REUSE_DATABASE_URL"
  # postgres://user[:pass]@host[:port]/db[?params]
  _rest="${database_url#*://}"
  _creds="${_rest%%@*}"
  _hostpart="${_rest#*@}"
  _hostport="${_hostpart%%/*}"
  _dbpart="${_hostpart#*/}"
  postgres_user="${_creds%%:*}"
  if [ "$_creds" = "$postgres_user" ]; then
    postgres_password=""
  else
    postgres_password="${_creds#*:}"
  fi
  if [ "${_hostport#*:}" != "$_hostport" ]; then
    postgres_port="${_hostport#*:}"
  fi
  postgres_db="${_dbpart%%\?*}"
else
  postgres_db="multica_${slug}_${offset}"
  postgres_user="multica"
  postgres_password="multica"
  database_url="postgres://multica:multica@localhost:${postgres_port}/${postgres_db}?sslmode=disable"
fi

backend_port=$((18080 + offset))
frontend_port=$((13000 + offset))
desktop_renderer_port=$((14000 + offset))
frontend_origin="http://localhost:${frontend_port}"
desktop_daemon_profile="desktop-localhost-${backend_port}"

cat > "$ENV_FILE" <<EOF
POSTGRES_DB=${postgres_db}
POSTGRES_USER=${postgres_user}
POSTGRES_PASSWORD=${postgres_password}
POSTGRES_PORT=${postgres_port}
DATABASE_URL=${database_url}

PORT=${backend_port}
JWT_SECRET=change-me-in-production
MULTICA_DEV_VERIFICATION_CODE=888888
MULTICA_SERVER_URL=ws://localhost:${backend_port}/ws
MULTICA_APP_URL=${frontend_origin}

GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
GOOGLE_REDIRECT_URI=${frontend_origin}/auth/callback

FRONTEND_PORT=${frontend_port}
FRONTEND_ORIGIN=${frontend_origin}
NEXT_PUBLIC_API_URL=http://localhost:${backend_port}
NEXT_PUBLIC_WS_URL=ws://localhost:${backend_port}/ws

VITE_API_URL=http://localhost:${backend_port}
VITE_WS_URL=ws://localhost:${backend_port}/ws
VITE_APP_URL=${frontend_origin}
DESKTOP_APP_SUFFIX=${slug}
DESKTOP_RENDERER_PORT=${desktop_renderer_port}
DESKTOP_DAEMON_PROFILE=${desktop_daemon_profile}
EOF

echo "Generated $ENV_FILE for worktree '$worktree_name'"
echo "  Shared Postgres: localhost:${postgres_port}"
echo "  Database: ${postgres_db}"
echo "  Backend:  http://localhost:${backend_port}"
echo "  Frontend: ${frontend_origin}"
echo "  Desktop:  Multica Canary ${slug} (renderer :${desktop_renderer_port})"
echo "  Daemon profile: ${desktop_daemon_profile}"
echo ""
echo "Next steps:"
echo "  make setup-worktree"
echo "  make start-worktree"
echo "  make start-desktop-worktree"
