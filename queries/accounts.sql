-- name: LockUnusedInvite :one
SELECT code_hash FROM invites
WHERE code_hash = @code_hash AND used_at IS NULL
FOR UPDATE;

-- name: UseInvite :exec
UPDATE invites SET used_by = @used_by, used_at = now()
WHERE code_hash = @code_hash AND used_at IS NULL;

-- name: PersonByHandle :one
SELECT * FROM actors
WHERE kind = 'person' AND domain = @domain AND lower(name) = lower(@name) AND deleted_at IS NULL;

-- name: UpdateProfile :one
UPDATE actors SET display_name = @display_name, summary = @summary, bot = @bot
WHERE id = @id AND local AND kind = 'person'
RETURNING *;
