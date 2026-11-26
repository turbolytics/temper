-- Captured from pgdump pg_dump 17.12 against PostgreSQL 17.12 with log_statement = all.
-- 285 statements captured; each statement shape appears once, in first-seen order.

SELECT 1 FROM pg_database WHERE datname = 'postgres' ;
CREATE DATABASE pgdump_src;
CREATE DATABASE pgdump_dst;
CREATE SCHEMA shop;
CREATE TYPE shop.status AS ENUM ('new', 'paid', 'shipped');
CREATE DOMAIN shop.price AS numeric(12,2) CHECK (VALUE >= 0);
CREATE SEQUENCE shop.order_no START 1000;
CREATE TABLE shop.customer (
  id bigserial PRIMARY KEY, email text NOT NULL UNIQUE, name varchar(200),
  tags text[] DEFAULT '{}', profile jsonb, created timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE shop.orders (
  id bigint DEFAULT nextval('shop.order_no'), customer_id bigint REFERENCES shop.customer(id) ON DELETE CASCADE,
  status shop.status NOT NULL DEFAULT 'new', total shop.price, placed date, during tstzrange,
  note text COLLATE "C", uid uuid, raw bytea, CHECK (total < 1000000), PRIMARY KEY (id, placed)
) PARTITION BY RANGE (placed);
CREATE TABLE shop.orders_2026 PARTITION OF shop.orders FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE INDEX ON shop.orders (customer_id, placed DESC);
CREATE INDEX customer_profile ON shop.customer USING gin (profile);
CREATE UNIQUE INDEX customer_lower_email ON shop.customer (lower(email)) WHERE email IS NOT NULL;
CREATE VIEW shop.paid AS SELECT o.id, c.email, o.total FROM shop.orders o JOIN shop.customer c ON c.id = o.customer_id WHERE o.status = 'paid';
CREATE MATERIALIZED VIEW shop.daily AS SELECT placed, count(*) n, sum(total) total FROM shop.orders GROUP BY placed;
CREATE FUNCTION shop.touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.created := now(); RETURN NEW; END $$;
CREATE TRIGGER customer_touch BEFORE UPDATE ON shop.customer FOR EACH ROW EXECUTE FUNCTION shop.touch();
CREATE FUNCTION shop.order_count(c bigint) RETURNS bigint LANGUAGE sql STABLE AS $$ SELECT count(*) FROM shop.orders WHERE customer_id = c $$;
COMMENT ON TABLE shop.customer IS 'people who buy things';
COMMENT ON COLUMN shop.customer.email IS 'unique login';
CREATE ROLE shop_reader;
GRANT USAGE ON SCHEMA shop TO shop_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA shop TO shop_reader;
ALTER TABLE shop.customer ENABLE ROW LEVEL SECURITY;
CREATE POLICY own ON shop.customer USING (email = current_user);
INSERT INTO shop.customer (email, name, tags, profile) SELECT 'u' || i || '@x.test', 'User ' || i, ARRAY['t' || (i % 3)], jsonb_build_object('n', i) FROM generate_series(1, 50) i;
INSERT INTO shop.orders (customer_id, status, total, placed, during, uid, raw)
  SELECT 1 + i % 50, (ARRAY['new','paid','shipped']::shop.status[])[1 + i % 3], i * 1.5, DATE '2026-01-01' + i,
         tstzrange(TIMESTAMPTZ '2026-01-01' + i * interval '1 day', NULL), md5(i::text)::uuid, decode(md5(i::text), 'hex')
  FROM generate_series(1, 200) i;
REFRESH MATERIALIZED VIEW shop.daily;
SELECT pg_catalog.set_config('search_path', '', false);
SELECT pg_catalog.pg_is_in_recovery();
SET DATESTYLE = ISO;
SET INTERVALSTYLE = POSTGRES;
SET extra_float_digits TO 3;
SET synchronize_seqscans TO off;
SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET row_security = off;
SELECT set_config(name, 'view, foreign-table', false) FROM pg_settings WHERE name = 'restrict_nonsystem_relation_kind';
BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SELECT oid, rolname FROM pg_catalog.pg_roles ORDER BY 1;
SELECT x.tableoid, x.oid, x.extname, n.nspname, x.extrelocatable, x.extversion, x.extconfig, x.extcondition FROM pg_extension x JOIN pg_namespace n ON n.oid = x.extnamespace;
SELECT classid, objid, refobjid FROM pg_depend WHERE refclassid = 'pg_extension'::regclass AND deptype = 'e' ORDER BY 3;
SELECT n.tableoid, n.oid, n.nspname, n.nspowner, n.nspacl, acldefault('n', n.nspowner) AS acldefault FROM pg_namespace n;
SELECT c.tableoid, c.oid, c.relname, c.relnamespace, c.relkind, c.reltype, c.relowner, c.relchecks, c.relhasindex, c.relhasrules, c.relpages, c.relhastriggers, c.relpersistence, c.reloftype, c.relacl, acldefault(CASE WHEN c.relkind = 'S' THEN 's'::"char" ELSE 'r'::"char" END, c.relowner) AS acldefault, CASE WHEN c.relkind = 'f' THEN (SELECT ftserver FROM pg_catalog.pg_foreign_table WHERE ftrelid = c.oid) ELSE 0 END AS foreignserver, c.relfrozenxid, tc.relfrozenxid AS tfrozenxid, tc.oid AS toid, tc.relpages AS toastpages, tc.reloptions AS toast_reloptions, d.refobjid AS owning_tab, d.refobjsubid AS owning_col, tsp.spcname AS reltablespace, false AS relhasoids, c.relispopulated, c.relreplident, c.relrowsecurity, c.relforcerowsecurity, c.relminmxid, tc.relminmxid AS tminmxid, array_remove(array_remove(c.reloptions,'check_option=local'),'check_option=cascaded') AS reloptions, CASE WHEN 'check_option=local' = ANY (c.reloptions) THEN 'LOCAL'::text WHEN 'check_option=cascaded' = ANY (c.reloptions) THEN 'CASCADED'::text ELSE NULL END AS checkoption, am.amname, (d.deptype = 'i') IS TRUE AS is_identity_sequence, c.relispartition AS ispartition 
FROM pg_class c
LEFT JOIN pg_depend d ON (c.relkind = 'S' AND d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.objsubid = 0 AND d.refclassid = 'pg_class'::regclass AND d.deptype IN ('a', 'i'))
LEFT JOIN pg_tablespace tsp ON (tsp.oid = c.reltablespace)
LEFT JOIN pg_am am ON (c.relam = am.oid)
LEFT JOIN pg_class tc ON (c.reltoastrelid = tc.oid AND tc.relkind = 't' AND c.relkind <> 'p')
WHERE c.relkind IN ('r', 'S', 'v', 'c', 'm', 'f', 'p')
ORDER BY c.oid;
LOCK TABLE shop.customer, shop.orders, shop.orders_2026 IN ACCESS SHARE MODE;
SELECT p.tableoid, p.oid, p.proname, p.prolang, p.pronargs, p.proargtypes, p.prorettype, p.proacl, acldefault('f', p.proowner) AS acldefault, p.pronamespace, p.proowner FROM pg_proc p LEFT JOIN pg_init_privs pip ON (p.oid = pip.objoid AND pip.classoid = 'pg_proc'::regclass AND pip.objsubid = 0) WHERE p.prokind <> 'a'
  AND NOT EXISTS (SELECT 1 FROM pg_depend WHERE classid = 'pg_proc'::regclass AND objid = p.oid AND deptype = 'i')
  AND (
  pronamespace != (SELECT oid FROM pg_namespace WHERE nspname = 'pg_catalog')
  OR EXISTS (SELECT 1 FROM pg_cast
  WHERE pg_cast.oid > 16383 
  AND p.oid = pg_cast.castfunc)
  OR EXISTS (SELECT 1 FROM pg_transform
  WHERE pg_transform.oid > 16383 AND 
  (p.oid = pg_transform.trffromsql
  OR p.oid = pg_transform.trftosql))
  OR p.proacl IS DISTINCT FROM pip.initprivs);
SELECT tableoid, oid, typname, typnamespace, typacl, acldefault('T', typowner) AS acldefault, typowner, typelem, typrelid, CASE WHEN typrelid = 0 THEN ' '::"char" ELSE (SELECT relkind FROM pg_class WHERE oid = typrelid) END AS typrelkind, typtype, typisdefined, typname[0] = '_' AND typelem != 0 AND (SELECT typarray FROM pg_type te WHERE oid = pg_type.typelem) = oid AS isarray FROM pg_type;
PREPARE getDomainConstraints(pg_catalog.oid) AS
SELECT tableoid, oid, conname, pg_catalog.pg_get_constraintdef(oid) AS consrc, convalidated, contype FROM pg_catalog.pg_constraint WHERE contypid = $1 AND contype IN ('c', 'n') ORDER BY conname;
EXECUTE getDomainConstraints('16396');
SELECT tableoid, oid, lanname, lanpltrusted, lanplcallfoid, laninline, lanvalidator, lanacl, acldefault('l', lanowner) AS acldefault, lanowner FROM pg_language WHERE lanispl ORDER BY oid;
SELECT p.tableoid, p.oid, p.proname AS aggname, p.pronamespace AS aggnamespace, p.pronargs, p.proargtypes, p.proowner, p.proacl AS aggacl, acldefault('f', p.proowner) AS acldefault FROM pg_proc p LEFT JOIN pg_init_privs pip ON (p.oid = pip.objoid AND pip.classoid = 'pg_proc'::regclass AND pip.objsubid = 0) WHERE p.prokind = 'a' AND (p.pronamespace != (SELECT oid FROM pg_namespace WHERE nspname = 'pg_catalog') OR p.proacl IS DISTINCT FROM pip.initprivs);
SELECT tableoid, oid, oprname, oprnamespace, oprowner, oprkind, oprleft, oprright, oprcode::oid AS oprcode FROM pg_operator;
SELECT tableoid, oid, amname, amtype, amhandler::pg_catalog.regproc AS amhandler FROM pg_am;
SELECT tableoid, oid, opcmethod, opcname, opcnamespace, opcowner FROM pg_opclass;
SELECT tableoid, oid, opfmethod, opfname, opfnamespace, opfowner FROM pg_opfamily;
SELECT tableoid, oid, prsname, prsnamespace, prsstart::oid, prstoken::oid, prsend::oid, prsheadline::oid, prslextype::oid FROM pg_ts_parser;
SELECT tableoid, oid, tmplname, tmplnamespace, tmplinit::oid, tmpllexize::oid FROM pg_ts_template;
SELECT tableoid, oid, dictname, dictnamespace, dictowner, dicttemplate, dictinitoption FROM pg_ts_dict;
SELECT tableoid, oid, cfgname, cfgnamespace, cfgowner, cfgparser FROM pg_ts_config;
SELECT tableoid, oid, fdwname, fdwowner, fdwhandler::pg_catalog.regproc, fdwvalidator::pg_catalog.regproc, fdwacl, acldefault('F', fdwowner) AS acldefault, array_to_string(ARRAY(SELECT quote_ident(option_name) || ' ' || quote_literal(option_value) FROM pg_options_to_table(fdwoptions) ORDER BY option_name), E',
    ') AS fdwoptions FROM pg_foreign_data_wrapper;
SELECT tableoid, oid, srvname, srvowner, srvfdw, srvtype, srvversion, srvacl, acldefault('S', srvowner) AS acldefault, array_to_string(ARRAY(SELECT quote_ident(option_name) || ' ' || quote_literal(option_value) FROM pg_options_to_table(srvoptions) ORDER BY option_name), E',
    ') AS srvoptions FROM pg_foreign_server;
SELECT oid, tableoid, defaclrole, defaclnamespace, defaclobjtype, defaclacl, CASE WHEN defaclnamespace = 0 THEN acldefault(CASE WHEN defaclobjtype = 'S' THEN 's'::"char" ELSE defaclobjtype END, defaclrole) ELSE '{}' END AS acldefault FROM pg_default_acl;
SELECT tableoid, oid, collname, collnamespace, collowner, collencoding FROM pg_collation;
SELECT tableoid, oid, conname, connamespace, conowner FROM pg_conversion;
SELECT tableoid, oid, castsource, casttarget, castfunc, castcontext, castmethod FROM pg_cast c WHERE NOT EXISTS ( SELECT 1 FROM pg_range r WHERE c.castsource = r.rngtypid AND c.casttarget = r.rngmultitypid ) ORDER BY 3,4;
SELECT tableoid, oid, trftype, trflang, trffromsql::oid, trftosql::oid FROM pg_transform ORDER BY 3,4;
SELECT inhrelid, inhparent FROM pg_inherits;
SELECT e.tableoid, e.oid, evtname, evtenabled, evtevent, evtowner, array_to_string(array(select quote_literal(x)  from unnest(evttags) as t(x)), ', ') as evttags, e.evtfoid::regproc as evtfname FROM pg_event_trigger e ORDER BY e.oid;
SELECT conrelid, confrelid FROM pg_constraint JOIN pg_depend ON (objid = confrelid) WHERE contype = 'f' AND refclassid = 'pg_extension'::regclass AND classid = 'pg_class'::regclass;
SELECT
a.attrelid,
a.attnum,
a.attname,
a.attstattarget,
a.attstorage,
t.typstorage,
a.attnotnull,
a.atthasdef,
a.attisdropped,
a.attlen,
a.attalign,
a.attislocal,
pg_catalog.format_type(t.oid, a.atttypmod) AS atttypname,
array_to_string(ARRAY(
  SELECT o FROM unnest(a.attoptions) o
  WHERE substr(o, 1, 10) <> 'pg_attver='
), ', ') AS attoptions,
(
  SELECT substr(o, 10 + 1)
  FROM unnest(a.attoptions) o
  WHERE substr(o, 1, 10) = 'pg_attver='
) AS attversion,
CASE WHEN a.attcollation <> t.typcollation THEN a.attcollation ELSE 0 END AS attcollation,
pg_catalog.array_to_string(ARRAY(SELECT pg_catalog.quote_ident(option_name) || ' ' || pg_catalog.quote_literal(option_value) FROM pg_catalog.pg_options_to_table(attfdwoptions) ORDER BY option_name), E',
    ') AS attfdwoptions,
a.attcompression AS attcompression,
a.attidentity,
CASE WHEN a.atthasmissing AND NOT a.attisdropped THEN a.attmissingval ELSE null END AS attmissingval,
a.attgenerated
FROM unnest('{16400,16412,16425,16442,16446}'::pg_catalog.oid[]) AS src(tbloid)
JOIN pg_catalog.pg_attribute a ON (src.tbloid = a.attrelid) LEFT JOIN pg_catalog.pg_type t ON (a.atttypid = t.oid)
WHERE a.attnum > 0::pg_catalog.int2
ORDER BY a.attrelid, a.attnum;
SELECT a.tableoid, a.oid, adrelid, adnum, pg_catalog.pg_get_expr(adbin, adrelid) AS adsrc
FROM unnest('{16400,16412,16425}'::pg_catalog.oid[]) AS src(tbloid)
JOIN pg_catalog.pg_attrdef a ON (src.tbloid = a.adrelid)
ORDER BY a.adrelid, a.adnum;
SELECT c.tableoid, c.oid, conrelid, conname, pg_catalog.pg_get_constraintdef(c.oid) AS consrc, conislocal, convalidated FROM unnest('{16412,16425}'::pg_catalog.oid[]) AS src(tbloid)
JOIN pg_catalog.pg_constraint c ON (src.tbloid = c.conrelid)
WHERE contype = 'c' ORDER BY c.conrelid, c.conname;
SELECT partrelid FROM pg_partitioned_table WHERE
(SELECT c.oid FROM pg_opclass c JOIN pg_am a ON c.opcmethod = a.oid
WHERE opcname = 'enum_ops' AND opcnamespace = 'pg_catalog'::regnamespace AND amname = 'hash') = ANY(partclass);
SELECT t.tableoid, t.oid, i.indrelid, t.relname AS indexname, pg_catalog.pg_get_indexdef(i.indexrelid) AS indexdef, i.indkey, i.indisclustered, c.contype, c.conname, c.condeferrable, c.condeferred, c.tableoid AS contableoid, c.oid AS conoid, pg_catalog.pg_get_constraintdef(c.oid, false) AS condef, (SELECT spcname FROM pg_catalog.pg_tablespace s WHERE s.oid = t.reltablespace) AS tablespace, t.reloptions AS indreloptions, i.indisreplident, inh.inhparent AS parentidx, i.indnkeyatts AS indnkeyatts, i.indnatts AS indnatts, (SELECT pg_catalog.array_agg(attnum ORDER BY attnum)   FROM pg_catalog.pg_attribute   WHERE attrelid = i.indexrelid AND     attstattarget >= 0) AS indstatcols, (SELECT pg_catalog.array_agg(attstattarget ORDER BY attnum)   FROM pg_catalog.pg_attribute   WHERE attrelid = i.indexrelid AND     attstattarget >= 0) AS indstatvals, i.indnullsnotdistinct FROM unnest('{16400,16412,16425}'::pg_catalog.oid[]) AS src(tbloid)
JOIN pg_catalog.pg_index i ON (src.tbloid = i.indrelid) JOIN pg_catalog.pg_class t ON (t.oid = i.indexrelid) JOIN pg_catalog.pg_class t2 ON (t2.oid = i.indrelid) LEFT JOIN pg_catalog.pg_constraint c ON (i.indrelid = c.conrelid AND i.indexrelid = c.conindid AND c.contype IN ('p','u','x')) LEFT JOIN pg_catalog.pg_inherits inh ON (inh.inhrelid = indexrelid) WHERE (i.indisvalid OR t2.relkind = 'p') AND i.indisready ORDER BY i.indrelid, indexname;
SELECT tableoid, oid, stxname, stxnamespace, stxowner, stxrelid, stxstattarget FROM pg_catalog.pg_statistic_ext;
SELECT c.tableoid, c.oid, conrelid, conname, confrelid, conindid, pg_catalog.pg_get_constraintdef(c.oid) AS condef
FROM unnest('{16400,16412,16425}'::pg_catalog.oid[]) AS src(tbloid)
JOIN pg_catalog.pg_constraint c ON (src.tbloid = c.conrelid)
WHERE contype = 'f' AND conparentid = 0 ORDER BY conrelid, conname;
SELECT t.tgrelid, t.tgname, pg_catalog.pg_get_triggerdef(t.oid, false) AS tgdef, t.tgenabled, t.tableoid, t.oid, t.tgparentid <> 0 AS tgispartition
FROM unnest('{16400,16412,16425}'::pg_catalog.oid[]) AS src(tbloid)
JOIN pg_catalog.pg_trigger t ON (src.tbloid = t.tgrelid) LEFT JOIN pg_catalog.pg_trigger u ON (u.oid = t.tgparentid) WHERE ((NOT t.tgisinternal AND t.tgparentid = 0) OR t.tgenabled != u.tgenabled) ORDER BY t.tgrelid, t.tgname;
SELECT tableoid, oid, rulename, ev_class AS ruletable, ev_type, is_instead, ev_enabled FROM pg_rewrite ORDER BY oid;
SELECT pol.oid, pol.tableoid, pol.polrelid, pol.polname, pol.polcmd, pol.polpermissive, CASE WHEN pol.polroles = '{0}' THEN NULL ELSE    pg_catalog.array_to_string(ARRAY(SELECT pg_catalog.quote_ident(rolname) from pg_catalog.pg_roles WHERE oid = ANY(pol.polroles)), ', ') END AS polroles, pg_catalog.pg_get_expr(pol.polqual, pol.polrelid) AS polqual, pg_catalog.pg_get_expr(pol.polwithcheck, pol.polrelid) AS polwithcheck FROM unnest('{16400,16412,16425}'::pg_catalog.oid[]) AS src(tbloid)
JOIN pg_catalog.pg_policy pol ON (src.tbloid = pol.polrelid);
SELECT p.tableoid, p.oid, p.pubname, p.pubowner, p.puballtables, p.pubinsert, p.pubupdate, p.pubdelete, p.pubtruncate, p.pubviaroot FROM pg_publication p;
SELECT tableoid, oid, prpubid, prrelid, pg_catalog.pg_get_expr(prqual, prrelid) AS prrelqual, (CASE
  WHEN pr.prattrs IS NOT NULL THEN
    (SELECT array_agg(attname)
       FROM
         pg_catalog.generate_series(0, pg_catalog.array_upper(pr.prattrs::pg_catalog.int2[], 1)) s,
         pg_catalog.pg_attribute
      WHERE attrelid = pr.prrelid AND attnum = prattrs[s])
  ELSE NULL END) prattrs FROM pg_catalog.pg_publication_rel pr;
SELECT tableoid, oid, pnpubid, pnnspid FROM pg_catalog.pg_publication_namespace;
SELECT s.tableoid, s.oid, s.subname,
 s.subowner,
 s.subconninfo, s.subslotname, s.subsynccommit,
 s.subpublications,
 s.subbinary,
 s.substream,
 s.subtwophasestate,
 s.subdisableonerr,
 s.subpasswordrequired,
 s.subrunasowner,
 s.suborigin,
 NULL AS suboriginremotelsn,
 false AS subenabled,
 s.subfailover
FROM pg_subscription s
WHERE s.subdbid = (SELECT oid FROM pg_database
                   WHERE datname = current_database());
WITH RECURSIVE w AS ( SELECT d1.objid, d2.refobjid, c2.relkind AS refrelkind FROM pg_depend d1 JOIN pg_class c1 ON c1.oid = d1.objid AND c1.relkind = 'm' JOIN pg_rewrite r1 ON r1.ev_class = d1.objid JOIN pg_depend d2 ON d2.classid = 'pg_rewrite'::regclass AND d2.objid = r1.oid AND d2.refobjid <> d1.objid JOIN pg_class c2 ON c2.oid = d2.refobjid AND c2.relkind IN ('m','v') WHERE d1.classid = 'pg_class'::regclass UNION SELECT w.objid, d3.refobjid, c3.relkind FROM w JOIN pg_rewrite r3 ON r3.ev_class = w.refobjid JOIN pg_depend d3 ON d3.classid = 'pg_rewrite'::regclass AND d3.objid = r3.oid AND d3.refobjid <> w.refobjid JOIN pg_class c3 ON c3.oid = d3.refobjid AND c3.relkind IN ('m','v') ) SELECT 'pg_class'::regclass::oid AS classid, objid, refobjid FROM w WHERE refrelkind = 'm';
SELECT oid, lomowner, lomacl, acldefault('L', lomowner) AS acldefault FROM pg_largeobject_metadata ORDER BY lomowner, lomacl::pg_catalog.text, oid;
SELECT classid, objid, refclassid, refobjid, deptype FROM pg_depend WHERE deptype != 'p' AND deptype != 'e'
UNION ALL
SELECT 'pg_opfamily'::regclass AS classid, amopfamily AS objid, refclassid, refobjid, deptype FROM pg_depend d, pg_amop o WHERE deptype NOT IN ('p', 'e', 'i') AND classid = 'pg_amop'::regclass AND objid = o.oid AND NOT (refclassid = 'pg_opfamily'::regclass AND amopfamily = refobjid)
UNION ALL
SELECT 'pg_opfamily'::regclass AS classid, amprocfamily AS objid, refclassid, refobjid, deptype FROM pg_depend d, pg_amproc p WHERE deptype NOT IN ('p', 'e', 'i') AND classid = 'pg_amproc'::regclass AND objid = p.oid AND NOT (refclassid = 'pg_opfamily'::regclass AND amprocfamily = refobjid)
ORDER BY 1,2;
SELECT DISTINCT attrelid FROM pg_attribute WHERE attacl IS NOT NULL;
SELECT objoid, classoid, objsubid, privtype, initprivs FROM pg_init_privs;
SELECT description, classoid, objoid, objsubid FROM pg_catalog.pg_description ORDER BY classoid, objoid, objsubid;
SELECT label, provider, classoid, objoid, objsubid FROM pg_catalog.pg_seclabels ORDER BY classoid, objoid, objsubid;
SELECT pg_catalog.current_schemas(false);
SELECT tableoid, oid, datname, datdba, pg_encoding_to_char(encoding) AS encoding, datcollate, datctype, datfrozenxid, datacl, acldefault('d', datdba) AS acldefault, datistemplate, datconnlimit, datminmxid, datlocprovider, datlocale, datcollversion, daticurules, (SELECT spcname FROM pg_tablespace t WHERE t.oid = dattablespace) AS tablespace, shobj_description(oid, 'pg_database') AS description FROM pg_database WHERE datname = current_database();
SELECT provider, label FROM pg_catalog.pg_shseclabel WHERE classoid = 'pg_catalog.pg_database'::pg_catalog.regclass AND objoid = '16384';
SELECT unnest(setconfig) FROM pg_db_role_setting WHERE setrole = 0 AND setdatabase = '16384'::oid;
SELECT rolname, unnest(setconfig) FROM pg_db_role_setting s, pg_roles r WHERE setrole = r.oid AND setdatabase = '16384'::oid;
PREPARE dumpDomain(pg_catalog.oid) AS
SELECT t.typnotnull, pg_catalog.format_type(t.typbasetype, t.typtypmod) AS typdefn, pg_catalog.pg_get_expr(t.typdefaultbin, 'pg_catalog.pg_type'::pg_catalog.regclass) AS typdefaultbin, t.typdefault, CASE WHEN t.typcollation <> u.typcollation THEN t.typcollation ELSE 0 END AS typcollation FROM pg_catalog.pg_type t LEFT JOIN pg_catalog.pg_type u ON (t.typbasetype = u.oid) WHERE t.oid = $1;
PREPARE dumpEnumType(pg_catalog.oid) AS
SELECT oid, enumlabel FROM pg_catalog.pg_enum WHERE enumtypid = $1 ORDER BY enumsortorder;
PREPARE dumpFunc(pg_catalog.oid) AS
SELECT
proretset,
prosrc,
probin,
provolatile,
proisstrict,
prosecdef,
lanname,
proconfig,
procost,
prorows,
pg_catalog.pg_get_function_arguments(p.oid) AS funcargs,
pg_catalog.pg_get_function_identity_arguments(p.oid) AS funciargs,
pg_catalog.pg_get_function_result(p.oid) AS funcresult,
proleakproof,
array_to_string(protrftypes, ' ') AS protrftypes,
proparallel,
prokind,
prosupport,
pg_get_function_sqlbody(p.oid) AS prosqlbody
FROM pg_catalog.pg_proc p, pg_catalog.pg_language l
WHERE p.oid = $1 AND l.oid = p.prolang;
SELECT pg_catalog.format_type('20'::pg_catalog.oid, NULL);
PREPARE getColumnACLs(pg_catalog.oid) AS
SELECT at.attname, at.attacl, '{}' AS acldefault, pip.privtype, pip.initprivs FROM pg_catalog.pg_attribute at LEFT JOIN pg_catalog.pg_init_privs pip ON (at.attrelid = pip.objoid AND pip.classoid = 'pg_catalog.pg_class'::pg_catalog.regclass AND at.attnum = pip.objsubid) WHERE at.attrelid = $1 AND NOT at.attisdropped AND (at.attacl IS NOT NULL OR pip.initprivs IS NOT NULL) ORDER BY at.attnum;
SELECT format_type(seqtypid, NULL), seqstart, seqincrement, seqmax, seqmin, seqcache, seqcycle FROM pg_catalog.pg_sequence WHERE seqrelid = '16399'::oid;
SELECT pg_get_partkeydef('16412');
SELECT pg_catalog.pg_get_viewdef('16446'::pg_catalog.oid) AS viewdef;
PREPARE dumpTableAttach(pg_catalog.oid) AS
SELECT pg_get_expr(c.relpartbound, c.oid) FROM pg_class c WHERE c.oid = $1;
SELECT last_value, is_called FROM shop.customer_id_seq;
SELECT last_value, is_called FROM shop.order_no;
COPY shop.customer (id, email, name, tags, profile, created) TO stdout;
COPY shop.orders_2026 (id, customer_id, status, total, placed, during, note, uid, raw) TO stdout;
--
-- PostgreSQL database dump
--;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
ALTER SCHEMA shop OWNER TO postgres;
CREATE DOMAIN shop.price AS numeric(12,2)
	CONSTRAINT price_check CHECK ((VALUE >= (0)::numeric));
ALTER DOMAIN shop.price OWNER TO postgres;
ALTER TYPE shop.status OWNER TO postgres;
ALTER FUNCTION shop.order_count(c bigint) OWNER TO postgres;
ALTER FUNCTION shop.touch() OWNER TO postgres;
SET default_tablespace = '';
SET default_table_access_method = heap;
CREATE TABLE shop.customer (
    id bigint NOT NULL,
    email text NOT NULL,
    name character varying(200),
    tags text[] DEFAULT '{}'::text[],
    profile jsonb,
    created timestamp with time zone DEFAULT now() NOT NULL
);
ALTER TABLE shop.customer OWNER TO postgres;
CREATE SEQUENCE shop.customer_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE shop.customer_id_seq OWNER TO postgres;
ALTER SEQUENCE shop.customer_id_seq OWNED BY shop.customer.id;
CREATE SEQUENCE shop.order_no
    START WITH 1000
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE shop.order_no OWNER TO postgres;
CREATE TABLE shop.orders (
    id bigint DEFAULT nextval('shop.order_no'::regclass) NOT NULL,
    customer_id bigint,
    status shop.status DEFAULT 'new'::shop.status NOT NULL,
    total shop.price,
    placed date NOT NULL,
    during tstzrange,
    note text COLLATE pg_catalog."C",
    uid uuid,
    raw bytea,
    CONSTRAINT orders_total_check CHECK (((total)::numeric < (1000000)::numeric))
)
PARTITION BY RANGE (placed);
ALTER TABLE shop.orders OWNER TO postgres;
CREATE MATERIALIZED VIEW shop.daily AS
 SELECT placed,
    count(*) AS n,
    sum((total)::numeric) AS total
   FROM shop.orders
  GROUP BY placed
  WITH NO DATA;
ALTER MATERIALIZED VIEW shop.daily OWNER TO postgres;
CREATE TABLE shop.orders_2026 (
    id bigint DEFAULT nextval('shop.order_no'::regclass) NOT NULL,
    customer_id bigint,
    status shop.status DEFAULT 'new'::shop.status NOT NULL,
    total shop.price,
    placed date NOT NULL,
    during tstzrange,
    note text COLLATE pg_catalog."C",
    uid uuid,
    raw bytea,
    CONSTRAINT orders_total_check CHECK (((total)::numeric < (1000000)::numeric))
);
ALTER TABLE shop.orders_2026 OWNER TO postgres;
ALTER VIEW shop.paid OWNER TO postgres;
ALTER TABLE ONLY shop.orders ATTACH PARTITION shop.orders_2026 FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
ALTER TABLE ONLY shop.customer ALTER COLUMN id SET DEFAULT nextval('shop.customer_id_seq'::regclass);
COPY shop.customer (id, email, name, tags, profile, created) FROM stdin;
COPY shop.orders_2026 (id, customer_id, status, total, placed, during, note, uid, raw) FROM stdin;
SELECT pg_catalog.setval('shop.customer_id_seq', 50, true);
ALTER TABLE ONLY shop.customer
    ADD CONSTRAINT customer_email_key UNIQUE (email);
ALTER TABLE ONLY shop.customer
    ADD CONSTRAINT customer_pkey PRIMARY KEY (id);
ALTER TABLE ONLY shop.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id, placed);
ALTER TABLE ONLY shop.orders_2026
    ADD CONSTRAINT orders_2026_pkey PRIMARY KEY (id, placed);
CREATE INDEX orders_customer_id_placed_idx ON ONLY shop.orders USING btree (customer_id, placed DESC);
CREATE INDEX orders_2026_customer_id_placed_idx ON shop.orders_2026 USING btree (customer_id, placed DESC);
ALTER INDEX shop.orders_customer_id_placed_idx ATTACH PARTITION shop.orders_2026_customer_id_placed_idx;
ALTER INDEX shop.orders_pkey ATTACH PARTITION shop.orders_2026_pkey;
ALTER TABLE shop.orders
    ADD CONSTRAINT orders_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES shop.customer(id) ON DELETE CASCADE;
GRANT SELECT ON TABLE shop.customer TO shop_reader;
GRANT SELECT ON TABLE shop.orders TO shop_reader;
GRANT SELECT ON TABLE shop.daily TO shop_reader;
GRANT SELECT ON TABLE shop.orders_2026 TO shop_reader;
GRANT SELECT ON TABLE shop.paid TO shop_reader;
SELECT oid, rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin, rolconnlimit, rolpassword, rolvaliduntil, rolreplication, rolbypassrls, pg_catalog.shobj_description(oid, 'pg_authid') as rolcomment, rolname = current_user AS is_current_user FROM pg_authid WHERE rolname !~ '^pg_' ORDER BY 2;
SELECT unnest(setconfig) FROM pg_db_role_setting WHERE setdatabase = 0 AND setrole = (SELECT oid FROM pg_authid WHERE rolname = 'postgres');
SELECT ur.rolname AS role, um.rolname AS member, ug.rolname AS grantor, a.roleid AS roleid, a.member AS memberid, a.grantor AS grantorid, a.admin_option, a.inherit_option, a.set_option FROM pg_auth_members a LEFT JOIN pg_authid ur on ur.oid = a.roleid LEFT JOIN pg_authid um on um.oid = a.member LEFT JOIN pg_authid ug on ug.oid = a.grantor WHERE NOT (ur.rolname ~ '^pg_' AND um.rolname ~ '^pg_')ORDER BY 1,2,3;
SELECT parname, pg_catalog.pg_get_userbyid(10) AS parowner, paracl, pg_catalog.acldefault('p', 10) AS acldefault FROM pg_catalog.pg_parameter_acl ORDER BY 1;
SELECT oid, spcname, pg_catalog.pg_get_userbyid(spcowner) AS spcowner, pg_catalog.pg_tablespace_location(oid), spcacl, acldefault('t', spcowner) AS acldefault, array_to_string(spcoptions, ', '),pg_catalog.shobj_description(oid, 'pg_tablespace') FROM pg_catalog.pg_tablespace WHERE spcname !~ '^pg_' ORDER BY 1;
