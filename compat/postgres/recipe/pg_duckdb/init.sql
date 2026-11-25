-- Every database the oracle creates is copied from template1, so each one
-- starts with the extension, as an application database would.
\c template1
CREATE EXTENSION IF NOT EXISTS pg_duckdb;
