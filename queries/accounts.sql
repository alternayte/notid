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

-- name: SoleOwnerOf :one
SELECT c.domain, c.name FROM memberships m JOIN communities c ON c.id = m.community_id
WHERE m.actor_id = @actor_id AND m.role = 'owner'
  AND NOT EXISTS (
    SELECT 1 FROM memberships o
    WHERE o.community_id = m.community_id AND o.role = 'owner' AND o.actor_id <> m.actor_id
  )
LIMIT 1;

-- Deletion tombstones the content so the threads it joined stay readable.
-- name: DeleteOwnPosts :exec
UPDATE posts SET deleted_at = now(), title = '', body = '', url = ''
WHERE author_id = @author_id AND deleted_at IS NULL;

-- name: DeleteOwnComments :exec
UPDATE comments SET deleted_at = now(), body = ''
WHERE author_id = @author_id AND deleted_at IS NULL;

-- name: DeleteMembershipsOf :exec
DELETE FROM memberships WHERE actor_id = @actor_id;

-- name: AnonymizeActor :exec
UPDATE actors SET
    name = 'deleted_' || id, display_name = '', summary = '', bot = false,
    public_key_pem = '', private_key_enc = NULL, deleted_at = now()
WHERE id = @id AND deleted_at IS NULL;
