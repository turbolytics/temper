#!/bin/sh
# pg_dump and pg_restore (PostgreSQL 17) round-trip a schema with the common
# object kinds: what a backup tool sends.
set -e
psql -q -v ON_ERROR_STOP=1 -c "CREATE DATABASE pgdump_src" -c "CREATE DATABASE pgdump_dst"
psql -q -v ON_ERROR_STOP=1 -d pgdump_src -f /apps/pgdump/schema.sql
pg_dump -Fc -d pgdump_src -f /tmp/dump
pg_restore -d pgdump_dst /tmp/dump
pg_dump -d pgdump_src --schema-only -f /tmp/schema.sql
pg_dumpall --globals-only -f /tmp/globals.sql
echo "pg_dump $(pg_dump --version | awk '{print $3}')"
