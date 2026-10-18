-- +goose Up
DELETE FROM reports a USING reports b
WHERE a.state = 'open' AND b.state = 'open' AND a.reporter_id = b.reporter_id AND a.id > b.id
  AND (a.post_id = b.post_id OR a.comment_id = b.comment_id);
CREATE UNIQUE INDEX reports_once_post ON reports (reporter_id, post_id) WHERE state = 'open' AND post_id IS NOT NULL;
CREATE UNIQUE INDEX reports_once_comment ON reports (reporter_id, comment_id) WHERE state = 'open' AND comment_id IS NOT NULL;
CREATE INDEX reports_reporter ON reports (reporter_id, created_at);

-- +goose Down
DROP INDEX reports_once_post;
DROP INDEX reports_once_comment;
DROP INDEX reports_reporter;
