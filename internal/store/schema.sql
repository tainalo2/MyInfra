CREATE TABLE IF NOT EXISTS users (
    id text PRIMARY KEY,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS onboarded boolean;
UPDATE users SET onboarded = true WHERE onboarded IS NULL;
ALTER TABLE users ALTER COLUMN onboarded SET DEFAULT false;
ALTER TABLE users ALTER COLUMN onboarded SET NOT NULL;

CREATE TABLE IF NOT EXISTS sessions (
    token_hash text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS secrets (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('ssh_key', 'provider_token', 'dns_token', 'git_token')),
    name text NOT NULL,
    provider text NOT NULL DEFAULT '',
    fingerprint text NOT NULL DEFAULT '',
    public_key text NOT NULL DEFAULT '',
    hint text NOT NULL DEFAULT '',
    has_private boolean NOT NULL DEFAULT false,
    ciphertext bytea NOT NULL,
    nonce bytea NOT NULL,
    wrapped_dek bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS clusters (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS servers (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    cluster_id text REFERENCES clusters (id) ON DELETE SET NULL,
    name text NOT NULL,
    origin text NOT NULL CHECK (origin IN ('linked', 'purchased')),
    status text NOT NULL CHECK (status IN ('pending', 'deploying', 'linked', 'failed')),
    address text NOT NULL DEFAULT '',
    join_token text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS server_keys (
    server_id text NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    secret_id text NOT NULL REFERENCES secrets (id) ON DELETE CASCADE,
    PRIMARY KEY (server_id, secret_id)
);

CREATE TABLE IF NOT EXISTS applications (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    source_kind text NOT NULL CHECK (source_kind IN ('image', 'git', 'template')),
    source text NOT NULL,
    destination_kind text NOT NULL CHECK (destination_kind IN ('server', 'cluster')),
    server_id text REFERENCES servers (id),
    cluster_id text REFERENCES clusters (id),
    replicas integer NOT NULL DEFAULT 1 CHECK (replicas >= 1 AND replicas <= 10),
    variables jsonb NOT NULL DEFAULT '{}'::jsonb,
    run_status text NOT NULL DEFAULT 'preparing',
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (destination_kind = 'server' AND server_id IS NOT NULL AND cluster_id IS NULL)
        OR (destination_kind = 'cluster' AND cluster_id IS NOT NULL AND server_id IS NULL)
    )
);

ALTER TABLE applications ADD COLUMN IF NOT EXISTS run_status text NOT NULL DEFAULT 'preparing';

ALTER TABLE servers ADD COLUMN IF NOT EXISTS provider_id text NOT NULL DEFAULT '';

ALTER TABLE servers ADD COLUMN IF NOT EXISTS plan jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS provider_server_id text NOT NULL DEFAULT '';

ALTER TABLE servers ADD COLUMN IF NOT EXISTS deploy jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS control_plane boolean NOT NULL DEFAULT false;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS ssh_host_key text NOT NULL DEFAULT '';

ALTER TABLE servers ADD COLUMN IF NOT EXISTS wg_index integer NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS servers_wg_index ON servers (cluster_id, wg_index) WHERE wg_index > 0;

UPDATE servers AS chosen
SET control_plane = true
WHERE chosen.cluster_id IS NOT NULL
  AND chosen.control_plane = false
  AND chosen.id = (
    SELECT oldest.id FROM servers AS oldest
    WHERE oldest.cluster_id = chosen.cluster_id
    ORDER BY oldest.created_at
    LIMIT 1
  )
  AND NOT EXISTS (
    SELECT 1 FROM servers AS marked
    WHERE marked.cluster_id = chosen.cluster_id AND marked.control_plane
  );

ALTER TABLE servers DROP CONSTRAINT IF EXISTS servers_status_check;

ALTER TABLE servers ADD CONSTRAINT servers_status_check CHECK (status IN ('pending', 'deploying', 'linked', 'failed'));

ALTER TABLE secrets ADD COLUMN IF NOT EXISTS key_id text NOT NULL DEFAULT '';

ALTER TABLE secrets DROP CONSTRAINT IF EXISTS secrets_kind_check;

ALTER TABLE secrets ADD CONSTRAINT secrets_kind_check CHECK (kind IN ('ssh_key', 'provider_token', 'dns_token', 'git_token', 'registry_token'));

ALTER TABLE clusters ADD COLUMN IF NOT EXISTS origin text NOT NULL DEFAULT 'created';

ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_origin_check;

ALTER TABLE clusters ADD CONSTRAINT clusters_origin_check CHECK (origin IN ('created', 'imported'));

ALTER TABLE clusters ADD COLUMN IF NOT EXISTS kube_ciphertext bytea;

ALTER TABLE clusters ADD COLUMN IF NOT EXISTS kube_nonce bytea;

ALTER TABLE clusters ADD COLUMN IF NOT EXISTS kube_wrapped bytea;

CREATE TABLE IF NOT EXISTS application_services (
    id text PRIMARY KEY,
    application_id text NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    name text NOT NULL,
    image text NOT NULL,
    published boolean NOT NULL DEFAULT false,
    disk boolean NOT NULL DEFAULT false,
    replicas integer NOT NULL DEFAULT 1 CHECK (replicas >= 1 AND replicas <= 10),
    UNIQUE (application_id, name)
);

ALTER TABLE application_services ADD COLUMN IF NOT EXISTS listen integer NOT NULL DEFAULT 80;

ALTER TABLE application_services ADD COLUMN IF NOT EXISTS disk_path text NOT NULL DEFAULT '/data';

ALTER TABLE application_services ADD COLUMN IF NOT EXISTS disk_gi integer NOT NULL DEFAULT 1;

ALTER TABLE application_services ADD COLUMN IF NOT EXISTS args text NOT NULL DEFAULT '';

ALTER TABLE application_services DROP CONSTRAINT IF EXISTS application_services_listen_check;

ALTER TABLE application_services ADD CONSTRAINT application_services_listen_check CHECK (listen >= 1 AND listen <= 65535);

ALTER TABLE application_services DROP CONSTRAINT IF EXISTS application_services_disk_gi_check;

ALTER TABLE application_services ADD CONSTRAINT application_services_disk_gi_check CHECK (disk_gi >= 1 AND disk_gi <= 1024);

CREATE TABLE IF NOT EXISTS installation (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    provider text NOT NULL DEFAULT '',
    account_id text NOT NULL DEFAULT '',
    server_id text NOT NULL DEFAULT '',
    public_url text NOT NULL DEFAULT ''
);

ALTER TABLE installation ADD COLUMN IF NOT EXISTS method text NOT NULL DEFAULT '';

ALTER TABLE installation ADD COLUMN IF NOT EXISTS cluster_id text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS domains (
    id text PRIMARY KEY,
    application_id text NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    hostname text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('default', 'custom')),
    status text NOT NULL CHECK (status IN ('unpublished', 'pending_dns', 'ready')),
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE domains ADD COLUMN IF NOT EXISTS zone text NOT NULL DEFAULT '';

ALTER TABLE domains ADD COLUMN IF NOT EXISTS secret_id text NOT NULL DEFAULT '';

ALTER TABLE domains ADD COLUMN IF NOT EXISTS detail text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS domains_hostname_key ON domains (hostname);

CREATE TABLE IF NOT EXISTS templates (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS destructions (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('application', 'server', 'cluster')),
    target_id text NOT NULL DEFAULT '',
    name text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'failed', 'done')),
    body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE applications ADD COLUMN IF NOT EXISTS paused boolean NOT NULL DEFAULT false;

ALTER TABLE applications ADD COLUMN IF NOT EXISTS rollout text NOT NULL DEFAULT '';

ALTER TABLE applications ADD COLUMN IF NOT EXISTS rollout_detail text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS image_rollbacks (
    application_id text PRIMARY KEY REFERENCES applications (id) ON DELETE CASCADE,
    images jsonb NOT NULL,
    run_status text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS app_secrets (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    ciphertext bytea NOT NULL,
    nonce bytea NOT NULL,
    wrapped_dek bytea NOT NULL,
    apps jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, name)
);

CREATE TABLE IF NOT EXISTS template_runs (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    template_id text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('running', 'failed', 'done', 'cancelled')),
    cluster_id text NOT NULL DEFAULT '',
    body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS clusters_owner_name_key ON clusters (owner_id, lower(btrim(name)));

CREATE UNIQUE INDEX IF NOT EXISTS servers_owner_name_key ON servers (owner_id, lower(btrim(name)));

CREATE UNIQUE INDEX IF NOT EXISTS applications_owner_name_key ON applications (owner_id, lower(btrim(name)));

CREATE UNIQUE INDEX IF NOT EXISTS templates_owner_name_key ON templates (owner_id, lower(btrim(name)));

CREATE UNIQUE INDEX IF NOT EXISTS app_secrets_owner_name_ci_key ON app_secrets (owner_id, lower(btrim(name)));

ALTER TABLE template_runs DROP CONSTRAINT IF EXISTS template_runs_status_check;

ALTER TABLE template_runs ADD CONSTRAINT template_runs_status_check CHECK (status IN ('running', 'failed', 'done', 'cancelled'));

CREATE TABLE IF NOT EXISTS events (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    at timestamptz NOT NULL DEFAULT now(),
    kind text NOT NULL,
    level text NOT NULL DEFAULT 'info',
    message text NOT NULL,
    server_id text NOT NULL DEFAULT '',
    cluster_id text NOT NULL DEFAULT ''
);

ALTER TABLE events DROP CONSTRAINT IF EXISTS events_level_check;

ALTER TABLE events ADD CONSTRAINT events_level_check CHECK (level IN ('info', 'ok', 'warn', 'bad'));

CREATE INDEX IF NOT EXISTS events_owner_at ON events (owner_id, at DESC);

ALTER TABLE applications ADD COLUMN IF NOT EXISTS run_detail text NOT NULL DEFAULT '';

-- The certificate files of Traefik, sealed, so that a restart or a new cluster
-- reuses the certificates instead of asking for new ones.
CREATE TABLE IF NOT EXISTS certificate_stores (
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    resolver text NOT NULL,
    ciphertext bytea NOT NULL,
    nonce bytea NOT NULL,
    wrapped_dek bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, resolver)
);

-- Empty: the service keeps its node disk. migrating: its SQLite files are being
-- copied to the object store of the cluster. ready: it restarts from that copy.
ALTER TABLE application_services ADD COLUMN IF NOT EXISTS backup text NOT NULL DEFAULT '';

-- Empty: the service keeps its data on its disk. migrating: a replicated PostgreSQL
-- is being built and the data moved into it. ready: the service runs from it.
ALTER TABLE application_services ADD COLUMN IF NOT EXISTS database text NOT NULL DEFAULT '';

-- Images a person brought: an archive from docker save, sent or fetched from a
-- URL. MyInfra loads them on the servers itself, under myinfra.local/<name>:<tag>:
-- the servers need not reach MyInfra, which often runs behind a home router.
CREATE TABLE IF NOT EXISTS images (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ref text NOT NULL UNIQUE,
    source text NOT NULL CHECK (source IN ('upload', 'url')),
    source_url text NOT NULL DEFAULT '',
    size bigint NOT NULL,
    digest text NOT NULL,
    platform text NOT NULL DEFAULT '',
    config bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The archive, cut in pieces: PostgreSQL holds it wherever MyInfra runs.
CREATE TABLE IF NOT EXISTS image_chunks (
    image_id text NOT NULL REFERENCES images (id) ON DELETE CASCADE,
    seq integer NOT NULL,
    data bytea NOT NULL,
    PRIMARY KEY (image_id, seq)
);
