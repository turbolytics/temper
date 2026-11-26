-- A schema with the object kinds pg_dump has to read back.
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
