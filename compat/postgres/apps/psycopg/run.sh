#!/bin/sh
# psycopg's own test suite against PostgreSQL, at the release the oracle uses.
set -e
VERSION=3.3.9
git clone -q --depth 1 -b "$VERSION" https://github.com/psycopg/psycopg /work/psycopg
cd /work/psycopg
pip install -q "./psycopg[test]" "./psycopg_pool" "psycopg-binary==$VERSION"
export PSYCOPG_TEST_DSN="host=capture user=postgres password=postgres dbname=postgres"
python -m pytest tests -q -m "not slow and not timing" -k "not crdb" 2>&1 | tail -3 || true
echo "psycopg $VERSION"
