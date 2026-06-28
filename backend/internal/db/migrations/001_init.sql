-- 001_init.sql
-- Initial schema for the Cloud Sandbox Management API.
--
-- Design rules (per project spec):
--   * No Row-Level Security. The Go Management API is the sole authorization
--     gatekeeper and uses a single master connection pool.
--   * All identifiers are UUIDs generated client-side (application layer) so
--     that records are deterministic across the API, DB, and Fly.io.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Tracks the small bookkeeping tables created for local users / orgs that
-- mirror the Logto identity provider. The Management API remains the source of
-- truth for authorization; Logto only authenticates.
CREATE TABLE IF NOT EXISTS organizations (
    id          uuid PRIMARY KEY,
    logto_id    text NOT NULL UNIQUE,
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id          uuid PRIMARY KEY,
    logto_id    text NOT NULL UNIQUE,
    email       text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS organization_memberships (
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role        text NOT NULL DEFAULT 'admin',
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, org_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_org ON organization_memberships(org_id);

-- Workspace templates: a versioned Dockerfile that, once built, produces an
-- image ref usable to launch sandbox sessions.
CREATE TABLE IF NOT EXISTS templates (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name               text NOT NULL,
    dockerfile_contents text NOT NULL,
    status             text NOT NULL DEFAULT 'draft',   -- draft | building | ready | failed
    image_ref          text,
    fly_app_name       text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS idx_templates_org ON templates(org_id);

-- Workspace sessions: a running (or suspended) Fly Machine bound to a template.
CREATE TABLE IF NOT EXISTS sessions (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    template_id     uuid NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    name            text NOT NULL,
    status          text NOT NULL DEFAULT 'pending', -- pending | running | suspended | stopped | error
    fly_machine_id  text,
    fly_app_name    text,
    fly_volume_id   text,
    url             text,
    region          text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sessions_org      ON sessions(org_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user     ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_template ON sessions(template_id);

CREATE TABLE IF NOT EXISTS schema_migrations (
    id          text PRIMARY KEY,
    applied_at  timestamptz NOT NULL DEFAULT now()
);
