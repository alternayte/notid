-- +goose Up
-- active_at orders the Active sort: the latest reply, or the post's own feed time.
ALTER TABLE posts ADD COLUMN active_at timestamptz
    GENERATED ALWAYS AS (greatest(sort_at, coalesce(last_comment_at, sort_at))) STORED;
CREATE INDEX posts_community_active ON posts (community_id, active_at DESC, id DESC);
CREATE INDEX posts_community_top ON posts (community_id, score DESC, id DESC);

-- +goose Down
DROP INDEX posts_community_top;
ALTER TABLE posts DROP COLUMN active_at;
