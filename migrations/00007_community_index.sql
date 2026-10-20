-- +goose Up
-- A remote group's own NSFW flag (ActivityStreams sensitive), copied to its community.
ALTER TABLE actors ADD COLUMN sensitive boolean NOT NULL DEFAULT false;
-- The NSFW setting: set when a person confirms they are 18 or older, cleared when they turn it off.
ALTER TABLE actors ADD COLUMN nsfw_confirmed_at timestamptz;

-- index_source marks a server an admin chose to index; indexed_at is its last full read.
ALTER TABLE peers ADD COLUMN index_source boolean NOT NULL DEFAULT false;
ALTER TABLE peers ADD COLUMN indexed_at timestamptz;

-- The community index: communities read from the community lists of Lemmy and
-- PieFed servers. server is the peer key it was read from, with a port when the
-- server needs one; domain is the host of the AP ID.
CREATE TABLE community_index (
    ap_id text PRIMARY KEY,
    server text NOT NULL,
    domain text NOT NULL,
    name text NOT NULL,
    title text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    active_month integer NOT NULL DEFAULT 0,
    software text NOT NULL,
    nsfw boolean NOT NULL,
    read_at timestamptz NOT NULL DEFAULT now(),
    search tsvector GENERATED ALWAYS AS (to_tsvector('simple', name || ' ' || title || ' ' || description)) STORED
);
CREATE INDEX community_index_search ON community_index USING gin (search);
CREATE INDEX community_index_server ON community_index (server);
CREATE INDEX community_index_active ON community_index (active_month DESC);

-- +goose Down
DROP TABLE community_index;
ALTER TABLE peers DROP COLUMN indexed_at;
ALTER TABLE peers DROP COLUMN index_source;
ALTER TABLE actors DROP COLUMN nsfw_confirmed_at;
ALTER TABLE actors DROP COLUMN sensitive;
