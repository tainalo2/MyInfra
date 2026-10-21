CREATE TABLE IF NOT EXISTS sync_meta (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    instance_id text NOT NULL DEFAULT '',
    seq bigint NOT NULL DEFAULT 0
);

INSERT INTO sync_meta (singleton, instance_id, seq)
VALUES (true, '', 0)
ON CONFLICT (singleton) DO NOTHING;

CREATE TABLE IF NOT EXISTS sync_tomb (
    tbl text NOT NULL,
    row_id text NOT NULL,
    seq bigint NOT NULL,
    origin text NOT NULL DEFAULT '',
    payload jsonb,
    PRIMARY KEY (tbl, row_id)
);

CREATE TABLE IF NOT EXISTS sync_peers (
    url text PRIMARY KEY,
    last_ok timestamptz,
    last_error text NOT NULL DEFAULT ''
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE server_keys ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE server_keys ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE applications ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE application_services ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE application_services ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE domains ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE destructions ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE destructions ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE template_runs ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE template_runs ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE image_rollbacks ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE image_rollbacks ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE app_secrets ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE app_secrets ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE updates ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE updates ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';
ALTER TABLE update_jobs ADD COLUMN IF NOT EXISTS sync_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE update_jobs ADD COLUMN IF NOT EXISTS sync_origin text NOT NULL DEFAULT '';

CREATE OR REPLACE FUNCTION myinfra_sync_touch() RETURNS trigger AS $$
DECLARE
    next_seq bigint;
    inst text;
    rid text;
BEGIN
    IF current_setting('myinfra.apply', true) = '1' THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    UPDATE sync_meta SET seq = seq + 1 WHERE singleton RETURNING seq INTO next_seq;
    SELECT instance_id INTO inst FROM sync_meta;
    IF TG_OP = 'DELETE' THEN
        IF TG_TABLE_NAME = 'server_keys' THEN
            rid := OLD.server_id || '/' || OLD.secret_id;
        ELSIF TG_TABLE_NAME = 'image_rollbacks' THEN
            rid := OLD.application_id;
        ELSIF TG_TABLE_NAME = 'updates' THEN
            rid := OLD.kind || '/' || OLD.target_id;
        ELSE
            rid := OLD.id;
        END IF;
        INSERT INTO sync_tomb (tbl, row_id, seq, origin, payload)
        VALUES (TG_TABLE_NAME, rid, next_seq, inst, to_jsonb(OLD))
        ON CONFLICT (tbl, row_id) DO UPDATE
        SET seq = EXCLUDED.seq, origin = EXCLUDED.origin, payload = EXCLUDED.payload;
        RETURN OLD;
    END IF;
    NEW.sync_seq := next_seq;
    NEW.sync_origin := inst;
    IF TG_TABLE_NAME = 'server_keys' THEN
        rid := NEW.server_id || '/' || NEW.secret_id;
    ELSIF TG_TABLE_NAME = 'image_rollbacks' THEN
        rid := NEW.application_id;
    ELSIF TG_TABLE_NAME = 'updates' THEN
        rid := NEW.kind || '/' || NEW.target_id;
    ELSE
        rid := NEW.id;
    END IF;
    DELETE FROM sync_tomb WHERE tbl = TG_TABLE_NAME AND sync_tomb.row_id = rid;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    name text;
BEGIN
    FOREACH name IN ARRAY ARRAY[
        'users', 'secrets', 'clusters', 'servers', 'server_keys', 'applications',
        'application_services', 'domains', 'templates', 'destructions', 'template_runs', 'image_rollbacks',
        'app_secrets', 'updates', 'update_jobs'
    ]
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', name || '_sync', name);
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION myinfra_sync_touch()',
            name || '_sync', name
        );
    END LOOP;
END;
$$;
