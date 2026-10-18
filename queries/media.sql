-- name: InsertMedia :one
INSERT INTO media (owner_id, storage_key, mime, bytes, sha256, width, height)
VALUES (@owner_id, @storage_key, @mime, @bytes, @sha256, @width, @height)
RETURNING *;

-- name: MediaByKey :one
SELECT * FROM media WHERE storage_key = @storage_key;

-- An image only removed posts use is not served; the file stays so a restore works.
-- name: MediaToServe :one
SELECT m.*,
    (EXISTS (SELECT 1 FROM posts p WHERE p.media_id = m.id)
     AND NOT EXISTS (SELECT 1 FROM posts p WHERE p.media_id = m.id AND p.removed_at IS NULL))::boolean AS hidden
FROM media m WHERE m.storage_key = @storage_key;

-- Uploads that no post and no community banner uses, once their grace time has passed.
-- name: DeleteUnusedMedia :many
DELETE FROM media m
WHERE m.id IN (
    SELECT u.id FROM media u
    WHERE u.created_at < @before
      AND NOT EXISTS (SELECT 1 FROM posts p WHERE p.media_id = u.id)
      AND NOT EXISTS (SELECT 1 FROM communities c WHERE c.theme->>'banner' = u.storage_key)
    ORDER BY u.id
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
)
RETURNING m.storage_key;

-- name: LockOwnerUploads :exec
SELECT pg_advisory_xact_lock(@owner_id::bigint);

-- name: OwnerBytes :one
SELECT coalesce(sum(bytes), 0)::bigint AS bytes FROM media WHERE owner_id = @owner_id;
