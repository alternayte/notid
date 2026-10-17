-- name: LockUnusedInvite :one
SELECT code_hash FROM invites
WHERE code_hash = @code_hash AND used_at IS NULL
FOR UPDATE;

-- name: UseInvite :exec
UPDATE invites SET used_by = @used_by, used_at = now()
WHERE code_hash = @code_hash AND used_at IS NULL;
