#!/bin/sh
# recipe/capture.sh APP   (django, psycopg or pgdump)
# Runs APP's test suite against a fresh PostgreSQL 17.12 that logs every
# statement, then writes corpus/apps/APP.sql and corpus/apps/APP.refs.json.
set -e
cd "$(dirname "$0")/.."
app="$1"
[ -x "apps/$app/run.sh" ] || { echo "usage: recipe/capture.sh {django,psycopg,pgdump}" >&2; exit 2; }
mkdir -p corpus/.capture
docker compose --profile capture rm -sfv capture >/dev/null 2>&1 || true
docker compose --profile capture up -d --wait capture
docker compose --profile capture build app
docker compose --profile capture run --rm -T app sh "/apps/$app/run.sh" > "corpus/.capture/$app.out"
version=$(tail -1 "corpus/.capture/$app.out")
docker compose --profile capture logs --no-log-prefix capture > "corpus/.capture/$app.log"
docker compose --profile capture rm -sfv capture >/dev/null
docker compose run --rm -T oracle weights file "$app" "$version" "/corpus/.capture/$app.log"
