-- +goose Up
CREATE TABLE installation (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    installation_id uuid NOT NULL DEFAULT gen_random_uuid(),
    created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO installation DEFAULT VALUES;

CREATE TABLE actors (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ap_id text NOT NULL UNIQUE,
    kind text NOT NULL CHECK (kind IN ('person', 'group', 'application')),
    local boolean NOT NULL,
    user_id uuid UNIQUE REFERENCES auth_users (id) ON DELETE SET NULL,
    domain text NOT NULL,
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    summary text NOT NULL DEFAULT '',
    inbox text NOT NULL DEFAULT '',
    shared_inbox text NOT NULL DEFAULT '',
    public_key_pem text NOT NULL DEFAULT '',
    private_key_enc bytea,
    bot boolean NOT NULL DEFAULT false,
    trust text CHECK (trust IN ('new', 'trusted')),
    promoted_at timestamptz,
    deleted_at timestamptz,
    fetched_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (local OR user_id IS NULL),
    CHECK (local OR private_key_enc IS NULL),
    CHECK ((kind = 'person' AND local) = (trust IS NOT NULL)),
    CHECK (NOT (kind = 'person' AND local AND deleted_at IS NULL AND user_id IS NULL))
);
CREATE UNIQUE INDEX actors_handle ON actors (kind, domain, lower(name));

CREATE TABLE media (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id bigint NOT NULL REFERENCES actors (id),
    storage_key text NOT NULL UNIQUE,
    mime text NOT NULL CHECK (mime IN ('image/jpeg', 'image/png', 'image/webp')),
    bytes bigint NOT NULL CHECK (bytes > 0),
    sha256 bytea NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX media_owner ON media (owner_id);

CREATE TABLE communities (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id bigint NOT NULL UNIQUE REFERENCES actors (id),
    domain text NOT NULL,
    name text NOT NULL,
    title text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    theme jsonb NOT NULL DEFAULT '{}',
    appeals_contact text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'frozen', 'departed', 'arriving')),
    nsfw boolean NOT NULL DEFAULT false,
    hold_new_posts boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain, name)
);

CREATE TABLE community_domains (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    domain text NOT NULL UNIQUE,
    txt_token text NOT NULL,
    created_by bigint NOT NULL REFERENCES actors (id),
    verified_at timestamptz,
    community_id bigint UNIQUE REFERENCES communities (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE rules (
    community_id bigint NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    position integer NOT NULL,
    text text NOT NULL,
    PRIMARY KEY (community_id, position)
);

CREATE TABLE memberships (
    community_id bigint NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    actor_id bigint NOT NULL REFERENCES actors (id),
    role text NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'moderator', 'owner')),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'accepted')),
    follow_ap_id text UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (community_id, actor_id)
);
CREATE INDEX memberships_actor ON memberships (actor_id);

CREATE TABLE posts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ap_id text NOT NULL UNIQUE,
    ref text NOT NULL UNIQUE CHECK (length(ref) = 12),
    local boolean NOT NULL,
    community_id bigint NOT NULL REFERENCES communities (id),
    author_id bigint NOT NULL REFERENCES actors (id),
    title text NOT NULL,
    body text NOT NULL DEFAULT '',
    url text NOT NULL DEFAULT '',
    media_id bigint REFERENCES media (id),
    nsfw boolean NOT NULL DEFAULT false,
    published timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    sort_at timestamptz NOT NULL,
    updated timestamptz,
    locked boolean NOT NULL DEFAULT false,
    held boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    removed_at timestamptz,
    score integer NOT NULL DEFAULT 0,
    up integer NOT NULL DEFAULT 0,
    down integer NOT NULL DEFAULT 0,
    comment_count integer NOT NULL DEFAULT 0,
    last_comment_at timestamptz,
    delivery_state text CHECK (delivery_state IN ('pending', 'delivered', 'rejected')),
    delivery_error text NOT NULL DEFAULT '',
    search tsvector GENERATED ALWAYS AS (to_tsvector('simple', title || ' ' || body)) STORED,
    CHECK (sort_at <= published AND sort_at <= received_at)
);
CREATE INDEX posts_community_sort ON posts (community_id, sort_at DESC, id DESC);
CREATE INDEX posts_sort ON posts (sort_at DESC, id DESC);
CREATE INDEX posts_author ON posts (author_id);
CREATE INDEX posts_search ON posts USING gin (search);

CREATE TABLE comments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ap_id text NOT NULL UNIQUE,
    ref text NOT NULL UNIQUE CHECK (length(ref) = 12),
    local boolean NOT NULL,
    post_id bigint NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    parent_id bigint REFERENCES comments (id) ON DELETE CASCADE,
    author_id bigint NOT NULL REFERENCES actors (id),
    body text NOT NULL,
    published timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    updated timestamptz,
    held boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    removed_at timestamptz,
    score integer NOT NULL DEFAULT 0,
    up integer NOT NULL DEFAULT 0,
    down integer NOT NULL DEFAULT 0,
    delivery_state text CHECK (delivery_state IN ('pending', 'delivered', 'rejected')),
    delivery_error text NOT NULL DEFAULT '',
    search tsvector GENERATED ALWAYS AS (to_tsvector('simple', body)) STORED
);
CREATE INDEX comments_post ON comments (post_id, parent_id);
CREATE INDEX comments_author ON comments (author_id);
CREATE INDEX comments_search ON comments USING gin (search);

CREATE TABLE votes (
    actor_id bigint NOT NULL REFERENCES actors (id),
    post_id bigint REFERENCES posts (id) ON DELETE CASCADE,
    comment_id bigint REFERENCES comments (id) ON DELETE CASCADE,
    value smallint NOT NULL CHECK (value IN (-1, 1)),
    counted boolean NOT NULL,
    ap_id text UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((post_id IS NULL) <> (comment_id IS NULL))
);
CREATE UNIQUE INDEX votes_post ON votes (actor_id, post_id) WHERE post_id IS NOT NULL;
CREATE UNIQUE INDEX votes_comment ON votes (actor_id, comment_id) WHERE comment_id IS NOT NULL;

CREATE TABLE saves (
    actor_id bigint NOT NULL REFERENCES actors (id),
    post_id bigint REFERENCES posts (id) ON DELETE CASCADE,
    comment_id bigint REFERENCES comments (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((post_id IS NULL) <> (comment_id IS NULL))
);
CREATE UNIQUE INDEX saves_post ON saves (actor_id, post_id) WHERE post_id IS NOT NULL;
CREATE UNIQUE INDEX saves_comment ON saves (actor_id, comment_id) WHERE comment_id IS NOT NULL;

CREATE TABLE reports (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reporter_id bigint NOT NULL REFERENCES actors (id),
    post_id bigint REFERENCES posts (id) ON DELETE CASCADE,
    comment_id bigint REFERENCES comments (id) ON DELETE CASCADE,
    community_id bigint NOT NULL REFERENCES communities (id),
    reason text NOT NULL,
    state text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'upheld', 'dismissed')),
    resolved_by bigint REFERENCES actors (id),
    resolved_at timestamptz,
    flag_ap_id text UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((post_id IS NULL) <> (comment_id IS NULL))
);
CREATE INDEX reports_open ON reports (community_id, created_at) WHERE state = 'open';

CREATE TABLE moderation_log (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    community_id bigint REFERENCES communities (id),
    moderator_id bigint NOT NULL REFERENCES actors (id),
    action text NOT NULL,
    target_actor_id bigint REFERENCES actors (id),
    target_ap_id text,
    reason text NOT NULL DEFAULT '',
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX moderation_log_community ON moderation_log (community_id, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION moderation_log_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'moderation_log is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER moderation_log_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON moderation_log
    FOR EACH STATEMENT EXECUTE FUNCTION moderation_log_append_only();

CREATE TABLE bans (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    community_id bigint REFERENCES communities (id) ON DELETE CASCADE,
    actor_id bigint NOT NULL REFERENCES actors (id),
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (community_id, actor_id)
);

CREATE TABLE peers (
    domain text PRIMARY KEY,
    software text NOT NULL DEFAULT '',
    version text NOT NULL DEFAULT '',
    blocked boolean NOT NULL DEFAULT false,
    reason text NOT NULL DEFAULT '',
    last_ok_at timestamptz,
    failing_since timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE directory (
    community_id bigint PRIMARY KEY REFERENCES communities (id) ON DELETE CASCADE,
    added_by bigint NOT NULL REFERENCES actors (id),
    note text NOT NULL DEFAULT '',
    checked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE received_activities (
    ap_id text PRIMARY KEY,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX received_activities_age ON received_activities (received_at);

CREATE TABLE invites (
    code_hash bytea PRIMARY KEY,
    created_by bigint NOT NULL REFERENCES actors (id),
    used_by bigint REFERENCES actors (id),
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((used_by IS NULL) = (used_at IS NULL))
);

CREATE TABLE backups (
    community_id bigint PRIMARY KEY REFERENCES communities (id) ON DELETE CASCADE,
    recipient text NOT NULL,
    destination_enc bytea,
    last_ok_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE backups, invites, received_activities, directory, peers, bans,
    moderation_log, reports, saves, votes, comments, posts, memberships, rules,
    community_domains, communities, media, actors, installation;
DROP FUNCTION moderation_log_append_only();
