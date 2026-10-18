-- name: InsertMedia :one
INSERT INTO media (owner_id, storage_key, mime, bytes, sha256, width, height)
VALUES (@owner_id, @storage_key, @mime, @bytes, @sha256, @width, @height)
RETURNING *;

-- name: MediaByKey :one
SELECT * FROM media WHERE storage_key = @storage_key;

-- name: OwnerBytes :one
SELECT coalesce(sum(bytes), 0)::bigint AS bytes FROM media WHERE owner_id = @owner_id;
