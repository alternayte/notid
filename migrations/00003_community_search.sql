-- +goose Up
ALTER TABLE communities ADD COLUMN search tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', name || ' ' || title || ' ' || description)) STORED;
CREATE INDEX communities_search ON communities USING gin (search);

-- +goose Down
ALTER TABLE communities DROP COLUMN search;
